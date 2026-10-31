package isureal

import (
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// 診断用: 走行中に署名を自前で計算した（キャッシュに無かった）image_id を、走行ごとにファイルへ残す。
// 走行どうしで集合がどれだけ重なるかを見て、「前の走行の実績で署名を先に作る」案が成り立つかを判断する。
// 重なりは asia1 で `comm -12 <(sort a.txt) <(sort b.txt) | wc -l` などで見る。
// 保存先は env ISUREAL_SIGN_MISS_DIR（既定 /home/isucon/sign-miss。空で無効）。
const (
	signMissMax     = 400000
	signMissFlushAt = 150 * time.Second // 走行（約 100〜140 秒）が終わったころに書き出す
)

var (
	signHits   atomic.Int64
	signMisses atomic.Int64

	signMissMu    sync.Mutex
	signMissIDs   map[string]struct{}
	signMissStart time.Time
	signMissTimer *time.Timer
)

func noteSignHit() { signHits.Add(1) }

func noteSignMiss(imageID string) {
	signMisses.Add(1)
	if warmingUp.Load() { // 起動時のウォームアップは走行中の実績ではない
		return
	}
	signMissMu.Lock()
	if signMissIDs != nil && len(signMissIDs) < signMissMax {
		signMissIDs[imageID] = struct{}{}
	}
	signMissMu.Unlock()
}

// POST /api/initialize で呼ぶ。前の走行の分が残っていれば書き出し、新しい走行の記録を始める。
func startSignMissRecording() {
	dir := envOrDefault("ISUREAL_SIGN_MISS_DIR", "/home/isucon/sign-miss")
	if dir == "" {
		return
	}
	flushSignMiss(dir)
	signMissMu.Lock()
	signMissIDs = map[string]struct{}{}
	signMissStart = time.Now()
	if signMissTimer != nil {
		signMissTimer.Stop()
	}
	signMissTimer = time.AfterFunc(signMissFlushAt, func() { flushSignMiss(dir) })
	signMissMu.Unlock()
	signHits.Store(0)
	signMisses.Store(0)
}

func flushSignMiss(dir string) {
	signMissMu.Lock()
	ids, start := signMissIDs, signMissStart
	signMissIDs = nil
	signMissMu.Unlock()
	if ids == nil {
		return
	}
	list := make([]string, 0, len(ids))
	for id := range ids {
		list = append(list, id)
	}
	sort.Strings(list)
	path := filepath.Join(dir, start.Format("20060102-150405")+".txt")
	if err := os.MkdirAll(dir, 0o755); err == nil {
		err = os.WriteFile(path, []byte(strings.Join(list, "\n")+"\n"), 0o644)
		if err != nil {
			log.Printf("sign miss: write %s: %v", path, err)
		}
	}
	log.Printf("sign miss: 重複なし %d 個 hits=%d misses=%d -> %s", len(list), signHits.Load(), signMisses.Load(), path)
}
