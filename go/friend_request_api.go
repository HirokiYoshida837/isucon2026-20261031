package isureal

import (
	"database/sql"
	"errors"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/labstack/echo/v5"
)

type createFriendRequestRequest struct {
	RecipientID string `json:"recipientId"`
}

type createFriendRequestResponse struct {
	RelationID string    `json:"relationId"`
	Status     string    `json:"status"`
	UpdatedAt  time.Time `json:"updatedAt"`
}

func createFriendRequestHandler(h *HandlerContext) echo.HandlerFunc {
	return func(c *echo.Context) error {
		var input createFriendRequestRequest
		if err := c.Bind(&input); err != nil {
			return err
		}
		ctx := c.Request().Context()
		actor := GetSession(c).UserID
		if actor == input.RecipientID || input.RecipientID == "" {
			return NewAPIError(http.StatusBadRequest, "invalid_relationship")
		}
		var res struct {
			Code    int        `db:"code"`
			ID      *string    `db:"rid"`
			Status  *string    `db:"rstatus"`
			Updated *time.Time `db:"rupdated"`
		}
		if err := h.DB.GetContext(ctx, &res, `SELECT * FROM create_friend_request($1, $2, $3, $4)`, actor, input.RecipientID, uuid.NewString(), uuid.NewString()); err != nil {
			return err
		}
		switch res.Code {
		case 404:
			return NewAPIError(http.StatusNotFound, "relationship_not_found")
		case 409:
			return NewAPIError(http.StatusConflict, "relationship_conflict")
		}
		request := FriendRequest{ID: *res.ID, Status: *res.Status, UpdatedAt: *res.Updated}
		return c.JSON(http.StatusCreated, createFriendRequestResponse{RelationID: request.ID, Status: request.Status, UpdatedAt: request.UpdatedAt})
	}
}

type transitionFriendRequestRequest struct {
	RequestID string `param:"requestId" json:"-"`
	Action    string `param:"action" json:"-"`
}

type transitionFriendRequestResponse createFriendRequestResponse

func transitionFriendRequestHandler(h *HandlerContext) echo.HandlerFunc {
	return func(c *echo.Context) error {
		var input transitionFriendRequestRequest
		if err := c.Bind(&input); err != nil {
			return err
		}
		var status string
		switch input.Action {
		case "accept":
			status = "accepted"
		case "reject":
			status = "rejected"
		case "cancel":
			status = "cancelled"
		default:
			return NewAPIError(http.StatusForbidden, "relationship_action_forbidden")
		}
		ctx := c.Request().Context()
		actor := GetSession(c).UserID
		request, err := getFriendRequest(ctx, h.DB, input.RequestID)
		if errors.Is(err, sql.ErrNoRows) {
			return NewAPIError(http.StatusNotFound, "relationship_not_found")
		}
		if err != nil {
			return err
		}
		if (status == "cancelled" && actor != request.RequesterID) || (status != "cancelled" && actor != request.RecipientID) {
			return NewAPIError(http.StatusForbidden, "relationship_action_forbidden")
		}
		tx, err := beginRelationshipTx(ctx, h.DB, request.RequesterID, request.RecipientID)
		if err != nil {
			return err
		}
		defer tx.Rollback()
		var locked struct {
			FriendRequest
			Blocked bool `db:"blocked"`
		}
		if err := tx.GetContext(ctx, &locked, `
   SELECT `+friendRequestColumns+`,
    EXISTS(SELECT 1 FROM block_relations WHERE (blocker_id=r.requester_id AND blocked_id=r.recipient_id) OR (blocker_id=r.recipient_id AND blocked_id=r.requester_id)) AS blocked
   FROM friend_requests r WHERE id=$1
  `, input.RequestID); err != nil {
			return err
		}
		if locked.Status != "pending" {
			return NewAPIError(http.StatusForbidden, "relationship_action_forbidden")
		}
		if status == "accepted" {
			if locked.Blocked {
				return NewAPIError(http.StatusConflict, "relationship_conflict")
			}
			request, err = acceptFriendRequest(ctx, tx, locked.FriendRequest)
		} else {
			err = tx.GetContext(ctx, &request, `UPDATE friend_requests SET status=$2, updated_at=clock_timestamp() WHERE id=$1 RETURNING `+friendRequestColumns, locked.ID, status)
		}
		if err != nil {
			return err
		}
		if err := tx.Commit(); err != nil {
			return err
		}
		return c.JSON(http.StatusOK, transitionFriendRequestResponse{RelationID: request.ID, Status: request.Status, UpdatedAt: request.UpdatedAt})
	}
}
