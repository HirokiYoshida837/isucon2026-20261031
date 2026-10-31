package isureal

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"hash/fnv"
	"log"
	"net/http"
	"runtime"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/jmoiron/sqlx"
	"github.com/labstack/echo/v5"
)

// europe1 / america1 が、asia1 の画像署名を先回りで計算して渡す（CPU が暇で、SHA 拡張があり 1 署名 約 140µs）。
// 対象: 初期データの投稿画像（新しい順）・プロフィール画像・RealMoji 画像。持ち主の鍵と image_id を起動時に 1 回読む。
// 分担: image_id のハッシュで 2 分割（europe = 0、america = 1）。片方が止まっても asia1 は残りを自前で計算する。
// 流れ: 5 秒ごとに「現在」と「次」のバケット（sig_bucket.go）について、asia1 に無ければ計算しながら送る。
// 帯域は ISUREAL_SIG_PUSH_BPS（既定 2MB/s）に絞る（eu/am ↔ asia のリンクは DB 通信と共用）。
const sigPushChunk = 2048 // 負荷を検知してから止まるまでの遅れを 0.3 秒以内に抑える
const sigPushInterval = 5 * time.Second

// 負荷中は署名の計算を止める（#214: ベンチ中に eu/am の 2 コアを計算が使い切り、america の成長が止まった）。
// 1 秒に sigPushBusyRPS 件以上のリクエストが来たら、その後 sigPushBusyHold の間は「負荷中」。
// 現在と次のバケットは負荷の前に作り終えているので、走行中に計算が止まっても asia1 は困らない（無い分は自前で計算する）。
// 暇なときでも 1 コアはリクエストに残す（workers = GOMAXPROCS − 1）。
const sigPushBusyRPS = 2
const sigPushBusyHold = 3 * time.Second

var sigPushRequests atomic.Int64
var sigPushBusyUntil atomic.Int64 // UnixNano

// 全リクエストを数える（負荷の検知用）
func sigPushLoadMiddleware(next echo.HandlerFunc) echo.HandlerFunc {
	return func(c *echo.Context) error {
		sigPushRequests.Add(1)
		return next(c)
	}
}

func sigPushBusy() bool { return time.Now().UnixNano() < sigPushBusyUntil.Load() }

func sigPushWatchLoad() {
	prev := sigPushRequests.Load()
	for {
		time.Sleep(time.Second)
		cur := sigPushRequests.Load()
		if cur-prev >= sigPushBusyRPS {
			sigPushBusyUntil.Store(time.Now().Add(sigPushBusyHold).UnixNano())
		}
		prev = cur
	}
}

// 負荷中は待つ
func sigPushWaitIdle() {
	for sigPushBusy() {
		time.Sleep(200 * time.Millisecond)
	}
}

type sigPushItem struct {
	key []byte
	id  string
}

type sigPusher struct {
	db       *sqlx.DB
	shard    int
	target   string
	token    string
	bps      int
	maxItems int
	client   *http.Client
	items    []sigPushItem
	computed map[int64][][32]byte // バケット → items と同じ並びの署名（チャンク単位に埋める）
	done     map[int64][]bool     // バケット → チャンクごとの計算済み
	pushedOK map[int64]bool
}

func startSigPusher(db *sqlx.DB, config *Config) {
	if config.SigPushTarget == "" || config.SigPushTarget == "off" {
		return
	}
	shard := map[string]int{"europe": 0, "america": 1}[config.Region]
	p := &sigPusher{
		db: db, shard: shard, target: config.SigPushTarget, token: signTokenFor(config.DBPassword),
		bps: config.SigPushBPS, maxItems: config.SigPushMaxItems,
		client:   &http.Client{Timeout: 120 * time.Second},
		computed: map[int64][][32]byte{}, done: map[int64][]bool{}, pushedOK: map[int64]bool{},
	}
	go sigPushWatchLoad()
	go p.run()
}

