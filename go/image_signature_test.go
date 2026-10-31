package isureal

import (
	"bytes"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	mrand "math/rand/v2"
	"net/url"
	"strconv"
	"testing"
)

// 変更前の buildSignedURL（url.Values.Encode 版）。出力が 1 バイトも変わらないことを確かめる
func buildSignedURLOld(imageID string, exp int64, sig []byte) string {
	query := url.Values{
		"exp": {strconv.FormatInt(exp, 10)},
		"sig": {base64.RawURLEncoding.EncodeToString(sig)},
	}
	return "/api/images/" + url.PathEscape(imageID) + "?" + query.Encode()
}

func TestBuildSignedURLMatchesOld(t *testing.T) {
	ids := []string{"", "019a2b3c-4d5e-7f80-9a1b-2c3d4e5f6071", "a b/c?d&e=f#g%h+i", "日本語.png", "..", "~!*'();:@$,[]"}
	for range 2000 {
		b := make([]byte, mrand.IntN(40))
		rand.Read(b)
		ids = append(ids, string(b))
	}
	exps := []int64{0, -1, 1, 1793415639, 1<<63 - 1, -1 << 63}
	for i, id := range ids {
		exp := exps[i%len(exps)]
		if i >= len(exps) {
			exp = mrand.Int64() - mrand.Int64()
		}
		sig := make([]byte, mrand.IntN(64))
		rand.Read(sig)
		if got, want := buildSignedURL(id, exp, sig), buildSignedURLOld(id, exp, sig); got != want {
			t.Fatalf("url differs for %q exp=%d: got %q want %q", id, exp, got, want)
		}
	}
}

// 変更前の実装（毎回 hmac.New する）。出力が一致することを確かめる
func imageSignatureOld(key []byte, imageID string, exp int64) []byte {
	value := []byte(strconv.FormatInt(exp, 10) + ":" + imageID)
	for range imageSignatureIterations {
		mac := hmac.New(sha256.New, key)
		mac.Write(value)
		value = mac.Sum(nil)
	}
	return value
}

func TestImageSignatureMatchesOld(t *testing.T) {
	for i := range 50 {
		key := make([]byte, sha256.Size)
		rand.Read(key)
		id := make([]byte, 16)
		rand.Read(id)
		imageID := strconv.Itoa(i) + "-" + string(bytes.ToValidUTF8(id, []byte("x")))
		exp := int64(1793415639 + i*7919)
		if got, want := imageSignature(key, imageID, exp), imageSignatureOld(key, imageID, exp); !bytes.Equal(got, want) {
			t.Fatalf("signature differs for %q exp=%d", imageID, exp)
		}
	}
}

func BenchmarkImageSignature(b *testing.B) {
	key := make([]byte, sha256.Size)
	rand.Read(key)
	for b.Loop() {
		imageSignature(key, "019a2b3c-4d5e-7f80-9a1b-2c3d4e5f6071", 1793415639)
	}
}

func BenchmarkImageSignatureOld(b *testing.B) {
	key := make([]byte, sha256.Size)
	rand.Read(key)
	for b.Loop() {
		imageSignatureOld(key, "019a2b3c-4d5e-7f80-9a1b-2c3d4e5f6071", 1793415639)
	}
}
