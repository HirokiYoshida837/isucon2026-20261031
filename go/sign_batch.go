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
	now := time.Now()
	exp := now.Add(imageURLLifetime).Unix()
	reuseFrom := now.Add(imageURLLifetime - signedURLReuse).Unix()
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
		if e, ok := issuedImageURLs.get(k); ok && e.exp >= reuseFrom {
			continue
		}
		keys = append(keys, r.key)
		ids = append(ids, r.imageID)
	}
	if len(keys) < simdMinLanes {
		return
	}
	sigs := imageSignatures(keys, ids, exp)
	for i := range keys {
		storeIssued(keys[i], ids[i], exp, sigs[i])
	}
}
