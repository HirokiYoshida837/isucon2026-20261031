//go:build goexperiment.simd && amd64

package isureal

import (
	"bytes"
	"crypto/rand"
	"fmt"
	"sync"
	"testing"
)

func randomInputs(n int) ([][]byte, []string) {
	keys := make([][]byte, n)
	ids := make([]string, n)
	for i := range keys {
		keys[i] = make([]byte, 32)
		rand.Read(keys[i])
		b := make([]byte, 16)
		rand.Read(b)
		ids[i] = fmt.Sprintf("%x-%x-%x", b[:4], b[4:8], b[8:])
	}
	return keys, ids
}

func TestImageSignaturesMatchScalar(t *testing.T) {
	if !simdAvailable() {
		t.Skip("AVX-512 が無い")
	}
	for _, n := range []int{1, 2, 5, 15, 16, 17, 40} {
		keys, ids := randomInputs(n)
		exp := int64(1793415639 + n)
		got := imageSignatures(keys, ids, exp)
		for i := range keys {
			if want := imageSignature(keys[i], ids[i], exp); !bytes.Equal(got[i], want) {
				t.Fatalf("n=%d lane=%d: 一致しない\n got=%x\nwant=%x", n, i, got[i], want)
			}
		}
	}
}

func BenchmarkScalar16(b *testing.B) {
	keys, ids := randomInputs(16)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for j := range keys {
			imageSignature(keys[j], ids[j], 1793415639)
		}
	}
}

func BenchmarkSimd16(b *testing.B) {
	if !simdAvailable() {
		b.Skip("AVX-512 が無い")
	}
	keys, ids := randomInputs(16)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		imageSignatures(keys, ids, 1793415639)
	}
}

func TestPrewarmSigsFillsCache(t *testing.T) {
	if !simdAvailable() {
		t.Skip("AVX-512 が無い")
	}
	issuedImageURLs.reset()
	verifiedImageSigs.reset()
	keys, ids := randomInputs(37)
	reqs := make([]sigReq, len(keys))
	for i := range keys {
		reqs[i] = sigReq{key: keys[i], imageID: ids[i]}
	}
	reqs = append(reqs, reqs[0]) // 重複は 1 つにまとまる
	prewarmSigs(reqs)
	for i := range keys {
		e, ok := issuedImageURLs.get(signedURLKey{key: string(keys[i]), imageID: ids[i]})
		if !ok {
			t.Fatalf("%d: キャッシュに入っていない", i)
		}
		if want := buildSignedURL(ids[i], e.exp, imageSignature(keys[i], ids[i], e.exp)); want != e.url {
			t.Fatalf("%d: URL が従来の計算と違う", i)
		}
		if got := signedImageURL(User{ImageKey: keys[i]}, ids[i]); got != e.url {
			t.Fatalf("%d: signedImageURL がキャッシュに当たっていない", i)
		}
	}
}

func TestImageSignaturesConcurrent(t *testing.T) {
	if !simdAvailable() {
		t.Skip("AVX-512 が無い")
	}
	var wg sync.WaitGroup
	errs := make(chan string, 8)
	for g := 0; g < 8; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for r := 0; r < 20; r++ {
				keys, ids := randomInputs(1 + r*3)
				got := imageSignatures(keys, ids, 1793415639)
				for i := range keys {
					if !bytes.Equal(got[i], imageSignature(keys[i], ids[i], 1793415639)) {
						errs <- "並行実行で一致しない"
						return
					}
				}
			}
		}()
	}
	wg.Wait()
	close(errs)
	for e := range errs {
		t.Error(e)
	}
}
