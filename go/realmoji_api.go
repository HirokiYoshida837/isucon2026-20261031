package isureal

import (
	"bytes"
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
		img, err := jpeg.Decode(bytes.NewReader(data))
		if err != nil {
			return NewAPIError(http.StatusBadRequest, "invalid_realmoji")
		}
		if img.Bounds().Dx() > 512 || img.Bounds().Dy() > 512 {
			return NewAPIError(http.StatusBadRequest, "invalid_realmoji")
		}

		ctx := c.Request().Context()
		actor := GetSession(c).UserID
		// ペアロック・存在と状態の判定・閲覧可否・置き換えと INSERT を 1 往復で行う（sql/90-interaction-func.sql、#167）
		var res struct {
			Code int `db:"code"`
			RealMojiWithReactor
		}
		imageID := uuid.NewString()
		if err := h.DB.GetContext(ctx, &res, `
   SELECT code, r_id AS id, r_post_id AS post_id, r_reactor_id AS reactor_id, r_image_id AS image_id, r_reaction_type AS reaction_type,
          r_created_at AS created_at, r_replaced_at AS replaced_at, r_deleted_at AS deleted_at, `+funcUserColumnsAs("u_", "reactor")+`
   FROM create_realmoji($1, $2, $3, $4, $5, $6)
  `, actor, input.PostID, uuid.NewString(), imageID, input.ReactionType, data); err != nil {
			return err
		}
		switch res.Code {
		case 0:
		case 404:
			return NewAPIError(http.StatusNotFound, "relationship_not_found")
		default:
			return NewAPIError(http.StatusForbidden, "interaction_forbidden")
		}
		reaction := res.RealMojiWithReactor
		if err := reaction.Reactor.setImageKey(); err != nil {
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
