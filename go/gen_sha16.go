//go:build ignore

// sha16_avx512_amd64.s を生成する。go/ で `go generate ./...`（または `go run gen_sha16.go`）。
//
// 16 レーン（zmm の 16 個の uint32）で独立した HMAC-SHA256 の連鎖を計算する。
// HMAC の 1 回 = 内側の圧縮 1 回 + 外側の圧縮 1 回（鍵ブロックの圧縮は ipad / opad の状態として前計算済み）。
// どちらのブロックも「32 バイトの値 + パディング（0x80000000, 0 × 6, 768）」なので、後半 8 語は定数として畳み込む。
//
// レジスタ: Z0〜Z7 = 状態 a〜h（ラウンドごとに名前を付け替える）、Z8〜Z23 = メッセージ W の 16 語の窓、
// Z24〜Z29 = 一時。AX = ipad、BX = opad、CX = v、DX = 残り回数、R8 = 定数表。
package main

import (
	"bytes"
	"fmt"
	"math/bits"
	"os"
)

var k = [64]uint32{
	0x428a2f98, 0x71374491, 0xb5c0fbcf, 0xe9b5dba5, 0x3956c25b, 0x59f111f1, 0x923f82a4, 0xab1c5ed5,
	0xd807aa98, 0x12835b01, 0x243185be, 0x550c7dc3, 0x72be5d74, 0x80deb1fe, 0x9bdc06a7, 0xc19bf174,
	0xe49b69c1, 0xefbe4786, 0x0fc19dc6, 0x240ca1cc, 0x2de92c6f, 0x4a7484aa, 0x5cb0a9dc, 0x76f988da,
	0x983e5152, 0xa831c66d, 0xb00327c8, 0xbf597fc7, 0xc6e00bf3, 0xd5a79147, 0x06ca6351, 0x14292967,
	0x27b70a85, 0x2e1b2138, 0x4d2c6dfc, 0x53380d13, 0x650a7354, 0x766a0abb, 0x81c2c92e, 0x92722c85,
	0xa2bfe8a1, 0xa81a664b, 0xc24b8b70, 0xc76c51a3, 0xd192e819, 0xd6990624, 0xf40e3585, 0x106aa070,
	0x19a4c116, 0x1e376c08, 0x2748774c, 0x34b0bcb5, 0x391c0cb3, 0x4ed8aa4a, 0x5b9cca4f, 0x682e6ff3,
	0x748f82ee, 0x78a5636f, 0x84c87814, 0x8cc70208, 0x90befffa, 0xa4506ceb, 0xbef9a3f7, 0xc67178f2,
}

var out bytes.Buffer

func p(format string, a ...any) { fmt.Fprintf(&out, format+"\n", a...) }

// VPTERNLOGD の imm8: ビット番号 (A<<2)|(B<<1)|C の位置に f(A,B,C) を置く。
// Go の書き方は `VPTERNLOGD $imm, C, B, A`（A が出力先を兼ねる）。
func ternImm(f func(a, b, c uint8) uint8) uint8 {
	var imm uint8
	for i := uint8(0); i < 8; i++ {
		imm |= (f(i>>2&1, i>>1&1, i&1) & 1) << i
	}
	return imm
}

var (
	immXor3 = ternImm(func(a, b, c uint8) uint8 { return a ^ b ^ c })       // 0x96
	immCh   = ternImm(func(a, b, c uint8) uint8 { return a&b | ^a&c })      // 0xca: A=e, B=f, C=g
	immMaj  = ternImm(func(a, b, c uint8) uint8 { return a&b | a&c | b&c }) // 0xe8: A=a, B=b, C=c
)

// 定数表（ブロードキャストで読む 4 バイトずつ）
var consts []uint32

func constOff(v uint32) int {
	for i, c := range consts {
		if c == v {
			return 4 * i
		}
	}
	consts = append(consts, v)
	return 4 * (len(consts) - 1)
}

