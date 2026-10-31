package isureal

import (
	"database/sql"
	"errors"
	"net/http"

	"github.com/labstack/echo/v5"
)

func deleteAccountHandler(h *HandlerContext) echo.HandlerFunc {
	return func(c *echo.Context) error {
		ctx := c.Request().Context()
		userID := GetSession(c).UserID
		var lockedID string
		err := h.DB.GetContext(ctx, &lockedID, `
WITH u AS (UPDATE users SET status='deleted', deleted_at=clock_timestamp(), updated_at=clock_timestamp() WHERE id=$1 AND status='active' RETURNING id),
s AS (UPDATE login_sessions SET active=false, invalidated_at=clock_timestamp(), reason='account_deletion' WHERE user_id=$1 AND active=true AND EXISTS(SELECT 1 FROM u))
SELECT id FROM u`, userID)
		if errors.Is(err, sql.ErrNoRows) {
			return NewAPIError(http.StatusNotFound, "account_not_found")
		}
		if err != nil {
			return err
		}
		sessionCache.removeUser(userID)
		return c.NoContent(http.StatusNoContent)
	}
}
