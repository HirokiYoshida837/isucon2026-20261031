//go:build amd64

package isureal

//go:generate go run gen_sha16.go

// 16 レーンの HMAC-SHA256 の連鎖（sha16_avx512_amd64.s、gen_sha16.go の生成物）。AVX-512F が必要。
// ipad / opad は鍵ごとの内側・外側の状態、v は値。どれも [語][レーン]。v に HMAC を iters 回かけて上書きする。
//
//go:noescape
func hmacChain16AVX512(ipad, opad, v *[8][16]uint32, iters int)
