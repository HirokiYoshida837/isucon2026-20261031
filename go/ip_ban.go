package isureal

import (
	"context"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/jmoiron/sqlx"
	"github.com/labstack/echo/v5"
)

const ipBanReloadInterval = 200 * time.Millisecond

type IPBanList struct {
	startOnce sync.Once
	snapshot  atomic.Pointer[ipBanSnapshot]
	local     sync.Map // in-app guard が直接入れた BAN: ip -> time.Time
}

func (l *IPBanList) addLocal(ip string, until time.Time) { l.local.Store(ip, until) }

func (l *IPBanList) clearLocal() {
	l.local.Range(func(k, _ any) bool { l.local.Delete(k); return true })
	l.snapshot.Store(nil)
}

type ipBanSnapshot struct {
	loadedAt time.Time
	bans     map[string]time.Time
}

type ipBan struct {
	IP          string    `db:"ip"`
	BannedUntil time.Time `db:"banned_until"`
}

// Start は起動時に同期で 1 回ロードし、以降は ipBanReloadInterval ごとにバックグラウンドで再読込する。
// 失敗時は前の snapshot を保持する（DB が未起動でも落ちない）。2 回目以降の呼び出しは何もしない。
func (l *IPBanList) Start(db *sqlx.DB) {
	l.startOnce.Do(func() {
		l.reload(db)
		go func() {
			t := time.NewTicker(ipBanReloadInterval)
			defer t.Stop()
			for range t.C {
				l.reload(db)
			}
		}()
	})
}

func (l *IPBanList) reload(db *sqlx.DB) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if loaded, err := loadIPBans(ctx, db); err == nil {
		l.snapshot.Store(loaded)
	}
}

func (l *IPBanList) Banned(ip string) bool {
	if v, ok := l.local.Load(ip); ok && time.Now().Before(v.(time.Time)) {
		return true
	}
	snapshot := l.snapshot.Load()
	if snapshot == nil {
		return false
	}
	until, ok := snapshot.bans[ip]
	return ok && time.Now().Before(until)
}

func loadIPBans(ctx context.Context, db *sqlx.DB) (*ipBanSnapshot, error) {
	var rows []ipBan
	if err := db.SelectContext(ctx, &rows, `SELECT ip, banned_until FROM ip_bans WHERE banned_until > clock_timestamp()`); err != nil {
		return nil, err
	}
	bans := make(map[string]time.Time, len(rows))
	for _, row := range rows {
		bans[row.IP] = row.BannedUntil
	}
	return &ipBanSnapshot{loadedAt: time.Now(), bans: bans}, nil
}

func extractClientIP(req *http.Request) string {
	if ip := req.Header.Get("X-Client-IP"); ip != "" {
		return ip
	}
	return echo.ExtractIPDirect()(req)
}

func IPBanMiddleware(h *HandlerContext) echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c *echo.Context) error {
			if h.IPBans.Banned(c.RealIP()) {
				return NewAPIError(http.StatusForbidden, "ip_banned")
			}
			return next(c)
		}
	}
}
