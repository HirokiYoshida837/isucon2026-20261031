package isureal

import (
	"context"
	"sync"
	"time"
)

// このリージョンの全画像の署名を、europe1 / america1 に先に作ってもらう（asia1 の CPU を走行中に使わない）。
// 対象の一覧（鍵と image_id）は起動時のウォームアップが DB から読んで置く。
// 署名の期限は今まで通り「頼んだ時刻 + 24h」。再利用期間（5 分）の間だけ使えるので、POST /api/initialize のたびに作り直す。
const (
	prefetchBatch       = 4000 // 1 回の依頼の件数（2 台に半分ずつ）
	prefetchConcurrency = 2    // 同時に投げる依頼の数（リージョン間の帯域を食いつぶさないため）
	prefetchBatchTime   = 5 * time.Second
)

var (
	prefetchMu     sync.Mutex
	prefetchList   []sigReq
	prefetchCancel context.CancelFunc
)

func setPrefetchList(list []sigReq) {
	prefetchMu.Lock()
	prefetchList = list
	prefetchMu.Unlock()
}

// 走行中の先読みがあれば止めて、新しく始める。頼めない・一覧が無いときは何もしない。
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
	go prefetchSigs(ctx, prefetchList)
}

func prefetchSigs(ctx context.Context, list []sigReq) {
	sem := make(chan struct{}, prefetchConcurrency)
	var wg sync.WaitGroup
	for start := 0; start < len(list); start += prefetchBatch {
		if ctx.Err() != nil || !signAvailable() {
			break
		}
		batch := list[start:min(start+prefetchBatch, len(list))]
		sem <- struct{}{}
		wg.Add(1)
		go func() {
			defer func() { <-sem; wg.Done() }()
			bctx, cancel := context.WithTimeout(ctx, prefetchBatchTime)
			defer cancel()
			exp := time.Now().Add(imageURLLifetime).Unix()
			signAcross(bctx, exp, batch, prefetchLocalPercent, func(r sigReq, sig []byte) { storePrefetched(r.key, r.imageID, exp, sig) })
		}()
	}
	wg.Wait()
}
