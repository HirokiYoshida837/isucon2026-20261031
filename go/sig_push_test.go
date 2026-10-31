package isureal

import (
	"crypto/sha256"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestSigBucketExpWithinReuseWindow(t *testing.T) {
	for _, now := range []int64{1793425733, 1793425800, 1793426099} {
		exp := sigBucketExp(sigBucketOf(time.Unix(now, 0)))
		lo, hi := now+int64(imageURLLifetime/time.Second)-sigBucketSeconds, now+int64(imageURLLifetime/time.Second)
		if exp <= lo || exp > hi {
			t.Fatalf("exp %d out of (%d, %d]", exp, lo, hi)
		}
		if sigBucketFromExp(exp) != sigBucketOf(time.Unix(now, 0)) {
			t.Fatalf("bucket round trip failed for %d", now)
		}
	}
	if sigBucketFromExp(sigBucketExp(10)+1) != -1 {
		t.Fatal("unaligned exp must not map to a bucket")
	}
}

func TestSigRowsRoundTripAndPushedURL(t *testing.T) {
	key := make([]byte, sha256.Size)
	for i := range key {
		key[i] = byte(i)
	}
	ids := []string{"0b2d7a7e-1111-4000-8000-000000000001", "x"}
	bucket := sigBucketOf(time.Now())
	exp := sigBucketExp(bucket)
	sigs := make([][32]byte, len(ids))
	for i, id := range ids {
		copy(sigs[i][:], imageSignature(key, id, exp))
	}
	gotIDs, gotSigs, err := decodeSigRows(encodeSigRows(nil, ids, sigs))
	if err != nil || len(gotIDs) != 2 || gotIDs[1] != "x" || gotSigs[0] != sigs[0] {
		t.Fatalf("round trip failed: %v %v", err, gotIDs)
	}
	pushedSigs.put(bucket, gotIDs, gotSigs)
	defer pushedSigs.dropBefore(bucket + 10)
	user := User{ImageKey: key}
	want := buildSignedURL(ids[0], exp, sigs[0][:])
	if got := signedImageURL(user, ids[0]); got != want {
		t.Fatalf("signedImageURL = %s, want %s", got, want)
	}
	if !strings.Contains(signedImageURL(user, "not-pushed"), "exp="+itoa(exp)) {
		t.Fatal("locally signed URL must use the aligned exp")
	}
}

func itoa(v int64) string { return strconv.FormatInt(v, 10) }

func withSigBucketSeconds(t *testing.T, s string) {
	old := sigBucketSeconds
	t.Cleanup(func() { sigBucketSeconds = old })
	setSigBucketSeconds(s)
}

func TestSigBucketSecondsFromEnv(t *testing.T) {
	for in, want := range map[string]int64{"": 300, "3600": 3600, "0": 300, "-5": 300, "abc": 300, "86400": 300, "21600": 21600} {
		withSigBucketSeconds(t, in)
		if sigBucketSeconds != want {
			t.Fatalf("%q -> %d, want %d", in, sigBucketSeconds, want)
		}
	}
}

func TestSigBucketLongBucket(t *testing.T) {
	withSigBucketSeconds(t, "3600")
	life := int64(imageURLLifetime / time.Second)
	for _, now := range []int64{1793422800, 1793425733, 1793426399} {
		b := sigBucketOf(time.Unix(now, 0))
		exp := sigBucketExp(b)
		if exp <= now+life-3600 || exp > now+life {
			t.Fatalf("exp %d out of (%d, %d]", exp, now+life-3600, now+life)
		}
		if sigBucketFromExp(exp) != b {
			t.Fatalf("bucket round trip failed for %d", now)
		}
	}
	// 300 秒境界の exp は 3600 秒のバケットに戻らない（先回りの署名を引かない）
	if sigBucketFromExp(1793425800+life) != -1 {
		t.Fatal("exp not aligned to 3600 must not map to a bucket")
	}
}

func TestSigBucketSecondsMatch(t *testing.T) {
	withSigBucketSeconds(t, "")
	if !sigBucketSecondsMatch("") || !sigBucketSecondsMatch("300") || sigBucketSecondsMatch("3600") || sigBucketSecondsMatch("x") {
		t.Fatal("default 300: mismatch check is wrong")
	}
	withSigBucketSeconds(t, "3600")
	if sigBucketSecondsMatch("") || sigBucketSecondsMatch("300") || !sigBucketSecondsMatch("3600") {
		t.Fatal("3600: an old sender (no seconds) or 300 must be rejected")
	}
}
