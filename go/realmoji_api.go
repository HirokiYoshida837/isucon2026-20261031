package isureal

import (
	"bytes"
	"database/sql"
	"errors"
	"image/jpeg"
	"mime/multipart"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/labstack/echo/v5"
)

type replaceRealMojiRequest struct {
	PostID       string                `param:"postId" json:"-"`
	Image        *multipart.FileHeader `form:"image"`
	ReactionType string                `form:"reactionType"`
}

type replaceRealMojiResponse struct {
	ID           string                   `json:"id"`
	Reactor      relationshipUserResponse `json:"reactor"`
	ReactionType string                   `json:"reactionType"`
	ImageURL     string                   `json:"imageUrl"`
	CreatedAt    time.Time                `json:"createdAt"`
}

func newRealMojiResponse(reaction RealMoji, user User) replaceRealMojiResponse {
	return replaceRealMojiResponse{ID: reaction.ID, Reactor: newRelationshipUserResponse(user), ReactionType: reaction.ReactionType, ImageURL: signedImageURL(user, reaction.ImageID), CreatedAt: reaction.CreatedAt}
}

func replaceRealMojiHandler(h *HandlerContext) echo.HandlerFunc {
	return func(c *echo.Context) error {
		var input replaceRealMojiRequest
		if err := c.Bind(&input); err != nil {
			return err
		}
		if input.Image == nil {
			return NewAPIError(http.StatusBadRequest, "invalid_realmoji")
		}
		switch input.ReactionType {
		case "like", "smile", "surprise", "heart", "laugh", "instant":
		default:
			return NewAPIError(http.StatusBadRequest, "invalid_realmoji")
		}
		file, err := input.Image.Open()
		if err != nil {
			return err
		}
		defer file.Close()
		data, err := readBodySized(file, input.Image.Size)
		if err != nil {
			return err
		}
		// サイズ検証だけなのでヘッダだけ読む（投稿画像 #60 と同じ）
		img, err := jpeg.DecodeConfig(bytes.NewReader(data))
		if err != nil {
			return NewAPIError(http.StatusBadRequest, "invalid_realmoji")
		}
		if img.Width > 512 || img.Height > 512 {
			return NewAPIError(http.StatusBadRequest, "invalid_realmoji")
		}

		ctx := c.Request().Context()
		actor := GetSession(c).UserID
		tx, err := h.DB.BeginTxx(ctx, nil)
		if err != nil {
			return err
		}
		defer tx.Rollback()
		var lock postPairLock
		err = tx.GetContext(ctx, &lock, postPairLockSQL, actor, input.PostID)
		if errors.Is(err, sql.ErrNoRows) {
			return NewAPIError(http.StatusForbidden, "interaction_forbidden")
		}
		if err != nil {
			return err
		}
		if actor == lock.AuthorID {
			return NewAPIError(http.StatusForbidden, "interaction_forbidden")
		}
		if lock.ActiveCount != 2 {
			return NewAPIError(http.StatusNotFound, "relationship_not_found")
		}
		allowed, err := canViewPost(ctx, tx, actor, input.PostID)
		if err != nil {
			return err
		}
		if !allowed {
			return NewAPIError(http.StatusForbidden, "interaction_forbidden")
		}
		if _, err := tx.ExecContext(ctx, `UPDATE realmojis SET replaced_at=clock_timestamp() WHERE post_id=$1 AND reactor_id=$2 AND replaced_at IS NULL AND deleted_at IS NULL`, input.PostID, actor); err != nil {
			return err
		}
		var reaction RealMojiWithReactor
		if err := tx.GetContext(ctx, &reaction, `
   WITH image AS (
     INSERT INTO image_objects(id,owner_user_id,data,media_type) VALUES($4,$3,$6,'image/jpeg')
     RETURNING id,created_at,owner_user_id
   ), image_key AS (
     INSERT INTO image_keys(id,created_at,owner_user_id) SELECT id,created_at,owner_user_id FROM image
   ), r AS (
     INSERT INTO realmojis(id,post_id,reactor_id,image_id,reaction_type,created_at) VALUES($1,$2,$3,$4,$5,clock_timestamp())
     RETURNING id,post_id,reactor_id,image_id,reaction_type,created_at,replaced_at,deleted_at
   )
   SELECT r.id,r.post_id,r.reactor_id,r.image_id,r.reaction_type,r.created_at,r.replaced_at,r.deleted_at, `+userColumnsAs("u", "reactor")+`
   FROM r JOIN users u ON u.id = r.reactor_id AND u.status = 'active'
  `, uuid.NewString(), input.PostID, actor, uuid.NewString(), input.ReactionType, data); err != nil {
			return err
		}
		if err := reaction.Reactor.setImageKey(); err != nil {
			return err
		}
		if err := tx.Commit(); err != nil {
			return err
		}
		imageObjectCache.put(reaction.ImageID, "image/jpeg", data)
		return c.JSON(http.StatusCreated, newRealMojiResponse(reaction.RealMoji, reaction.Reactor))
	}
}

