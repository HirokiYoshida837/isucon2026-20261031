package isureal

import (
	"fmt"
	"log"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// 計測用: 画像署名の HMAC 計算を呼び出し元ごとに数え、10 秒ごとに増えた分を 1 行ログに出す（#287）。
// 計算結果・ロック・レスポンスは変えない。ホットパスの追加は atomic.Add だけ。
// issue_*: signedImageURL（pushedSigs / issuedImageURLs ヒット、スカラー計算）
// prewarm_* / warm_* / push_*: prewarmSigs（走行中 / 起動時ウォームアップ中）と sig push（europe1・america1）の
// SIMD 16 レーンの回数・使ったレーンの合計・スカラーに回った本数
// v_*: GET /api/images（rejected 表 / verified 表 / pushedSigs と一致 / 既知の署名と違い HMAC なしで 403 / HMAC で成功 / 失敗）
const (
	sIssuePushed = iota
	sIssueCached
	sIssueHMAC
	sPrewarmBatch // batch, lanes, scalar の順に並べる（countSigBatch）
	sPrewarmLanes
	sPrewarmScalar
	sWarmBatch
	sWarmLanes
	sWarmScalar
	sPushBatch
	sPushLanes
	sPushScalar
	sVRejected
	sVKnown
	sVPushed
	sVKnownNG
	sVHMACOK
	sVHMACNG
	sigStatN
)

var sigStatNames = [sigStatN]string{"issue_pushed", "issue_cached", "issue_hmac",
	"prewarm_batch", "prewarm_lanes", "prewarm_scalar", "warm_batch", "warm_lanes", "warm_scalar",
	"push_batch", "push_lanes", "push_scalar",
	"v_rejected", "v_known", "v_pushed", "v_known_ng", "v_hmac_ok", "v_hmac_ng"}

var (
	sigStat     [sigStatN]atomic.Int64
	sigStatMu   sync.Mutex
	sigStatLast [sigStatN]int64
)

func countSig(i int) { sigStat[i].Add(1) }

// imageSignatures に n 本渡したときの内訳（16 本ずつ、端数が simdMinLanes 未満ならスカラー。sha16_simd.go と同じ規則）
func countSigBatch(batch int, n int) {
	full, rem := n/16, n%16
	lanes, scalar := full*16, rem
	if rem >= simdMinLanes {
		full, lanes, scalar = full+1, lanes+rem, 0
	}
	sigStat[batch].Add(int64(full))
	sigStat[batch+1].Add(int64(lanes))
	sigStat[batch+2].Add(int64(scalar))
}

// 前回から増えた分を 1 行出す。増えていなければ出さない。hmac は計算した連鎖の本数の合計（SIMD のレーンも 1 本と数える）
func flushSigStat(at string) {
	sigStatMu.Lock()
	defer sigStatMu.Unlock()
	var b strings.Builder
	var hmac, total int64
	for i := range sigStat {
		v := sigStat[i].Load()
		d := v - sigStatLast[i]
		sigStatLast[i] = v
		total += d
		switch i {
		case sIssueHMAC, sPrewarmLanes, sPrewarmScalar, sWarmLanes, sWarmScalar, sPushLanes, sPushScalar, sVHMACOK, sVHMACNG:
			hmac += d
		}
		fmt.Fprintf(&b, " %s=%d", sigStatNames[i], d)
	}
	if total > 0 {
		log.Printf("sigstat: at=%s hmac=%d%s", at, hmac, b.String())
	}
}

func startSigStat() {
	go func() {
		for range time.Tick(10 * time.Second) {
			flushSigStat("tick")
		}
	}()
}
