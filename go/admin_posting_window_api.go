package isureal

import (
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/labstack/echo/v5"
)

type openPostingWindowRequest struct {
	Region        string `json:"region"`
	LocalDate     string `json:"localDate"`
	OnTimeSeconds *int64 `json:"onTimeSeconds"`
}

type openPostingWindowResponse struct {
	ID             string    `json:"id"`
	Region         string    `json:"region"`
	LocalDate      string    `json:"localDate"`
	StartsAt       time.Time `json:"startsAt"`
	OnTimeDeadline time.Time `json:"onTimeDeadline"`
}

func openPostingWindowHandler(h *HandlerContext) echo.HandlerFunc {
	return func(c *echo.Context) error {
		var input openPostingWindowRequest
		if err := c.Bind(&input); err != nil {
			return err
		}
		region := input.Region
		localDate, err := time.Parse(time.DateOnly, input.LocalDate)
		if !validRegion(region) || err != nil {
			return NewAPIError(http.StatusBadRequest, "invalid_posting_window")
		}
		seconds := int64(24 * 60 * 60)
		if input.OnTimeSeconds != nil {
			seconds = *input.OnTimeSeconds
		}
		ctx := c.Request().Context()
		tx, err := h.DB.BeginTxx(ctx, nil)
		if err != nil {
			return err
		}
		defer tx.Rollback()
		// users のテーブルロックは外す（全書き込みを止めていた）。同じリージョンの同時オープンだけ advisory lock で直列にする
		if _, err := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtext('posting_window_open:' || $1::text))`, region); err != nil {
			return err
		}
		var state struct {
			Conflict bool      `db:"conflict"`
			StartsAt time.Time `db:"starts_at"`
		}
		if err := tx.GetContext(ctx, &state, `SELECT EXISTS(SELECT 1 FROM posting_windows WHERE region = $1 AND local_date >= $2) AS conflict, clock_timestamp() AS starts_at`, region, localDate); err != nil {
			return err
		}
		if state.Conflict {
			return NewAPIError(http.StatusConflict, "posting_window_conflict")
		}
		startsAt := state.StartsAt
		if _, err := tx.ExecContext(ctx, `UPDATE posting_windows SET ended_at = $2 WHERE region = $1 AND ended_at IS NULL`, region, startsAt); err != nil {
			return err
		}
		var window PostingWindow
		if err := tx.GetContext(ctx, &window, `
   WITH notified AS (
     INSERT INTO window_notifications (id, user_id, posting_window_id, created_at)
     SELECT gen_random_uuid()::text, id, $2::varchar, $4::timestamptz FROM users WHERE region = $1 AND status = 'active'
   )
   INSERT INTO posting_windows (id, region, local_date, starts_at, on_time_deadline)
   VALUES ($2, $1, $3, $4, $5)
   RETURNING id, region, local_date, starts_at, on_time_deadline, ended_at
  `, region, uuid.NewString(), localDate, startsAt, startsAt.Add(time.Duration(seconds)*time.Second)); err != nil {
			return err
		}
		// commit 時に各 app へ届く（LISTEN している NotificationHub が即ポーリングする）
		if _, err := tx.ExecContext(ctx, `SELECT pg_notify($1, $2)`, postingWindowChannel, region); err != nil {
			return err
		}
		if err := tx.Commit(); err != nil {
			return err
		}
		return c.JSON(http.StatusCreated, openPostingWindowResponse{
			ID: window.ID, Region: window.Region, LocalDate: window.LocalDate.Format(time.DateOnly),
			StartsAt: window.StartsAt, OnTimeDeadline: window.OnTimeDeadline,
		})
	}
}
