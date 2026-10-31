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
const warmUpMaxEntries = 650000 // 一覧に持つ件数（asia の投稿 約 62 万件）
const warmUpLocalMax = 90000    // 自前で計算する件数。verifiedSigMaxEntries を超えると全消去されるため、その下に収める

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
	list := make([]sigReq, 0, 1024)
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
	setPrefetchList(list)
	if signAvailable() {
		prefetchSigs(ctx, list) // europe1 / america1 に頼む
		return
	}
	for _, r := range list[:min(len(list), warmUpLocalMax)] {
		if ctx.Err() != nil {
			return
		}
		signedImageURL(User{ImageKey: r.key}, r.imageID)
	}
}