type getRealMojisRequest struct {
	PostID          string     `param:"postId"`
	BeforeCreatedAt *time.Time `query:"beforeCreatedAt"`
	BeforeID        string     `query:"beforeId"`
}

type getRealMojisResponse struct {
	Items   []replaceRealMojiResponse `json:"items"`
	HasMore bool                      `json:"hasMore"`
}

func getRealMojisHandler(h *HandlerContext) echo.HandlerFunc {
	return func(c *echo.Context) error {
		var input getRealMojisRequest
		if err := c.Bind(&input); err != nil {
			return err
		}
		if (input.BeforeCreatedAt == nil) != (input.BeforeID == "") {
			return NewAPIError(http.StatusBadRequest, "invalid_pagination")
		}
		ctx := c.Request().Context()
		allowed, err := canViewPost(ctx, h.DB, GetSession(c).UserID, input.PostID)
		if err != nil {
			return err
		}
		if !allowed {
			return NewAPIError(http.StatusForbidden, "interaction_forbidden")
		}
		reactions, err := selectCurrentRealMojis(ctx, h.DB, `AND r.post_id=$1 AND ($2::timestamptz IS NULL OR (r.created_at,r.id)<($2,$3)) ORDER BY r.created_at DESC,r.id DESC LIMIT 51`, input.PostID, input.BeforeCreatedAt, input.BeforeID)
		if err != nil {
			return err
		}
		items := make([]replaceRealMojiResponse, 0, len(reactions))
		for _, reaction := range reactions {
			items = append(items, newRealMojiResponse(reaction.RealMoji, reaction.Reactor))
		}
		hasMore := len(items) > 50
		if hasMore {
			items = items[:50]
		}
		return c.JSON(http.StatusOK, getRealMojisResponse{Items: items, HasMore: hasMore})
	}
}

type deleteRealMojiRequest struct {
	RealMojiID string `param:"realMojiId" json:"-"`
}

func deleteRealMojiHandler(h *HandlerContext) echo.HandlerFunc {
	return func(c *echo.Context) error {
		var input deleteRealMojiRequest
		if err := c.Bind(&input); err != nil {
			return err
		}
		ctx := c.Request().Context()
		actor := GetSession(c).UserID
		// 単一文の原子的 UPDATE。0 行のときだけ 404 / 403 を判定する。
		result, err := h.DB.ExecContext(ctx, `UPDATE realmojis r SET deleted_at=clock_timestamp() FROM posts p WHERE r.id=$1 AND r.deleted_at IS NULL AND p.id=r.post_id AND ($2=r.reactor_id OR $2=p.author_id)`, input.RealMojiID, actor)
		if err != nil {
			return err
		}
		if n, err := result.RowsAffected(); err != nil {
			return err
		} else if n == 0 {
			var exists bool
			if err := h.DB.GetContext(ctx, &exists, `SELECT EXISTS(SELECT 1 FROM realmojis r JOIN posts p ON p.id=r.post_id WHERE r.id=$1 AND r.deleted_at IS NULL)`, input.RealMojiID); err != nil {
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
