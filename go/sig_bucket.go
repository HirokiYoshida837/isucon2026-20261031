package isureal

import (
	"crypto/subtle"
	"errors"
	"log"
	"strconv"
	"sync"
	"time"
)

// 署名の exp をバケット長の境界に揃える。exp = バケット番号 × sigBucketSeconds + 24h。
// 同じバケットの間は同じ exp・同じ署名になるので、europe1 / america1 が先回りで計算して asia1 に渡せる（sig_push_*.go）。
// exp は now+24h−バケット長 〜 now+24h に収まる（既定 300 秒 = #63 の 5 分再利用と同じ範囲）。
// バケット長は ISUREAL_SIG_BUCKET_SECONDS で起動時に 1 回だけ決める（setSigBucketSeconds。以後変えない）。
// 3 台で値が違うと asia1 は先回りの署名を受け取らず、自前で計算する。
var sigBucketSeconds = int64(signedURLReuse / time.Second)

const sigBucketSecondsMax = 6 * 3600

// NewServer の最初に 1 回だけ呼ぶ。空は既定値、数字でない・0 以下・上限超えは既定値に戻してログに出す
func setSigBucketSeconds(s string) {
	def := int64(signedURLReuse / time.Second)
	v := def
	if s != "" {
		n, err := strconv.ParseInt(s, 10, 64)
		if err != nil || n <= 0 || n > sigBucketSecondsMax {
			log.Printf("sig bucket: invalid ISUREAL_SIG_BUCKET_SECONDS=%q (1..%d); using %d", s, sigBucketSecondsMax, def)
		} else {
			v = n
		}
	}
	sigBucketSeconds = v
	log.Printf("sig bucket: %d seconds", v)
}

// 送る側が付けてきたバケット長が自分と同じか。付いていなければ旧版（既定値）とみなす
func sigBucketSecondsMatch(s string) bool {
	if s == "" {
		return sigBucketSeconds == int64(signedURLReuse/time.Second)
	}
	v, err := strconv.ParseInt(s, 10, 64)
	return err == nil && v == sigBucketSeconds
}

func sigBucketOf(now time.Time) int64 { return now.Unix() / sigBucketSeconds }

func sigBucketExp(bucket int64) int64 {
	return bucket*sigBucketSeconds + int64(imageURLLifetime/time.Second)
}

// exp からバケット番号を戻す。境界に揃っていない exp は -1
func sigBucketFromExp(exp int64) int64 {
	b := exp - int64(imageURLLifetime/time.Second)
	if b < 0 || b%sigBucketSeconds != 0 {
		return -1
	}
	return b / sigBucketSeconds
}

// europe1 / america1 から受け取った署名（asia1 が持つ）。バケット → image_id → 署名。
// 署名は (持ち主の鍵, image_id, exp) だけで決まり、持ち主の鍵は変わらないので image_id で引ける。
// 初期データの署名は initialize で変わらないので消さない。古いバケットは受信時に捨てる。
type pushedSigStore struct {
	mu      sync.RWMutex
	buckets map[int64]map[string][32]byte
}

var pushedSigs = &pushedSigStore{buckets: map[int64]map[string][32]byte{}}

func (s *pushedSigStore) get(bucket int64, imageID string) ([32]byte, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	sig, ok := s.buckets[bucket][imageID]
	return sig, ok
}

func (s *pushedSigStore) put(bucket int64, ids []string, sigs [][32]byte) {
	s.mu.Lock()
	defer s.mu.Unlock()
	m := s.buckets[bucket]
	if m == nil {
		m = make(map[string][32]byte, 1<<16)
		s.buckets[bucket] = m
	}
	for i, id := range ids {
		m[id] = sigs[i]
	}
}

// 現在より前のバケットを捨てる（メモリを現在と次の 2 バケット分に保つ）
func (s *pushedSigStore) dropBefore(bucket int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for b := range s.buckets {
		if b < bucket {
			delete(s.buckets, b)
		}
	}
}

func (s *pushedSigStore) counts() map[int64]int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make(map[int64]int, len(s.buckets))
	for b, m := range s.buckets {
		out[b] = len(m)
	}
	return out
}

// 送受信の形式: [1 バイト: image_id の長さ][image_id][32 バイト: 署名] の繰り返し
const sigRowMaxIDLen = 255

func encodeSigRows(buf []byte, ids []string, sigs [][32]byte) []byte {
	for i, id := range ids {
		buf = append(buf, byte(len(id)))
		buf = append(buf, id...)
		buf = append(buf, sigs[i][:]...)
	}
	return buf
}

func decodeSigRows(data []byte) ([]string, [][32]byte, error) {
	var ids []string
	var sigs [][32]byte
	for len(data) > 0 {
		n := int(data[0])
		if n == 0 || len(data) < 1+n+32 {
			return nil, nil, errors.New("broken sig rows")
		}
		ids = append(ids, string(data[1:1+n]))
		var sig [32]byte
		copy(sig[:], data[1+n:1+n+32])
		sigs = append(sigs, sig)
		data = data[1+n+32:]
	}
	return ids, sigs, nil
}

// europe1 / america1 から受け取った署名と一致するか（GET /api/images の検証で HMAC を省く。鍵と生存確認は呼び出し側が毎回見る）
func pushedSigMatches(imageID string, exp int64, sig []byte) bool {
	b := sigBucketFromExp(exp)
	if b < 0 {
		return false
	}
	want, ok := pushedSigs.get(b, imageID)
	return ok && subtle.ConstantTimeCompare(want[:], sig) == 1
}
