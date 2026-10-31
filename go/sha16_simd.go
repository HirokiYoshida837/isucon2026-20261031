//go:build goexperiment.simd && amd64

package isureal

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/binary"
	"simd/archsimd"
	"strconv"
	"sync"
)

// AVX-512 で独立した 16 個の署名（HMAC-SHA256 の 1000 回連鎖）を 1 命令で同時に計算する。
// 出力は imageSignature と同じ。GOEXPERIMENT=simd でビルドしたときだけ有効（go/Makefile）。
const simdLanes = 16
const simdMinLanes = 3 // 16 レーン 1 回（約 1.3ms）が、従来の 3 個ぶん（約 0.45ms × 3）と同じくらい

func simdAvailable() bool { return archsimd.X86.AVX512() }

type vec = archsimd.Uint32x16

var sha256K = [64]uint32{
	0x428a2f98, 0x71374491, 0xb5c0fbcf, 0xe9b5dba5, 0x3956c25b, 0x59f111f1, 0x923f82a4, 0xab1c5ed5,
	0xd807aa98, 0x12835b01, 0x243185be, 0x550c7dc3, 0x72be5d74, 0x80deb1fe, 0x9bdc06a7, 0xc19bf174,
	0xe49b69c1, 0xefbe4786, 0x0fc19dc6, 0x240ca1cc, 0x2de92c6f, 0x4a7484aa, 0x5cb0a9dc, 0x76f988da,
	0x983e5152, 0xa831c66d, 0xb00327c8, 0xbf597fc7, 0xc6e00bf3, 0xd5a79147, 0x06ca6351, 0x14292967,
	0x27b70a85, 0x2e1b2138, 0x4d2c6dfc, 0x53380d13, 0x650a7354, 0x766a0abb, 0x81c2c92e, 0x92722c85,
	0xa2bfe8a1, 0xa81a664b, 0xc24b8b70, 0xc76c51a3, 0xd192e819, 0xd6990624, 0xf40e3585, 0x106aa070,
	0x19a4c116, 0x1e376c08, 0x2748774c, 0x34b0bcb5, 0x391c0cb3, 0x4ed8aa4a, 0x5b9cca4f, 0x682e6ff3,
	0x748f82ee, 0x78a5636f, 0x84c87814, 0x8cc70208, 0x90befffa, 0xa4506ceb, 0xbef9a3f7, 0xc67178f2,
}

var sha256H0 = [8]uint32{0x6a09e667, 0xbb67ae85, 0x3c6ef372, 0xa54ff53a, 0x510e527f, 0x9b05688c, 0x1f83d9ab, 0x5be0cd19}

var (
	kVec     [64]vec
	kVecOnce sync.Once
)

func initKVec() {
	for i, k := range sha256K {
		kVec[i] = archsimd.BroadcastUint32x16(k)
	}
}

// 16 レーンぶんの SHA-256 の圧縮関数（1 ブロック）。st は 8 語の状態、in は 16 語のメッセージ。
func compress16(st *[8]vec, in *[16]vec) {
	var w [64]vec
	copy(w[:16], in[:])
	for i := 16; i < 64; i++ {
		x, y := w[i-15], w[i-2]
		s0 := x.RotateAllRight(7).Xor(x.RotateAllRight(18)).Xor(x.ShiftAllRight(3))
		s1 := y.RotateAllRight(17).Xor(y.RotateAllRight(19)).Xor(y.ShiftAllRight(10))
		w[i] = w[i-16].Add(s0).Add(w[i-7]).Add(s1)
	}
	a, b, c, d, e, f, g, h := st[0], st[1], st[2], st[3], st[4], st[5], st[6], st[7]
	for i := 0; i < 64; i++ {
		s1 := e.RotateAllRight(6).Xor(e.RotateAllRight(11)).Xor(e.RotateAllRight(25))
		ch := e.And(f).Xor(g.AndNot(e))
		t1 := h.Add(s1).Add(ch).Add(kVec[i]).Add(w[i])
		s0 := a.RotateAllRight(2).Xor(a.RotateAllRight(13)).Xor(a.RotateAllRight(22))
		maj := a.And(b).Xor(a.And(c)).Xor(b.And(c))
		t2 := s0.Add(maj)
		h, g, f, e, d, c, b, a = g, f, e, d.Add(t1), c, b, a, t1.Add(t2)
	}
	st[0], st[1], st[2], st[3] = st[0].Add(a), st[1].Add(b), st[2].Add(c), st[3].Add(d)
	st[4], st[5], st[6], st[7] = st[4].Add(e), st[5].Add(f), st[6].Add(g), st[7].Add(h)
}

