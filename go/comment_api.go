package isureal

import (
	"database/sql"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/labstack/echo/v5"
)

type createCommentRequest struct {
	PostID string `param:"postId" json:"-"`
	Body   string `json:"body"`
}

type createCommentResponse struct {
	ID        string                   `json:"id"`
	Author    relationshipUserResponse `json:"author"`
	Body      string                   `json:"body"`
	CreatedAt time.Time                `json:"createdAt"`
}

func newCommentResponse(comment Comment, user User) createCommentResponse {
	return createCommentResponse{ID: comment.ID, Author: newRelationshipUserResponse(user), Body: comment.Body, CreatedAt: comment.CreatedAt}
}

func createCommentHandler(h *HandlerContext) echo.HandlerFunc {
	return func(c *echo.Context) error {
		var input createCommentRequest
		if err := c.Bind(&input); err != nil {
			return err
		}
		if !validUserText(input.Body, 1, 1000) {
			return NewAPIError(http.StatusBadRequest, "invalid_interaction")
		}
		ctx := c.Request().Context()
		actor := GetSession(c).UserID
		// ペアロック・存在と状態の判定・コメント可否・INSERT を 1 往復で行う（sql/90-interaction-func.sql、#167）
		var res struct {
			Code int `db:"code"`
			CommentWithAuthor
		}
		if err := h.DB.GetContext(ctx, &res, `
   SELECT code, c_id AS id, c_post_id AS post_id, c_author_id AS author_id, c_body AS body, c_created_at AS created_at, c_deleted_at AS deleted_at,
          `+funcUserColumnsAs("u_", "author")+`
   FROM create_comment($1, $2, $3, $4)
  `, actor, input.PostID, uuid.NewString(), input.Body); err != nil {
			return err
		}
		switch res.Code {
		case 0:
		case 500:
			return sql.ErrNoRows
		default:
			return NewAPIError(http.StatusForbidden, "interaction_forbidden")
		}
		comment := res.CommentWithAuthor
		if err := comment.Author.setImageKey(); err != nil {
			return err
		}
		return c.JSON(http.StatusCreated, newCommentResponse(comment.Comment, comment.Author))
	}
}

type getCommentsRequest struct {
	PostID         string     `param:"postId"`
	AfterCreatedAt *time.Time `query:"afterCreatedAt"`
	AfterID        string     `query:"afterId"`
}

type getCommentsResponse struct {
	Items   []createCommentResponse `json:"items"`
	HasMore bool                    `json:"hasMore"`
}

func getCommentsHandler(h *HandlerContext) echo.HandlerFunc {
	return func(c *echo.Context) error {
		var input getCommentsRequest
		if err := c.Bind(&input); err != nil {
			return err
		}
		if (input.AfterCreatedAt == nil) != (input.AfterID == "") {
			return NewAPIError(http.StatusBadRequest, "invalid_pagination")
		}
		ctx := c.Request().Context()
		allowed, err := canComment(ctx, h.DB, GetSession(c).UserID, input.PostID)
		if err != nil {
			return err
		}
		if !allowed {
			return NewAPIError(http.StatusForbidden, "interaction_forbidden")
		}
		comments, err := selectCommentsWithAuthor(ctx, h.DB, `AND c.post_id=$1 AND ($2::timestamptz IS NULL OR (c.created_at,c.id)>($2,$3)) ORDER BY c.created_at,c.id LIMIT 51`, input.PostID, input.AfterCreatedAt, input.AfterID)
		if err != nil {
			return err
		}
		items := make([]createCommentResponse, 0, len(comments))
		for _, comment := range comments {
			items = append(items, newCommentResponse(comment.Comment, comment.Author))
		}
		hasMore := len(items) > 50
		if hasMore {
			items = items[:50]
		}
		return c.JSON(http.StatusOK, getCommentsResponse{Items: items, HasMore: hasMore})
	}
}

type deleteCommentRequest struct {
	CommentID string `param:"commentId" json:"-"`
}

func deleteCommentHandler(h *HandlerContext) echo.HandlerFunc {
	return func(c *echo.Context) error {
		var input deleteCommentRequest
		if err := c.Bind(&input); err != nil {
			return err
		}
		ctx := c.Request().Context()
		actor := GetSession(c).UserID
		// 単一文の原子的 UPDATE。0 行のときだけ 404 / 403 を判定する。
		result, err := h.DB.ExecContext(ctx, `UPDATE comments c SET deleted_at=clock_timestamp() FROM posts p WHERE c.id=$1 AND c.deleted_at IS NULL AND p.id=c.post_id AND ($2=c.author_id OR $2=p.author_id)`, input.CommentID, actor)
		if err != nil {
			return err
		}
		if n, err := result.RowsAffected(); err != nil {
			return err
		} else if n == 0 {
			var exists bool
			if err := h.DB.GetContext(ctx, &exists, `SELECT EXISTS(SELECT 1 FROM comments c JOIN posts p ON p.id=c.post_id WHERE c.id=$1 AND c.deleted_at IS NULL)`, input.CommentID); err != nil {
				return err
			}
			if !exists {
				return NewAPIError(http.StatusNotFound, "interaction_not_found")
			}
			return NewAPIError(http.StatusForbidden, "interaction_forbidden")
		}
		return c.NoContent(http.StatusNoContent)
	}
}
