package isureal

import (
	"context"
	"sync/atomic"
	"time"

	"github.com/jmoiron/sqlx"
)

// isureal-guard の判定をアプリ内の goroutine で行う（ISUREAL_GUARD_INAPP=1）。
// 条件・期間は isureal-guard と同じ。

const guardBanDuration = 2 * time.Minute

type guardKind int

const (
	guardLogin guardKind = iota
	guardInvalidImage
	guardInvalidSignature
)

type guardRule struct {
	threshold int
	window    time.Duration
}

var guardRules = [...]guardRule{
	guardLogin:            {10, 10 * time.Second},
	guardInvalidImage:     {2, 5 * time.Second},
	guardInvalidSignature: {3, 5 * time.Second},
}

type guardEvent struct {
	kind  guardKind
	ip    string
	at    time.Time
	reset chan struct{}
}

type Guard struct {
	db       *sqlx.DB
	bans     *IPBanList
	events   chan guardEvent
	failures [len(guardRules)]map[string][]time.Time
}

var inAppGuard atomic.Pointer[Guard]

func startGuard(db *sqlx.DB, bans *IPBanList) {
	g := &Guard{db: db, bans: bans, events: make(chan guardEvent, 1024)}
	g.clear()
	inAppGuard.Store(g)
	go g.run()
}

func (g *Guard) clear() {
	for i := range g.failures {
		g.failures[i] = make(map[string][]time.Time)
	}
}

// guardObserve はリクエストを待たせない。バッファが満杯なら捨てる。
func guardObserve(kind guardKind, ip string) {
	g := inAppGuard.Load()
	if g == nil || ip == "" {
		return
	}
	select {
	case g.events <- guardEvent{kind: kind, ip: ip, at: time.Now()}:
	default:
	}
}

// guardReset は検出のカウントとローカルの BAN を捨てる（初期化用）。goroutine が処理し終えるまで待つ。
func guardReset() {
	g := inAppGuard.Load()
	if g == nil {
		return
	}
	done := make(chan struct{})
	g.events <- guardEvent{reset: done}
	<-done
}

func (g *Guard) run() {
	for ev := range g.events {
		if ev.reset != nil {
			g.clear()
			g.bans.clearLocal()
			close(ev.reset)
			continue
		}
		rule := guardRules[ev.kind]
		m := g.failures[ev.kind]
		kept := m[ev.ip][:0]
		for _, t := range m[ev.ip] {
			if ev.at.Sub(t) < rule.window {
				kept = append(kept, t)
			}
		}
		kept = append(kept, ev.at)
		if len(kept) < rule.threshold {
			m[ev.ip] = kept
			if len(m) > 10000 {
				for ip, ts := range m {
					if ev.at.Sub(ts[len(ts)-1]) >= rule.window {
						delete(m, ip)
					}
				}
			}
			continue
		}
		delete(m, ev.ip)
		g.bans.addLocal(ev.ip, time.Now().Add(guardBanDuration))
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		_, _ = g.db.ExecContext(ctx, `
			INSERT INTO ip_bans (ip, banned_until)
			VALUES ($1, clock_timestamp() + make_interval(secs => $2))
			ON CONFLICT (ip) DO UPDATE SET banned_until = GREATEST(ip_bans.banned_until, EXCLUDED.banned_until)
		`, ev.ip, guardBanDuration.Seconds())
		cancel()
	}
}
