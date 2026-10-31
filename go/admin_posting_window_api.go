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
		// advisory lock → conflict 判定 → 前の期間の ended_at → window_notifications / posting_windows の INSERT → pg_notify を
		// sql/95-open-posting-window-func.sql の関数で 1 往復にする（元は 7 往復）。pg_notify は commit 時に各 app へ届く
		id := uuid.NewString()
		var out struct {
			Code           int        `db:"code"`
			StartsAt       *time.Time `db:"starts_at"`
			OnTimeDeadline *time.Time `db:"on_time_deadline"`
		}
		if err := h.DB.GetContext(ctx, &out, `SELECT o_code AS code, o_starts_at AS starts_at, o_on_time_deadline AS on_time_deadline FROM open_posting_window($1, $2, $3, $4)`, region, localDate, id, seconds); err != nil {
			return err
		}
		if out.Code == 409 {
			return NewAPIError(http.StatusConflict, "posting_window_conflict")
		}
		return c.JSON(http.StatusCreated, openPostingWindowResponse{
			ID: id, Region: region, LocalDate: localDate.Format(time.DateOnly),
			StartsAt: *out.StartsAt, OnTimeDeadline: *out.OnTimeDeadline,
		})
	}
}
