package isureal

import (
	"context"
	"sync"
	"sync/atomic"

	"github.com/jmoiron/sqlx"
)

// 起動時に、このリージョンの初期データの署名付き URL を背景で先に作る（asia1 の imageSignature の CPU を、ベンチ開始前に使う）。
// 1 goroutine だけで回す。POST /api/initialize の到着（ベンチ開始）で打ち切る。入れた分はそのまま使える。
// DB が未起動などで失敗したら黙って終わる（アプリの起動は止めない）。
const warmUpMaxEntries = 90000 // verifiedSigMaxEntries を超えると全消去されるため、その下に収める

// ウォームアップ中に入れたキャッシュ項目に印を付けるための旗。
var warmingUp atomic.Bool

var (
	warmUpMu     sync.Mutex
	warmUpCancel context.CancelFunc
)

func startImageURLWarmUp(db *sqlx.DB, region string) {
	ctx, cancel := context.WithCancel(context.Background())
	warmUpMu.Lock()
	warmUpCancel = cancel
	warmUpMu.Unlock()
	go func() {
		defer cancel()
		warmingUp.Store(true)
		defer warmingUp.Store(false)
		warmUpImageURLs(ctx, db, region)
	}()
}

func stopImageURLWarmUp() {
	warmUpMu.Lock()
	defer warmUpMu.Unlock()
	if warmUpCancel != nil {
		warmUpCancel()
	}
}

func warmUpImageURLs(ctx context.Context, db *sqlx.DB, region string) {
	// posts を全件読んで posted_at で並べ替える 1 本の SELECT（asia2 で 40 秒・ディスク sort・users / posts の AccessShareLock を
	// 読み切るまで握る）をやめ、投稿期間ごとに index で引く（#178）。1 回の結果が小さいので HMAC の計算中にロックを握らない
	var users []struct {
		ID            string `db:"id"`
		SecretKeyHash string `db:"secret_key_hash"`
	}
	if err := db.SelectContext(ctx, &users, `SELECT id, secret_key_hash FROM users WHERE region = $1 AND status = 'active'`, region); err != nil {
		return
	}
	keys := make(map[string][]byte, len(users))
	for _, u := range users {
		user := User{SecretKeyHash: u.SecretKeyHash}
		if user.setImageKey() == nil {
			keys[u.ID] = user.ImageKey
		}
	}
	// 新しい投稿期間から順に（表示頻度の高い順）
	var windows []string
	if err := db.SelectContext(ctx, &windows, `SELECT id FROM posting_windows WHERE region = $1 ORDER BY local_date DESC`, region); err != nil {
		return
	}
	batch := make([]sigReq, 0, 512)
	defer func() { prewarmSigs(batch) }()
	total := 0
	for _, windowID := range windows {
		if ctx.Err() != nil || total >= warmUpMaxEntries {
			return
		}
		// idx_posts_posting_window_id_posted_at_id（WHERE deleted_at IS NULL）で引く
		rows, err := db.QueryxContext(ctx, `SELECT image_id, author_id FROM posts WHERE posting_window_id = $1 AND deleted_at IS NULL ORDER BY posted_at DESC`, windowID)
		if err != nil {
			return
		}
		for rows.Next() {
			var imageID, authorID string
			if rows.Scan(&imageID, &authorID) != nil {
				rows.Close()
				return
			}
			key, ok := keys[authorID]
			if !ok {
				continue
			}
			batch = append(batch, sigReq{key: key, imageID: imageID})
			total++
			if len(batch) == cap(batch) {
				prewarmSigs(batch)
				batch = batch[:0]
			}
		}
		rows.Close()
	}
}
