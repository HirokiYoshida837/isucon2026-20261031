package isureal

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/labstack/echo/v5"
)

type getNotificationsResponse struct {
	ID              string    `json:"id"`
	PostingWindowID string    `json:"postingWindowId"`
	Region          string    `json:"region"`
	CreatedAt       time.Time `json:"createdAt"`
}

type getNotificationsRow struct {
	ID              *string    `db:"id"`
	PostingWindowID *string    `db:"posting_window_id"`
	Region          *string    `db:"region"`
	CreatedAt       *time.Time `db:"created_at"`
}

func getNotificationsHandler(h *HandlerContext) echo.HandlerFunc {
	return func(c *echo.Context) error {
		ctx := c.Request().Context()
		session := GetSession(c)
		userID := session.UserID
		var connectedAt time.Time
		if err := h.DB.GetContext(ctx, &connectedAt, `SELECT clock_timestamp()`); err != nil {
			return err
		}
		response := c.Response()
		response.Header().Set("Content-Type", "text/event-stream")
		response.WriteHeader(http.StatusOK)
		controller := http.NewResponseController(response)
		if err := controller.Flush(); err != nil {
			return nil
		}
		ticker := time.NewTicker(500 * time.Millisecond)
		defer ticker.Stop()
		for {
			var row struct {
				UserRegion string `db:"user_region"`
				getNotificationsRow
			}
			err := h.DB.GetContext(ctx, &row, `
    SELECT u.region AS user_region, n.id, n.posting_window_id, n.region, n.created_at
    FROM users u LEFT JOIN LATERAL (
      SELECT n.id, n.posting_window_id, w.region, n.created_at
      FROM notifications n JOIN posting_windows w ON w.id = n.posting_window_id
      WHERE n.user_id = u.id AND n.created_at > $2 AND w.ended_at IS NULL
      ORDER BY n.created_at, n.id LIMIT 1
    ) n ON true
    WHERE u.id = $1 AND u.status = 'active'
   `, userID, connectedAt)
			if err != nil {
				if !errors.Is(err, sql.ErrNoRows) && ctx.Err() == nil {
					c.Logger().Error("failed to get notification", "error", err)
				}
				return nil
			}
			if row.UserRegion != session.Region {
				return nil
			}
			if row.ID != nil {
				data, err := json.Marshal(getNotificationsResponse{
					ID: *row.ID, PostingWindowID: *row.PostingWindowID, Region: *row.Region, CreatedAt: *row.CreatedAt,
				})
				if err != nil {
					c.Logger().Error("failed to encode notification", "error", err)
					return nil
				}
				if _, err := fmt.Fprintf(response, "data: %s\n\n", data); err != nil {
					return nil
				}
				_ = controller.Flush()
				return nil
			}
			select {
			case <-ctx.Done():
				return nil
			case <-ticker.C:
			}
		}
	}
}