func (p *sigPusher) run() {
	// 失敗しても 5 秒ごとに全件を読み直さない（#205）。5s → 30s → 60s
	for wait := sigPushInterval; ; wait = min(max(wait*6, 30*time.Second), 60*time.Second) {
		if err := p.load(); err == nil {
			break
		} else {
			log.Printf("sig push: load failed: %v", err)
			time.Sleep(wait)
		}
	}
	log.Printf("sig push: %d images (shard %d) -> %s", len(p.items), p.shard, p.target)
	for {
		cur := sigBucketOf(time.Now())
		for b := range p.computed {
			if b < cur {
				delete(p.computed, b)
				delete(p.done, b)
				delete(p.pushedOK, b)
			}
		}
		have := p.status()
		for _, b := range []int64{cur, cur + 1} {
			if have == nil { // asia1 に届かない間も計算は進めておく
				p.computeAll(b)
				continue
			}
			if have[b] == 0 { // asia1 が再起動した
				p.pushedOK[b] = false
			}
			if !p.pushedOK[b] {
				if err := p.push(b); err != nil {
					log.Printf("sig push: bucket %d: %v", b, err)
					break
				}
				p.pushedOK[b] = true
			}
		}
		time.Sleep(sigPushInterval)
	}
}

// 署名する画像の一覧。この分担（shard）の分だけ持つ
func (p *sigPusher) load() error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	keys := map[string][]byte{}
	seen := map[string]struct{}{}
	add := func(id, secretKeyHash string) {
		if len(p.items) >= p.maxItems || len(id) == 0 || len(id) > sigRowMaxIDLen {
			return
		}
		h := fnv.New32a()
		h.Write([]byte(id))
		if int(h.Sum32()%2) != p.shard {
			return
		}
		if _, dup := seen[id]; dup {
			return
		}
		key, ok := keys[secretKeyHash]
		if !ok {
			k, err := imageSigningKey(secretKeyHash)
			if err != nil {
				return
			}
			key, keys[secretKeyHash] = k, k
		}
		seen[id] = struct{}{}
		p.items = append(p.items, sigPushItem{key: key, id: id})
	}
	p.items = p.items[:0]
	// posts 全件を posted_at で並べ替える SELECT（asia2 で 5〜30 秒・ディスク sort）をやめ、投稿期間ごとに index で引く（#178 / #197 と同じ）
	var users []struct {
		ID             string  `db:"id"`
		SecretKeyHash  string  `db:"secret_key_hash"`
		ProfileImageID *string `db:"profile_image_id"`
	}
	if err := p.db.SelectContext(ctx, &users, `SELECT id, secret_key_hash, profile_image_id FROM users WHERE status = 'active'`); err != nil {
		return err
	}
	hashes := make(map[string]string, len(users))
	for _, u := range users {
		hashes[u.ID] = u.SecretKeyHash
	}
	scan := func(q string, args ...any) error {
		rows, err := p.db.QueryxContext(ctx, q, args...)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var id, userID string
			if err := rows.Scan(&id, &userID); err != nil {
				return err
			}
			if hash, ok := hashes[userID]; ok {
				add(id, hash)
			}
		}
		return rows.Err()
	}
	var windows []string
	if err := p.db.SelectContext(ctx, &windows, `SELECT id FROM posting_windows ORDER BY local_date DESC, starts_at DESC`); err != nil {
		return err
	}
	for _, windowID := range windows {
		if len(p.items) >= p.maxItems {
			break
		}
		// idx_posts_posting_window_id_posted_at_id（WHERE deleted_at IS NULL）で引く
		if err := scan(`SELECT image_id, author_id FROM posts WHERE posting_window_id = $1 AND deleted_at IS NULL ORDER BY posted_at DESC`, windowID); err != nil {
			return err
		}
	}
	for _, u := range users {
		if u.ProfileImageID != nil {
			add(*u.ProfileImageID, u.SecretKeyHash)
		}
	}
	// 列は reactor_id（r.user_id で起動以来ずっと失敗していた。#205）
	return scan(`SELECT image_id, reactor_id FROM realmojis`)
}

func (p *sigPusher) status() map[int64]int {
	req, _ := http.NewRequest(http.MethodGet, p.target+"/internal/sigs/status", nil)
	req.Header.Set("X-Sign-Token", p.token)
	resp, err := p.client.Do(req)
	if err != nil {
		return nil
	}
	defer resp.Body.Close()
	var raw map[string]int
	if resp.StatusCode != http.StatusOK || json.NewDecoder(resp.Body).Decode(&raw) != nil {
		return nil
	}
	out := make(map[int64]int, len(raw))
	for k, v := range raw {
		if b, err := strconv.ParseInt(k, 10, 64); err == nil {
			out[b] = v
		}
	}
	return out
}

