//go:build !(goexperiment.simd && amd64)

package isureal

// GOEXPERIMENT=simd でない・amd64 でないビルドでは使わない（prewarmSigs が何もしない）。
const simdMinLanes = 3

func simdAvailable() bool { return false }

func imageSignatures(keys [][]byte, imageIDs []string, exp int64) [][]byte {
	sigs := make([][]byte, len(keys))
	for i := range keys {
		sigs[i] = imageSignature(keys[i], imageIDs[i], exp)
	}
	return sigs
}
