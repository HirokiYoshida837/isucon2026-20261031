package isureal

import (
	"context"
	"database/sql"
	"errors"
	"log"
	"sync"
	"sync/atomic"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jmoiron/sqlx"
)

const (
	notificationRevalidateEvery = 5 * time.Second // 接続中ユーザーの status / region を見直す間隔。ポーリング間隔を縮めても 5 秒のまま
	notificationClockSamples    = 5               // DB 時刻のずれを測る回数。往復が最短だったものを採る
	notificationClockSyncEach   = 10 * time.Second

	postingWindowChannel = "posting_window_open" // 投稿期間の開始時に管理 API が pg_notify する（payload はリージョン）
)

// notificationWaiter は SSE で通知を待っている 1 接続
type notificationWaiter struct {
	userID        string
	connectedAt   time.Time                      // DB 時刻（Hub.now）。これより後に作られた通知だけ届ける
	checkedWindow string                         // この投稿期間については通知を探し終えた（行が無かった）
	ch            chan *getNotificationsResponse // nil を受け取ったら接続を閉じる
}

// NotificationHub は接続中の SSE に通知を配る。
// 投稿期間の開始は LISTEN/NOTIFY で受け、間隔の長いポーリングは取りこぼし対策の保険として残す。
type NotificationHub struct {
	db     *sqlx.DB
	region string

	connConfig     *pgx.ConnConfig // LISTEN 専用接続用
	kickCh         chan struct{}
	pollInterval   time.Duration
	revalidateEach int // ポーリング何回ごとに revalidate するか
	mu             sync.Mutex
	waiters        map[*notificationWaiter]struct{}
	ticks          int

	clockOffset atomic.Int64 // DB 時刻 - アプリ時刻（ns）。接続ごとに clock_timestamp() を DB へ聞かないための補正
}

func NewNotificationHub(db *sqlx.DB, connConfig *pgx.ConnConfig, region string, pollInterval time.Duration) *NotificationHub {
	each := int(notificationRevalidateEvery / pollInterval)
	if each < 1 {
		each = 1
	}
	listenConfig := connConfig.Copy()
	listenConfig.Tracer = nil
	h := &NotificationHub{db: db, connConfig: listenConfig, kickCh: make(chan struct{}, 1), region: region, pollInterval: pollInterval, revalidateEach: each, waiters: make(map[*notificationWaiter]struct{})}
	if err := h.syncClock(context.Background()); err != nil {
		log.Printf("notification hub: clock sync: %v", err)
	}
	return h
}

// now は DB の clock_timestamp() 相当の現在時刻（アプリ時刻 + 測ったずれ）
func (h *NotificationHub) now() time.Time {
	return time.Now().Add(time.Duration(h.clockOffset.Load()))
}

// syncClock は DB とアプリの時刻差を測る。往復が最短の標本を採る（誤差は往復の半分以内）
func (h *NotificationHub) syncClock(ctx context.Context) error {
	best := time.Duration(-1)
	var offset time.Duration
	for i := 0; i < notificationClockSamples; i++ {
		var dbNow time.Time
		t0 := time.Now()
		if err := h.db.GetContext(ctx, &dbNow, `SELECT clock_timestamp()`); err != nil {
			return err
		}
		rtt := time.Since(t0)
		if best < 0 || rtt < best {
			best = rtt
			offset = dbNow.Sub(t0.Add(rtt / 2))
		}
	}
	h.clockOffset.Store(int64(offset))
	return nil
}

func (h *NotificationHub) add(userID string, connectedAt time.Time) *notificationWaiter {
	w := &notificationWaiter{userID: userID, connectedAt: connectedAt, ch: make(chan *getNotificationsResponse, 1)}
	h.mu.Lock()
	h.waiters[w] = struct{}{}
	h.mu.Unlock()
	return w
}

func (h *NotificationHub) remove(w *notificationWaiter) {
	h.mu.Lock()
	delete(h.waiters, w)
	h.mu.Unlock()
}

// reset は初期化で呼ぶ。投稿期間が作り直されるので「探し終えた」印を消す
func (h *NotificationHub) reset() {
	h.mu.Lock()
	for w := range h.waiters {
		w.checkedWindow = ""
	}
	h.mu.Unlock()
	h.kick()
}

// kick は次のポーリングを待たずに poll させる
func (h *NotificationHub) kick() {
	select {
	case h.kickCh <- struct{}{}:
	default:
	}
}

