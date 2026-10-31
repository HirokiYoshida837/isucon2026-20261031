package isureal

import "testing"

func TestKnownSigCache(t *testing.T) {
	verifiedImageSigs.reset()
	defer verifiedImageSigs.reset()
	key := make([]byte, 32)
	sig := imageSignature(key, "img", 1793415600)
	verifiedImageSigs.add("img", 1793415600, sig)
	if !knownSigMatches("img", 1793415600, sig) {
		t.Fatal("正しい署名が一致しない")
	}
	bad := append([]byte(nil), sig...)
	bad[0] ^= 1
	if knownSigMatches("img", 1793415600, bad) {
		t.Fatal("違う署名が一致した")
	}
	if knownSigMatches("img", 1793415601, sig) {
		t.Fatal("別の exp が一致した")
	}
	// 上限で 1 世代前に回っても引ける
	for i := 0; i < verifiedSigMaxEntries; i++ {
		verifiedImageSigs.add("x", int64(i), sig)
	}
	if !knownSigMatches("img", 1793415600, sig) {
		t.Fatal("1 世代前に回った署名が引けない")
	}
}
