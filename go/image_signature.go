package isureal

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"net/url"
	"strconv"
	"time"
)

const imageURLLifetime = 24 * time.Hour
const imageSignatureIterations = 1000

func imageSigningKey(secretKeyHash string) ([]byte, error) {
	key, err := hex.DecodeString(secretKeyHash)
	if err != nil || len(key) != sha256.Size {
		return nil, fmt.Errorf("invalid image signing key")
	}
	return key, nil
}

func imageSignature(key []byte, imageID string, exp int64) []byte {
	value := []byte(strconv.FormatInt(exp, 10) + ":" + imageID)
	for range imageSignatureIterations {
		mac := hmac.New(sha256.New, key)
		mac.Write(value)
		value = mac.Sum(nil)
	}
	return value
}

func signedImageURL(user User, imageID string) string {
	// exp は分単位に切り下げる（期限が最大 60 秒短くなるだけ）。同じ分の中では同じ URL を返せる。
	exp := time.Now().Add(imageURLLifetime).Truncate(time.Minute).Unix()
	cacheable := len(user.ImageKey) == sha256.Size
	cacheKey := signedURLKey{key: string(user.ImageKey), imageID: imageID, exp: exp}
	if cacheable {
		if u, ok := issuedImageURLs.get(cacheKey); ok {
			return u
		}
	}
	sig := imageSignature(user.ImageKey, imageID, exp)
	query := url.Values{
		"exp": {strconv.FormatInt(exp, 10)},
		"sig": {base64.RawURLEncoding.EncodeToString(sig)},
	}
	u := "/api/images/" + url.PathEscape(imageID) + "?" + query.Encode()
	if cacheable {
		issuedImageURLs.put(cacheKey, u)
		// 自分が発行した署名は検証済みとして登録する（呼び出し元は画像の持ち主の鍵で署名している）。
		verifiedImageSigs.add(newVerifiedSigKey(imageID, exp, sig))
	}
	return u
}
