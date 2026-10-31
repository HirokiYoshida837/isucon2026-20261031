package isureal

import (
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
		var res struct {
			Code    int        `db:"code"`
			ID      *string    `db:"rid"`
			Status  *string    `db:"rstatus"`
			Updated *time.Time `db:"rupdated"`
		}
		if err := h.DB.GetContext(ctx, &res, `SELECT * FROM transition_friend_request($1, $2, $3, $4)`, actor, input.RequestID, status, uuid.NewString()); err != nil {
			return err
		}
		switch res.Code {
		case 403:
			return NewAPIError(http.StatusForbidden, "relationship_action_forbidden")
		case 404:
			return NewAPIError(http.StatusNotFound, "relationship_not_found")
		case 409:
			return NewAPIError(http.StatusConflict, "relationship_conflict")
		}
		request := FriendRequest{ID: *res.ID, Status: *res.Status, UpdatedAt: *res.Updated}
		return c.JSON(http.StatusOK, transitionFriendRequestResponse{RelationID: request.ID, Status: request.Status, UpdatedAt: request.UpdatedAt})
	}
}