func (p *sigPusher) chunks() int { return (len(p.items) + sigPushChunk - 1) / sigPushChunk }

func (p *sigPusher) ensureBucket(b int64) {
	if p.computed[b] == nil {
		p.computed[b] = make([][32]byte, len(p.items))
		p.done[b] = make([]bool, p.chunks())
	}
}

// チャンク ci の署名を計算する（GOMAXPROCS 並列。AVX-512 があれば 16 個ずつ）
func (p *sigPusher) computeChunk(b int64, ci int) {
	p.ensureBucket(b)
	if p.done[b][ci] {
		return
	}
	sigPushWaitIdle()
	exp := sigBucketExp(b)
	start, end := ci*sigPushChunk, min((ci+1)*sigPushChunk, len(p.items))
	workers := max(1, runtime.GOMAXPROCS(0)-1)
	per := (end - start + workers - 1) / workers
	var wg sync.WaitGroup
	for w := 0; w < workers; w++ {
		s, e := start+w*per, min(start+(w+1)*per, end)
		if s >= e {
			break
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			keys := make([][]byte, 0, e-s)
			ids := make([]string, 0, e-s)
			for i := s; i < e; i++ {
				keys = append(keys, p.items[i].key)
				ids = append(ids, p.items[i].id)
			}
			var sigs [][]byte
			if simdAvailable() && len(keys) >= simdMinLanes {
				countSigBatch(sPushBatch, len(keys))
				sigs = imageSignatures(keys, ids, exp)
			} else {
				sigStat[sPushScalar].Add(int64(len(keys)))
				sigs = make([][]byte, len(keys))
				for i := range keys {
					sigs[i] = imageSignature(keys[i], ids[i], exp)
				}
			}
			for i := range sigs {
				copy(p.computed[b][s+i][:], sigs[i])
			}
		}()
	}
	wg.Wait()
	p.done[b][ci] = true
}

func (p *sigPusher) computeAll(b int64) {
	for ci := 0; ci < p.chunks(); ci++ {
		p.computeChunk(b, ci)
	}
}

// バケット b の全チャンクを asia1 に送る。計算は 1 チャンク先行させ、送信は bps に絞る
func (p *sigPusher) push(b int64) error {
	p.ensureBucket(b)
	n := p.chunks()
	if n == 0 {
		return nil
	}
	p.computeChunk(b, 0)
	started := time.Now()
	sent := 0
	buf := make([]byte, 0, sigPushChunk*(sha256.Size+40))
	for ci := 0; ci < n; ci++ {
		var wg sync.WaitGroup
		if ci+1 < n {
			wg.Add(1)
			go func() { defer wg.Done(); p.computeChunk(b, ci+1) }()
		}
		start, end := ci*sigPushChunk, min((ci+1)*sigPushChunk, len(p.items))
		ids := make([]string, 0, end-start)
		for i := start; i < end; i++ {
			ids = append(ids, p.items[i].id)
		}
		buf = encodeSigRows(buf[:0], ids, p.computed[b][start:end])
		if err := p.send(b, buf); err != nil {
			wg.Wait()
			return err
		}
		sent += len(buf)
		if p.bps > 0 {
			if ahead := time.Duration(float64(sent)/float64(p.bps)*float64(time.Second)) - time.Since(started); ahead > 0 {
				time.Sleep(ahead)
			}
		}
		wg.Wait()
	}
	return nil
}

func (p *sigPusher) send(b int64, body []byte) error {
	req, err := http.NewRequest(http.MethodPost, p.target+"/internal/sigs?bucket="+strconv.FormatInt(b, 10), bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("X-Sign-Token", p.token)
	req.Header.Set("Content-Type", "application/octet-stream")
	resp, err := p.client.Do(req)
	if err != nil {
		return err
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		return &httpStatusError{resp.StatusCode}
	}
	return nil
}

type httpStatusError struct{ code int }

func (e *httpStatusError) Error() string { return "asia1 returned " + strconv.Itoa(e.code) }
