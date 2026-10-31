package isureal

import (
	"context"
	"sync"

	"github.com/jmoiron/sqlx"
)

// 起動時に、このリージョンの初期データの署名付き URL を背景で先に作る（asia1 の imageSignature の CPU を、ベンチ開始前に使う）。
// 1 goroutine だけで回す。POST /api/initialize の到着（ベンチ開始）で打ち切る。入れた分はそのまま使える。
// DB が未起動などで失敗したら黙って終わる（アプリの起動は止めない）。
const warmUpMaxEntries = 90000 // verifiedSigMaxEntries を超えると全消去されるため、その下に収める

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
	for rows.Next() {
		if ctx.Err() != nil {
			return
		}
		var imageID, secretKeyHash string
		if rows.Scan(&imageID, &secretKeyHash) != nil {
			return
		}
		user := User{SecretKeyHash: secretKeyHash}
		if user.setImageKey() != nil {
			continue
		}
		signedImageURL(user, imageID)
	}
}
