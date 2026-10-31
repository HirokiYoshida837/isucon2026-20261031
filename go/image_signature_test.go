package isureal

import (
	"bytes"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"strconv"
	"testing"
)

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
