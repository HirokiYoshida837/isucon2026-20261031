//go:build goexperiment.simd && amd64

package isureal

import (
	"bytes"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"fmt"
	mrand "math/rand/v2"
	"simd/archsimd"
	"testing"
)

// 置き換える前の archsimd 版の連鎖（比較用）
func hmacChain16Archsimd(keys, vals *[simdLanes][32]byte, iters int) [simdLanes][32]byte {
	kVecOnce.Do(initKVec)
	keyWords := loadWords(keys)
	ipad := padState(&keyWords, 0x36363636)
	opad := padState(&keyWords, 0x5c5c5c5c)
	v := loadWords(vals)
	var in [16]vec
	in[8] = archsimd.BroadcastUint32x16(0x80000000)
	in[15] = archsimd.BroadcastUint32x16(768)
	for it := 0; it < iters; it++ {
		inner := ipad
		copy(in[:8], v[:])
		compress16(&inner, &in)
		outer := opad
		copy(in[:8], inner[:])
		compress16(&outer, &in)
		v = outer
	}
	var out [simdLanes][32]byte
	storeWords(&v, &out)
	return out
}

func hmacChainScalar(key, val []byte, iters int) []byte {
	mac := hmac.New(sha256.New, key)
	v := append([]byte(nil), val...)
	for range iters {
		mac.Reset()
		mac.Write(v)
		v = mac.Sum(v[:0])
	}
	return v
}

func TestHmacChain16AVX512(t *testing.T) {
	if !simdAvailable() {
		t.Skip("AVX-512 が無い")
	}
	for _, iters := range []int{0, 1, 2, 3, 999} {
		var ks, vs [simdLanes][32]byte
		for l := range simdLanes {
			rand.Read(ks[l][:])
			rand.Read(vs[l][:])
		}
		got := hmacChain16(&ks, &vs, iters)
		ref := hmacChain16Archsimd(&ks, &vs, iters)
		for l := range simdLanes {
			want := hmacChainScalar(ks[l][:], vs[l][:], iters)
			if !bytes.Equal(got[l][:], want) || !bytes.Equal(ref[l][:], want) {
				t.Fatalf("iters=%d lane=%d: 一致しない\n got=%x\n ref=%x\nwant=%x", iters, l, got[l], ref[l], want)
			}
		}
	}
}

func TestImageSignaturesRandomAVX512(t *testing.T) {
	if !simdAvailable() {
		t.Skip("AVX-512 が無い")
	}
	for n := 3; n <= 16; n++ {
		for r := 0; r < 3; r++ {
			keys, ids := randomInputs(n)
			if r == 1 {
				for i := range ids {
					ids[i] = fmt.Sprintf("%d", mrand.Uint64()) // 長さの違う image_id
				}
			}
			exp := mrand.Int64N(1 << 40)
			got := imageSignatures(keys, ids, exp)
			for i := range keys {
				if want := imageSignature(keys[i], ids[i], exp); !bytes.Equal(got[i], want) {
					t.Fatalf("n=%d lane=%d exp=%d: 一致しない", n, i, exp)
				}
			}
		}
	}
}

func benchChain(b *testing.B, f func(keys, vals *[simdLanes][32]byte, iters int) [simdLanes][32]byte) {
	if !simdAvailable() {
		b.Skip("AVX-512 が無い")
	}
	var ks, vs [simdLanes][32]byte
	for l := range simdLanes {
		rand.Read(ks[l][:])
		rand.Read(vs[l][:])
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		f(&ks, &vs, imageSignatureIterations-1)
	}
}

func BenchmarkChain16Archsimd(b *testing.B) { benchChain(b, hmacChain16Archsimd) }
func BenchmarkChain16AVX512(b *testing.B)   { benchChain(b, hmacChain16) }
