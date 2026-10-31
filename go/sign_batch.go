package isureal

import (
	"crypto/sha256"
	"time"
)

// 一覧のレスポンスを作る前に、キャッシュに無い画像の署名を AVX-512 で 16 個ずつまとめて作っておく。
// あとの signedImageURL / newPostResponse などはキャッシュに当たるだけになる。
// SIMD が使えない環境（GOEXPERIMENT=simd 無し・AVX-512 無し）では何もしない（従来どおり 1 個ずつ計算する）。

type sigReq struct {
	key     []byte
	imageID string
}

// 画像と、その持ち主のプロフィール画像（レスポンスに一緒に出る）の署名依頼
func imageSigReqs(imageID string, user User) []sigReq {
	return append([]sigReq{{key: user.ImageKey, imageID: imageID}}, profileSigReqs(user)...)
}

func profileSigReqs(user User) []sigReq {
	if user.ProfileImageID == nil {
		return nil
	}
	return []sigReq{{key: user.ImageKey, imageID: *user.ProfileImageID}}
}

func prewarmSigs(reqs []sigReq) {
	if len(reqs) < simdMinLanes || !simdAvailable() {
		return
	}
	bucket := sigBucketOf(time.Now())
	exp := sigBucketExp(bucket)
	seen := make(map[signedURLKey]struct{}, len(reqs))
	keys := make([][]byte, 0, len(reqs))
	ids := make([]string, 0, len(reqs))
	for _, r := range reqs {
		if len(r.key) != sha256.Size {
			continue
		}
		k := signedURLKey{key: string(r.key), imageID: r.imageID}
		if _, dup := seen[k]; dup {
			continue
		}
		seen[k] = struct{}{}
		if _, ok := pushedSigs.get(bucket, r.imageID); ok { // europe1 / america1 が計算済み
			continue
		}
		if e, ok := issuedImageURLs.get(k); ok && e.exp >= exp {
			continue
		}
		keys = append(keys, r.key)
		ids = append(ids, r.imageID)
	}
	if len(keys) < simdMinLanes {
		return
	}
	if warmingUp.Load() { // ウォームアップ中に走行中のリクエストが来ることは無い前提（initialize で止まる）
		countSigBatch(sWarmBatch, len(keys))
	} else {
		countSigBatch(sPrewarmBatch, len(keys))
	}
	sigs := imageSignatures(keys, ids, exp)
	for i := range keys {
		storeIssued(keys[i], ids[i], exp, sigs[i])
	}
}
