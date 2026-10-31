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
	"time"

	"github.com/jmoiron/sqlx"
)

// europe1 / america1 が、asia1 の画像署名を先回りで計算して渡す（CPU が暇で、SHA 拡張があり 1 署名 約 140µs）。
// 対象: 初期データの投稿画像（新しい順）・プロフィール画像・RealMoji 画像。持ち主の鍵と image_id を起動時に 1 回読む。
// 分担: image_id のハッシュで 2 分割（europe = 0、america = 1）。片方が止まっても asia1 は残りを自前で計算する。
// 流れ: 5 秒ごとに「現在」と「次」のバケット（sig_bucket.go）について、asia1 に無ければ計算しながら送る。
// 帯域は ISUREAL_SIG_PUSH_BPS（既定 2MB/s）に絞る（eu/am ↔ asia のリンクは DB 通信と共用）。
const sigPushChunk = 8192
const sigPushInterval = 5 * time.Second

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
	go p.run()
}

func (p *sigPusher) run() {
	for {
		if err := p.load(); err == nil {
			break
		} else {
			log.Printf("sig push: load failed: %v", err)
			time.Sleep(sigPushInterval)
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
	for _, q := range []string{
		`SELECT p.image_id, u.secret_key_hash FROM posts p JOIN users u ON u.id = p.author_id
		 WHERE p.deleted_at IS NULL AND u.status = 'active' ORDER BY p.posted_at DESC`,
		`SELECT u.profile_image_id, u.secret_key_hash FROM users u WHERE u.profile_image_id IS NOT NULL AND u.status = 'active'`,
		`SELECT r.image_id, u.secret_key_hash FROM realmojis r JOIN users u ON u.id = r.user_id WHERE u.status = 'active'`,
	} {
		rows, err := p.db.QueryxContext(ctx, q)
		if err != nil {
			return err
		}
		for rows.Next() {
			var id, hash string
			if err := rows.Scan(&id, &hash); err != nil {
				rows.Close()
				return err
			}
			add(id, hash)
		}
		rows.Close()
	}
	return nil
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
	exp := sigBucketExp(b)
	start, end := ci*sigPushChunk, min((ci+1)*sigPushChunk, len(p.items))
	workers := runtime.GOMAXPROCS(0)
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
				sigs = imageSignatures(keys, ids, exp)
			} else {
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