// listen は NOTIFY を待って kick する。切れたら張り直し、張り直したら取りこぼし分を探すために kick する
func (h *NotificationHub) listen(ctx context.Context) {
	for ctx.Err() == nil {
		if err := h.listenOnce(ctx); err != nil && ctx.Err() == nil {
			log.Printf("notification hub: listen: %v", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(time.Second):
		}
	}
}

func (h *NotificationHub) listenOnce(ctx context.Context) error {
	conn, err := pgx.ConnectConfig(ctx, h.connConfig)
	if err != nil {
		return err
	}
	defer conn.Close(context.Background())
	if _, err := conn.Exec(ctx, "LISTEN "+postingWindowChannel); err != nil {
		return err
	}
	h.kick()
	for {
		n, err := conn.WaitForNotification(ctx)
		if err != nil {
			return err
		}
		if n.Payload == h.region {
			h.kick()
		}
	}
}

func (h *NotificationHub) Run(ctx context.Context) {
	go h.listen(ctx)
	ticker := time.NewTicker(h.pollInterval)
	defer ticker.Stop()
	lastSync := time.Now()
	for {
		tick := false
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			tick = true
		case <-h.kickCh:
		}
		if time.Since(lastSync) >= notificationClockSyncEach {
			lastSync = time.Now()
			if err := h.syncClock(ctx); err != nil && ctx.Err() == nil {
				log.Printf("notification hub: clock sync: %v", err)
			}
		}
		if err := h.poll(ctx, tick); err != nil && ctx.Err() == nil {
			log.Printf("notification hub: %v", err)
		}
	}
}

func (h *NotificationHub) poll(ctx context.Context, tick bool) error {
	h.mu.Lock()
	n := len(h.waiters)
	h.mu.Unlock()
	if n == 0 {
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	var window struct {
		ID       string    `db:"id"`
		Region   string    `db:"region"`
		StartsAt time.Time `db:"starts_at"`
	}
	err := h.db.GetContext(ctx, &window, `SELECT id, region, starts_at FROM posting_windows WHERE region = $1 AND ended_at IS NULL`, h.region)
	hasWindow := err == nil
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}

	if hasWindow {
		// この投稿期間の通知をまだ探していない、かつ接続前に始まった期間ではない（通知の created_at = starts_at > connectedAt）
		h.mu.Lock()
		targets := make(map[string][]*notificationWaiter)
		for w := range h.waiters {
			if w.checkedWindow != window.ID && window.StartsAt.After(w.connectedAt) {
				targets[w.userID] = append(targets[w.userID], w)
			}
		}
		h.mu.Unlock()
		if len(targets) > 0 {
			userIDs := make([]string, 0, len(targets))
			for id := range targets {
				userIDs = append(userIDs, id)
			}
			var rows []struct {
				UserID string `db:"user_id"`
				getNotificationsResponse
			}
			err := h.db.SelectContext(ctx, &rows, `
    SELECT n.user_id, n.id, n.posting_window_id, w.region, n.created_at
    FROM notifications n JOIN posting_windows w ON w.id = n.posting_window_id
    WHERE n.posting_window_id = $1 AND n.user_id = ANY($2) AND w.ended_at IS NULL
   `, window.ID, userIDs)
			if err != nil {
				return err
			}
			h.mu.Lock()
			for _, row := range rows {
				for _, w := range targets[row.UserID] {
					if _, ok := h.waiters[w]; !ok || !row.CreatedAt.After(w.connectedAt) {
						continue
					}
					resp := row.getNotificationsResponse
					select {
					case w.ch <- &resp:
					default:
					}
					delete(h.waiters, w)
				}
			}
			for _, ws := range targets {
				for _, w := range ws {
					w.checkedWindow = window.ID
				}
			}
			h.mu.Unlock()
		}
	}

	// 接続中ユーザーの退会・リージョン変更を定期的に見直す（元の実装は毎ポーリングで見ていた）
	if !tick {
		return nil
	}
	h.ticks++
	if h.ticks%h.revalidateEach == 0 {
		return h.revalidate(ctx)
	}
	return nil
}

func (h *NotificationHub) revalidate(ctx context.Context) error {
	h.mu.Lock()
	byUser := make(map[string][]*notificationWaiter)
	for w := range h.waiters {
		byUser[w.userID] = append(byUser[w.userID], w)
	}
	h.mu.Unlock()
	if len(byUser) == 0 {
		return nil
	}
	userIDs := make([]string, 0, len(byUser))
	for id := range byUser {
		userIDs = append(userIDs, id)
	}
	var alive []string
	if err := h.db.SelectContext(ctx, &alive, `SELECT id FROM users WHERE id = ANY($1) AND status = 'active' AND region = $2`, userIDs, h.region); err != nil {
		return err
	}
	for _, id := range alive {
		delete(byUser, id)
	}
	h.mu.Lock()
	for _, ws := range byUser {
		for _, w := range ws {
			if _, ok := h.waiters[w]; !ok {
				continue
			}
			select {
			case w.ch <- nil:
			default:
			}
			delete(h.waiters, w)
		}
	}
	h.mu.Unlock()
	return nil
}
