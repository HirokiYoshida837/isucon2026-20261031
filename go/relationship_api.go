package isureal

import (
	"net/http"

	"github.com/google/uuid"
	"github.com/labstack/echo/v5"
)

type deleteFriendshipRequest struct {
	UserID string `param:"userId" json:"-"`
}

func deleteFriendshipHandler(h *HandlerContext) echo.HandlerFunc {
	return func(c *echo.Context) error {
		var input deleteFriendshipRequest
		if err := c.Bind(&input); err != nil {
			return err
		}
		ctx := c.Request().Context()
		actor := GetSession(c).UserID
		if actor == input.UserID || input.UserID == "" {
			return NewAPIError(http.StatusBadRequest, "invalid_relationship")
		}
		// 単一行の DELETE は原子的なのでペアロック不要。0 行のときだけ 404 / http.StatusConflict を判定する。
		result, err := h.DB.ExecContext(ctx, `DELETE FROM friendships WHERE user_low_id=$1 AND user_high_id=$2 AND (SELECT count(*) FROM users WHERE id IN ($1,$2) AND status='active') = 2`, min(actor, input.UserID), max(actor, input.UserID))
		if err != nil {
			return err
		}
		if n, err := result.RowsAffected(); err != nil {
			return err
		} else if n == 0 {
			var active int
			if err := h.DB.GetContext(ctx, &active, `SELECT count(*) FROM users WHERE id IN ($1,$2) AND status='active'`, actor, input.UserID); err != nil {
				return err
			}
			if active != 2 {
				return NewAPIError(http.StatusNotFound, "relationship_not_found")
			}
			return NewAPIError(http.StatusConflict, "relationship_conflict")
		}
		return c.NoContent(http.StatusNoContent)
	}
}

type followRequest struct {
	UserID string `param:"userId" json:"-"`
}

func followHandler(h *HandlerContext) echo.HandlerFunc {
	return func(c *echo.Context) error {
		var input followRequest
		if err := c.Bind(&input); err != nil {
			return err
		}
		ctx := c.Request().Context()
		actor := GetSession(c).UserID
		if actor == input.UserID || input.UserID == "" {
			return NewAPIError(http.StatusBadRequest, "invalid_relationship")
		}
		var code int
		if err := h.DB.GetContext(ctx, &code, `SELECT follow_user($1, $2, $3)`, actor, input.UserID, uuid.NewString()); err != nil {
			return err
		}
		switch code {
		case 404:
			return NewAPIError(http.StatusNotFound, "relationship_not_found")
		case 409:
			return NewAPIError(http.StatusConflict, "relationship_conflict")
		}
		return c.NoContent(http.StatusNoContent)
	}
}

type unfollowRequest struct {
	UserID string `param:"userId" json:"-"`
}

func unfollowHandler(h *HandlerContext) echo.HandlerFunc {
	return func(c *echo.Context) error {
		var input unfollowRequest
		if err := c.Bind(&input); err != nil {
			return err
		}
		ctx := c.Request().Context()
		actor := GetSession(c).UserID
		if actor == input.UserID || input.UserID == "" {
			return NewAPIError(http.StatusBadRequest, "invalid_relationship")
		}
		// 単一行の DELETE は原子的なのでペアロック不要。0 行のときだけ 404 / 409 を判定する。
		result, err := h.DB.ExecContext(ctx, `DELETE FROM follows WHERE follower_id=$1 AND followee_id=$2 AND (SELECT count(*) FROM users WHERE id IN ($1,$2) AND status='active') = 2`, actor, input.UserID)
		if err != nil {
			return err
		}
		if n, err := result.RowsAffected(); err != nil {
			return err
		} else if n == 0 {
			var active int
			if err := h.DB.GetContext(ctx, &active, `SELECT count(*) FROM users WHERE id IN ($1,$2) AND status='active'`, actor, input.UserID); err != nil {
				return err
			}
			if active != 2 {
				return NewAPIError(http.StatusNotFound, "relationship_not_found")
			}
			return NewAPIError(http.StatusConflict, "relationship_conflict")
		}
		return c.NoContent(http.StatusNoContent)
	}
}

type blockRequest struct {
	UserID string `param:"userId" json:"-"`
}

func blockHandler(h *HandlerContext) echo.HandlerFunc {
	return func(c *echo.Context) error {
		var input blockRequest
		if err := c.Bind(&input); err != nil {
			return err
		}
		ctx := c.Request().Context()
		actor := GetSession(c).UserID
		if actor == input.UserID || input.UserID == "" {
			return NewAPIError(http.StatusBadRequest, "invalid_relationship")
		}
		// ペアロック・active 確認・INSERT・friendships / follows / friend_requests の後始末を sql/94-block-func.sql の関数で 1 往復にする
		var code int
		if err := h.DB.GetContext(ctx, &code, `SELECT block_user($1, $2, $3)`, actor, input.UserID, uuid.NewString()); err != nil {
			return err
		}
		switch code {
		case 404:
			return NewAPIError(http.StatusNotFound, "relationship_not_found")
		case 409:
			return NewAPIError(http.StatusConflict, "relationship_conflict")
		}
		return c.NoContent(http.StatusNoContent)
	}
}

type unblockRequest struct {
	UserID string `param:"userId" json:"-"`
}

func unblockHandler(h *HandlerContext) echo.HandlerFunc {
	return func(c *echo.Context) error {
		var input unblockRequest
		if err := c.Bind(&input); err != nil {
			return err
		}
		ctx := c.Request().Context()
		actor := GetSession(c).UserID
		if actor == input.UserID || input.UserID == "" {
			return NewAPIError(http.StatusBadRequest, "invalid_relationship")
		}
		// 単一行の DELETE は原子的なのでペアロック不要。0 行のときだけ 404 / http.StatusForbidden を判定する。
		result, err := h.DB.ExecContext(ctx, `DELETE FROM block_relations WHERE blocker_id=$1 AND blocked_id=$2 AND (SELECT count(*) FROM users WHERE id IN ($1,$2) AND status='active') = 2`, actor, input.UserID)
		if err != nil {
			return err
		}
		if n, err := result.RowsAffected(); err != nil {
			return err
		} else if n == 0 {
			var active int
			if err := h.DB.GetContext(ctx, &active, `SELECT count(*) FROM users WHERE id IN ($1,$2) AND status='active'`, actor, input.UserID); err != nil {
				return err
			}
			if active != 2 {
				return NewAPIError(http.StatusNotFound, "relationship_not_found")
			}
			return NewAPIError(http.StatusForbidden, "relationship_action_forbidden")
		}
		return c.NoContent(http.StatusNoContent)
	}
}
