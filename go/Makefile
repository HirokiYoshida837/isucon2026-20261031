.PHONY: isureal clean

# 画像署名を AVX-512 で 16 個同時に計算する（sha16_simd.go）。GOEXPERIMENT=simd が無いと従来の計算に戻る
isureal:
	GOEXPERIMENT=simd go build -o isureal cmd/isureal/main.go

clean:
	rm -f isureal
