package isureal

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSignMissRecordingWritesSortedUniqueIDs(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("ISUREAL_SIGN_MISS_DIR", dir)
	startSignMissRecording()
	defer func() { signMissTimer.Stop(); signMissMu.Lock(); signMissIDs = nil; signMissMu.Unlock() }()

	noteSignMiss("b")
	noteSignMiss("a")
	noteSignMiss("b") // 重複は 1 つにまとまる
	noteSignHit()
	flushSignMiss(dir)

	files, _ := filepath.Glob(filepath.Join(dir, "*.txt"))
	if len(files) != 1 {
		t.Fatalf("ファイルが %d 個", len(files))
	}
	data, _ := os.ReadFile(files[0])
	if got := strings.TrimSpace(string(data)); got != "a\nb" {
		t.Fatalf("内容 %q", got)
	}
	if signMisses.Load() != 3 || signHits.Load() != 1 {
		t.Errorf("hits=%d misses=%d", signHits.Load(), signMisses.Load())
	}
}
