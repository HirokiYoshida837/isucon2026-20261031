package isureal

import (
	"context"
	"log"
	"sync"
	"time"
)

// 新しい投稿の画像の署名を、走行の準備の間に作り直しておく（走行中の署名の計算を減らす）。
// 一覧（鍵と image_id）は起動時のウォームアップが DB から読んで置く。
// 署名の期限は今まで通り「作った時刻 + 24h」で、再利用期間（5 分）の間だけ使える。
// 前回（65 万件を europe1 / america1 に頼んだ版）は走行と CPU を取り合って点が下がったので、
// 件数を絞り（既定 4 万件。env ISUREAL_PREFETCH_MAX、0 で無効）、asia1 は 1 ワーカーだけにする。
const prefetchBatch = 2000

var (
	prefetchMax    = envInt("ISUREAL_PREFETCH_MAX", 40000)
	prefetchMu     sync.Mutex
	prefetchList   []sigReq
	prefetchCancel context.CancelFunc
)

func setPrefetchList(list []sigReq) {
	prefetchMu.Lock()
	prefetchList = list[:min(len(list), prefetchMax)]
	prefetchMu.Unlock()
}

// 走行中の先読みがあれば止めて、新しく始める。asia2 に頼めないとき・一覧が無いときは何もしない
// （asia1 だけで抱えると走行と取り合うため）。
func startPrefetch() {
	prefetchMu.Lock()
	defer prefetchMu.Unlock()
	if prefetchCancel != nil {
		prefetchCancel()
		prefetchCancel = nil
	}
	if len(prefetchList) == 0 || !signAvailable() {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	prefetchCancel = cancel
	list := prefetchList
	go prefetchSigs(ctx, list, signLocalPercent, 1, false) // asia1 は 1 ワーカー（もう 1 コアはアプリに残す）
}

// list を prefetchBatch 件ずつ、localPercent % を asia1 で、残りを peer で計算してキャッシュに入れる。
// peer が途中で使えなくなったとき、fallbackLocal なら残りを自前で作り（起動時）、そうでなければ止める（初期化時）。
func prefetchSigs(ctx context.Context, list []sigReq, localPercent, localWorkers int, fallbackLocal bool) {
	began := time.Now()
	for start := 0; start < len(list); start += prefetchBatch {
		if ctx.Err() != nil {
			return
		}
		batch := list[start:min(start+prefetchBatch, len(list))]
		exp := time.Now().Add(imageURLLifetime).Unix()
		signAcross(ctx, exp, batch, localPercent, localWorkers, func(r sigReq, sig []byte) { storeIssued(r.key, r.imageID, exp, sig) })
		if localPercent < 100 && !signAvailable() {
			if !fallbackLocal {
				break
			}
			localPercent = 100
		}
	}
	log.Printf("sign prefetch: 対象 %d 件 %s（asia1 %d%%）", len(list), time.Since(began).Round(time.Millisecond), localPercent)
}
