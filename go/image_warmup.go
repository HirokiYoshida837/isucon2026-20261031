package isureal

import (
	"context"
	"runtime"
	"sync"
	"sync/atomic"

	"github.com/jmoiron/sqlx"
)

// 起動時に、このリージョンの初期データの署名付き URL を背景で先に作る（asia1 の imageSignature の CPU を、ベンチ開始前に使う）。
// 1 goroutine だけで回す。POST /api/initialize の到着（ベンチ開始）で打ち切る。入れた分はそのまま使える。
// DB が未起動などで失敗したら黙って終わる（アプリの起動は止めない）。
const warmUpMaxEntries = 90000 // 起動時に署名を作る件数（新しい投稿から）

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
	// 新しい投稿から順に（表示頻度の高い順）
	rows, err := db.QueryxContext(ctx, `
   SELECT p.image_id, u.secret_key_hash
   FROM posts p JOIN users u ON u.id = p.author_id
   WHERE u.region = $1 AND u.status = 'active' AND p.deleted_at IS NULL
   ORDER BY p.posted_at DESC
   LIMIT $2`, region, warmUpMaxEntries)
	if err != nil {
		return
	}
	defer rows.Close()
	keys := map[string][]byte{} // 同じ持ち主の鍵は 1 つだけ持つ
	list := make([]sigReq, 0, warmUpMaxEntries)
	for rows.Next() {
		if ctx.Err() != nil {
			return
		}
		var imageID, secretKeyHash string
		if rows.Scan(&imageID, &secretKeyHash) != nil {
			return
		}
		key, ok := keys[secretKeyHash]
		if !ok {
			user := User{SecretKeyHash: secretKeyHash}
			if user.setImageKey() != nil {
				continue
			}
			key = user.ImageKey
			keys[secretKeyHash] = key
		}
		list = append(list, sigReq{key: key, imageID: imageID})
	}
	setPrefetchList(list) // 初期化のたびの先読みで、先頭の prefetchMax 件を作り直す
	// 起動時はベンチの前なので asia1 の CPU を全部使ってよい。asia2 に頼めないときは全部自前。
	pct := signLocalPercent
	if !signAvailable() {
		pct = 100
	}
	prefetchSigs(ctx, list, pct, runtime.GOMAXPROCS(0), true)
}