func z(i int) string { return fmt.Sprintf("Z%d", i) }

const t0, t1, t2, t3, t4, t5 = "Z24", "Z25", "Z26", "Z27", "Z28", "Z29"

func sigma0(x uint32) uint32 { return bits.RotateLeft32(x, -7) ^ bits.RotateLeft32(x, -18) ^ x>>3 }
func sigma1(x uint32) uint32 { return bits.RotateLeft32(x, -17) ^ bits.RotateLeft32(x, -19) ^ x>>10 }

// x の σ を dst に（VPRORD r1, VPRORD r2, VPSRLD s を 3 項 xor）
func emitSigma(x, dst, u, w string, r1, r2, s int) {
	p("\tVPRORD $%d, %s, %s", r1, x, dst)
	p("\tVPRORD $%d, %s, %s", r2, x, u)
	p("\tVPSRLD $%d, %s, %s", s, x, w)
	p("\tVPTERNLOGD $0x%02x, %s, %s, %s", immXor3, w, u, dst)
}

// 1 ブロックの圧縮。pad の指す状態から始め、結果（状態 + pad）を Z8〜Z15 に置く。
// 入力の W[0..7] は Z8〜Z15、W[8..15] はパディングの定数。
func emitCompress(pad string) {
	var wConst [64]bool
	var wVal [64]uint32
	for i := 8; i < 16; i++ {
		wConst[i] = true
	}
	wVal[8], wVal[15] = 0x80000000, 768
	wreg := func(i int) string { return z(8 + i%16) }

	for j := 0; j < 8; j++ {
		p("\tVMOVDQU32 %d(%s), %s", 64*j, pad, z(j))
	}
	for i := 0; i < 64; i++ {
		if i >= 16 {
			// W[i] = W[i-16] + σ0(W[i-15]) + W[i-7] + σ1(W[i-2])。定数の項はまとめて 1 回で足す
			p("\t// W[%d]", i)
			var c uint32
			var regs []string
			if wConst[i-16] {
				c += wVal[i-16]
			} else {
				regs = append(regs, wreg(i-16))
			}
			if wConst[i-15] {
				c += sigma0(wVal[i-15])
			} else {
				emitSigma(wreg(i-15), t3, t4, t5, 7, 18, 3)
				regs = append(regs, t3)
			}
			if wConst[i-7] {
				c += wVal[i-7]
			} else {
				regs = append(regs, wreg(i-7))
			}
			if wConst[i-2] {
				c += sigma1(wVal[i-2])
			} else {
				emitSigma(wreg(i-2), t0, t1, t2, 17, 19, 10)
				regs = append(regs, t0)
			}
			dst := wreg(i)
			if len(regs) == 0 {
				panic("W が全部定数")
			}
			if regs[0] == dst {
				regs = regs[1:]
			} else if len(regs) == 1 {
				p("\tVMOVDQA32 %s, %s", regs[0], dst)
				regs = nil
			} else {
				p("\tVPADDD %s, %s, %s", regs[1], regs[0], dst)
				regs = regs[2:]
			}
			for _, r := range regs {
				p("\tVPADDD %s, %s, %s", r, dst, dst)
			}
			if c != 0 {
				p("\tVPADDD.BCST %d(R8), %s, %s // 定数 0x%08x", constOff(c), dst, dst, c)
			}
		}
		a, b, c, d := z((0-i)&7), z((1-i)&7), z((2-i)&7), z((3-i)&7)
		e, f, g, h := z((4-i)&7), z((5-i)&7), z((6-i)&7), z((7-i)&7)
		p("\t// ラウンド %d", i)
		if wConst[i] {
			p("\tVPADDD.BCST %d(R8), %s, %s // K+W = 0x%08x", constOff(k[i]+wVal[i]), h, h, k[i]+wVal[i])
		} else {
			p("\tVPADDD %s, %s, %s", wreg(i), h, h)
			p("\tVPADDD.BCST %d(R8), %s, %s // K", constOff(k[i]), h, h)
		}
		// Σ1(e) = ROTR6 ^ ROTR11 ^ ROTR25
		p("\tVPRORD $6, %s, %s", e, t0)
		p("\tVPRORD $11, %s, %s", e, t1)
		p("\tVPRORD $25, %s, %s", e, t2)
		p("\tVPTERNLOGD $0x%02x, %s, %s, %s", immXor3, t2, t1, t0)
		p("\tVPADDD %s, %s, %s", t0, h, h)
		// Ch(e, f, g)
		p("\tVMOVDQA32 %s, %s", e, t1)
		p("\tVPTERNLOGD $0x%02x, %s, %s, %s", immCh, g, f, t1)
		p("\tVPADDD %s, %s, %s", t1, h, h)
		p("\tVPADDD %s, %s, %s", h, d, d)
		// Σ0(a) = ROTR2 ^ ROTR13 ^ ROTR22
		p("\tVPRORD $2, %s, %s", a, t0)
		p("\tVPRORD $13, %s, %s", a, t1)
		p("\tVPRORD $22, %s, %s", a, t2)
		p("\tVPTERNLOGD $0x%02x, %s, %s, %s", immXor3, t2, t1, t0)
		// Maj(a, b, c)
		p("\tVMOVDQA32 %s, %s", a, t1)
		p("\tVPTERNLOGD $0x%02x, %s, %s, %s", immMaj, c, b, t1)
		p("\tVPADDD %s, %s, %s", t0, h, h)
		p("\tVPADDD %s, %s, %s", t1, h, h)
	}
	// 64 ラウンドで名前は一周して a〜h = Z0〜Z7 に戻る
	for j := 0; j < 8; j++ {
		p("\tVPADDD %d(%s), %s, %s", 64*j, pad, z(j), z(8+j))
	}
}

