package isureal

import (
	"context"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/jmoiron/sqlx"
)

// isureal-guard（/opt/isureal-guard/src/guard.go）の判定をアプリ内に移したもの。
// 判定条件・閾値・BAN の SQL は guard と同じにする。

const (
	ipBanDuration   = 2 * time.Minute
	ipBanSQLTimeout = 5 * time.Second
)

type failureKind string

const (
	loginFailureKind     failureKind = "login"
	invalidImageKind     failureKind = "invalid_image"
	invalidSignatureKind failureKind = "invalid_signature"
)

// classifyFailure は、アクションログと同じ情報から BAN 対象の失敗を取り出す（guard の parseFailure と同じ条件）。
func classifyFailure(action string, status int, abuse string) failureKind {
	switch {
	case action == "login" && status == http.StatusUnauthorized:
		return loginFailureKind
	case action == "put_profile_image" && status == http.StatusBadRequest && abuse == string(invalidImageKind):
		return invalidImageKind
	case action == "get_image" && status == http.StatusForbidden && abuse == string(invalidSignatureKind):
		return invalidSignatureKind
	default:
		return ""
	}
}

// failureDetector は IP ごとに、集計期間内の失敗の時刻を持つ（guard の detector と同じ）。
type failureDetector struct {
	mu        sync.Mutex
	threshold int
	window    time.Duration
	failures  map[string][]time.Time
	lastSweep time.Time
}

func newFailureDetector(threshold int, window time.Duration) *failureDetector {
	return &failureDetector{threshold: threshold, window: window, failures: make(map[string][]time.Time)}
}

// observe は ip の失敗を記録し、BAN すべきなら true を返す。BAN したら数え直す。
func (d *failureDetector) observe(ip string, at time.Time) bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.sweep(at)
	kept := d.failures[ip][:0]
	for _, failedAt := range d.failures[ip] {
		if at.Sub(failedAt) < d.window {
			kept = append(kept, failedAt)
		}
	}
	kept = append(kept, at)
	if len(kept) >= d.threshold {
		delete(d.failures, ip)
		return true
	}
	d.failures[ip] = kept
	return false
}

// sweep は集計期間ごとに、期間内の失敗がなくなった IP を捨てる。
func (d *failureDetector) sweep(now time.Time) {
	if now.Sub(d.lastSweep) < d.window {
		return
	}
	d.lastSweep = now
	for ip, times := range d.failures {
		if now.Sub(times[len(times)-1]) >= d.window {
			delete(d.failures, ip)
		}
	}
}

func (d *failureDetector) reset() {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.failures = make(map[string][]time.Time)
	d.lastSweep = time.Time{}
}

// IPBanDetector は失敗を数え、閾値に達した IP を ip_bans に書く。
type IPBanDetector struct {
	db        *sqlx.DB
	bans      *IPBanList
	detectors map[failureKind]*failureDetector
}

func NewIPBanDetector(db *sqlx.DB, bans *IPBanList) *IPBanDetector {
	return &IPBanDetector{
		db:   db,
		bans: bans,
		detectors: map[failureKind]*failureDetector{
			loginFailureKind:     newFailureDetector(10, 10*time.Second),
			invalidImageKind:     newFailureDetector(2, 5*time.Second),
			invalidSignatureKind: newFailureDetector(3, 5*time.Second),
		},
	}
}

// Observe はレスポンス確定後に呼ぶ。BAN すべき IP は別ゴルーチンで ip_bans に書く（リクエストを遅らせないため）。
func (d *IPBanDetector) Observe(action string, status int, abuse string, ip string) {
	if d == nil || ip == "" {
		return
	}
	kind := classifyFailure(action, status, abuse)
	if kind == "" {
		return
	}
	detector := d.detectors[kind]
	if detector.observe(ip, time.Now()) {
		go d.ban(ip, kind)
	}
}

// ban は BAN の期限を DB の時刻で決める。すでに BAN されていれば遅い方の期限を残す（guard と同じ SQL）。
func (d *IPBanDetector) ban(ip string, kind failureKind) {
	ctx, cancel := context.WithTimeout(context.Background(), ipBanSQLTimeout)
	defer cancel()
	var until time.Time
	err := d.db.QueryRowContext(ctx, `
		INSERT INTO ip_bans (ip, banned_until)
		VALUES ($1, clock_timestamp() + make_interval(secs => $2))
		ON CONFLICT (ip) DO UPDATE SET banned_until = GREATEST(ip_bans.banned_until, EXCLUDED.banned_until)
		RETURNING banned_until
	`, ip, ipBanDuration.Seconds()).Scan(&until)
	if err != nil {
		log.Printf("ip ban failed: ip=%s reason=%s: %v", ip, kind, err)
		return
	}
	d.bans.add(ip, until)
}

// reset は数えた失敗を捨てる（POST /api/initialize 用。ip_bans は sql/reset.sql が TRUNCATE する）。
func (d *IPBanDetector) reset() {
	for _, detector := range d.detectors {
		detector.reset()
	}
}
