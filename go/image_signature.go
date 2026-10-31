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

// HMAC を 1 つ作って Reset で使い回す（鍵のパディング計算と確保を 1000 回繰り返さない）。出力は毎回作る実装と同じ。
func imageSignature(key []byte, imageID string, exp int64) []byte {
	value := []byte(strconv.FormatInt(exp, 10) + ":" + imageID)
	mac := hmac.New(sha256.New, key)
	for range imageSignatureIterations {
		mac.Reset()
		mac.Write(value)
		value = mac.Sum(value[:0])
	}
	return value
}

// 発行した URL は signedURLReuse の間そのまま返す（期限が最大その分だけ短くなる）。
const signedURLReuse = 5 * time.Minute

func signedImageURL(user User, imageID string) string {
	now := time.Now()
	cacheable := len(user.ImageKey) == sha256.Size
	cacheKey := signedURLKey{key: string(user.ImageKey), imageID: imageID}
	if cacheable {
		if e, ok := issuedImageURLs.get(cacheKey); ok && e.exp >= now.Add(imageURLLifetime-signedURLReuse).Unix() {
			return e.url
		}
	}
	exp := now.Add(imageURLLifetime).Unix()
	sig := imageSignature(user.ImageKey, imageID, exp)
	if cacheable {
		return storeIssued(user.ImageKey, imageID, exp, sig)
	}
	return buildSignedURL(imageID, exp, sig)
}

func buildSignedURL(imageID string, exp int64, sig []byte) string {
	query := url.Values{
		"exp": {strconv.FormatInt(exp, 10)},
		"sig": {base64.RawURLEncoding.EncodeToString(sig)},
	}
	return "/api/images/" + url.PathEscape(imageID) + "?" + query.Encode()
}

// 署名を URL にしてキャッシュに入れる。自分が発行した署名は検証済みとして登録する（鍵は画像の持ち主のもの）。
func storeIssued(key []byte, imageID string, exp int64, sig []byte) string {
	u := buildSignedURL(imageID, exp, sig)
	issuedImageURLs.put(signedURLKey{key: string(key), imageID: imageID}, signedURLEntry{url: u, exp: exp, warm: warmingUp.Load()})
	verifiedImageSigs.add(newVerifiedSigKey(imageID, exp, sig))
	return u
}