func main() {
	p("// Code generated by gen_sha16.go; DO NOT EDIT.")
	p("// 生成物。編集しない（go/gen_sha16.go を直して go generate で作り直す）。")
	p("")
	p("//go:build amd64")
	p("")
	p("#include \"textflag.h\"")
	p("")
	p("// func hmacChain16AVX512(ipad, opad, v *[8][16]uint32, iters int)")
	p("// Ch: imm 0x%02x、Maj: imm 0x%02x、3 項 xor: imm 0x%02x（gen_sha16.go の ternImm で導出）", immCh, immMaj, immXor3)
	p("TEXT ·hmacChain16AVX512(SB), NOSPLIT, $0-32")
	p("\tMOVQ ipad+0(FP), AX")
	p("\tMOVQ opad+8(FP), BX")
	p("\tMOVQ v+16(FP), CX")
	p("\tMOVQ iters+24(FP), DX")
	p("\tLEAQ sha16C<>(SB), R8")
	for j := 0; j < 8; j++ {
		p("\tVMOVDQU32 %d(CX), %s", 64*j, z(8+j))
	}
	p("\tTESTQ DX, DX")
	p("\tJLE done")
	p("loop:")
	p("\t// 内側: ipad の状態に v（32 バイト）とパディング")
	emitCompress("AX")
	p("\t// 外側: opad の状態に内側の結果（32 バイト）とパディング")
	emitCompress("BX")
	p("\tDECQ DX")
	p("\tJNZ loop")
	p("done:")
	for j := 0; j < 8; j++ {
		p("\tVMOVDQU32 %s, %d(CX)", z(8+j), 64*j)
	}
	p("\tVZEROUPPER")
	p("\tRET")
	p("")
	for i, c := range consts {
		p("DATA sha16C<>+%d(SB)/4, $0x%08x", 4*i, c)
	}
	p("GLOBL sha16C<>(SB), RODATA|NOPTR, $%d", 4*len(consts))
	if err := os.WriteFile("sha16_avx512_amd64.s", out.Bytes(), 0o644); err != nil {
		panic(err)
	}
}