// 32 バイトの値 16 個（レーンごと）を、語ごとのベクトル 8 本に並べ替える（ビッグエンディアン）。
func loadWords(vals *[simdLanes][32]byte) (out [8]vec) {
	for j := 0; j < 8; j++ {
		var arr [simdLanes]uint32
		for l := 0; l < simdLanes; l++ {
			arr[l] = binary.BigEndian.Uint32(vals[l][4*j:])
		}
		out[j] = archsimd.LoadUint32x16(&arr)
	}
	return out
}

func storeWords(words *[8]vec, vals *[simdLanes][32]byte) {
	for j := 0; j < 8; j++ {
		var arr [simdLanes]uint32
		words[j].Store(&arr)
		for l := 0; l < simdLanes; l++ {
			binary.BigEndian.PutUint32(vals[l][4*j:], arr[l])
		}
	}
}

// 鍵ごとの HMAC の内側・外側の状態（鍵ブロックを 1 回圧縮した後の状態）
func padState(keyWords *[8]vec, pad uint32) [8]vec {
	padV := archsimd.BroadcastUint32x16(pad)
	var in [16]vec
	for j := 0; j < 8; j++ {
		in[j] = keyWords[j].Xor(padV)
	}
	for j := 8; j < 16; j++ {
		in[j] = padV // 鍵の後ろは 0 埋めなので、0 ^ pad
	}
	var st [8]vec
	for j := range st {
		st[j] = archsimd.BroadcastUint32x16(sha256H0[j])
	}
	compress16(&st, &in)
	return st
}

// keys（32B）ごとの HMAC を、値 vals（32B）に iters 回かけ続けた結果を返す。
func hmacChain16(keys, vals *[simdLanes][32]byte, iters int) [simdLanes][32]byte {
	kVecOnce.Do(initKVec)
	keyWords := loadWords(keys)
	ipad := padState(&keyWords, 0x36363636)
	opad := padState(&keyWords, 0x5c5c5c5c)
	v := loadWords(vals)

	// 32 バイトのメッセージの SHA-256 の末尾（0x80、0 埋め、全長 (64+32)*8 = 768 ビット）
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

// imageSignature と同じ値を、最大 16 個まとめて計算する。
func imageSignatures(keys [][]byte, imageIDs []string, exp int64) [][]byte {
	sigs := make([][]byte, len(keys))
	for start := 0; start < len(keys); start += simdLanes {
		end := min(start+simdLanes, len(keys))
		if end-start < simdMinLanes { // 16 レーンぶんの時間がかかるので、少ないときは従来の計算が速い
			for l := start; l < end; l++ {
				sigs[l] = imageSignature(keys[l], imageIDs[l], exp)
			}
			continue
		}
		var ks, vs [simdLanes][32]byte
		for l := start; l < end; l++ {
			copy(ks[l-start][:], keys[l])
			// 1 回目だけメッセージの長さが違うので、標準ライブラリで計算する（残りは 32 バイトの連鎖）
			mac := hmac.New(sha256.New, keys[l])
			mac.Write([]byte(strconv.FormatInt(exp, 10) + ":" + imageIDs[l]))
			copy(vs[l-start][:], mac.Sum(nil))
		}
		out := hmacChain16(&ks, &vs, imageSignatureIterations-1)
		for l := start; l < end; l++ {
			sig := make([]byte, 32)
			copy(sig, out[l-start][:])
			sigs[l] = sig
		}
	}
	return sigs
}
