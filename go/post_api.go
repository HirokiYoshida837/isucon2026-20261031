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

type createPostRequest struct {
	Image        *multipart.FileHeader `form:"image"`
	Caption      string                `form:"caption"`
	Visibility   string                `form:"visibility"`
	LocationKind string                `form:"location_kind"`
	Latitude     *float64              `form:"latitude"`
	Longitude    *float64              `form:"longitude"`
	Locality     *string               `form:"locality"`
}

type postAuthorResponse struct {
	ID              string  `json:"id"`
	Username        string  `json:"username"`
	DisplayName     string  `json:"displayName"`
	ProfileImageURL *string `json:"profileImageUrl"`
	Bio             string  `json:"bio"`
	AccountType     string  `json:"accountType"`
}

type createPostResponse struct {
	ID              string             `json:"id"`
	Author          postAuthorResponse `json:"author"`
	PostingWindowID string             `json:"postingWindowId"`
	ImageURL        string             `json:"imageUrl"`
	Caption         string             `json:"caption"`
	Visibility      string             `json:"visibility"`
	Location        PostLocation       `json:"location"`
	PostedAt        time.Time          `json:"postedAt"`
	OnTime          bool               `json:"onTime"`
	LateSeconds     int64              `json:"lateSeconds"`
}

func createPostHandler(h *HandlerContext) echo.HandlerFunc {
	return func(c *echo.Context) error {
		var input createPostRequest
		if err := c.Bind(&input); err != nil {
			return err
		}
		if input.Image == nil || !validUserText(input.Caption, 0, 1000) {
			return NewAPIError(http.StatusBadRequest, "invalid_post")
		}
		switch input.Visibility {
		case "friends", "friends_of_friends", "public":
		default:
			return NewAPIError(http.StatusBadRequest, "invalid_post")
		}
		location := PostLocation{
			Kind: input.LocationKind, Latitude: input.Latitude,
			Longitude: input.Longitude, Locality: input.Locality,
		}
		if !location.valid() {
			return NewAPIError(http.StatusBadRequest, "invalid_post")
		}
		imageFile, err := input.Image.Open()
		if err != nil {
			return err
		}
		defer imageFile.Close()
		imageData, err := readBodySized(imageFile, input.Image.Size)
		if err != nil {
			return err
		}
		// サイズ検証だけなのでヘッダだけ読む（投稿画像は avg 400KB。全画素のデコードは CPU を食う）
		img, err := jpeg.DecodeConfig(bytes.NewReader(imageData))
		if err != nil {
			return NewAPIError(http.StatusBadRequest, "invalid_post")
		}
		if img.Width > 4096 || img.Height > 4096 {
			return NewAPIError(http.StatusBadRequest, "invalid_post")
		}

		ctx := c.Request().Context()
		userID := GetSession(c).UserID
		// ユーザーのロック・期間と枠の判定・3 テーブルへの INSERT を 1 往復で行う（sql/87-create-post-func.sql）
		var res struct {
			Code int `db:"code"`
			User
			WindowID       *string    `db:"r_window_id"`
			OnTimeDeadline *time.Time `db:"r_on_time_deadline"`
			PostedAt       *time.Time `db:"r_posted_at"`
		}
		imageID := uuid.NewString()
		postID := uuid.NewString()
		if err := h.DB.GetContext(ctx, &res, `
   SELECT code, r_id AS id, r_username AS username, r_secret_key_hash AS secret_key_hash, r_display_name AS display_name,
          r_bio AS bio, r_profile_image_id AS profile_image_id, r_region AS region, r_account_type AS account_type,
          r_status AS status, r_window_id, r_on_time_deadline, r_posted_at
   FROM create_post($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
  `, userID, postID, imageID, input.Caption, input.Visibility,
			location.Kind, location.Latitude, location.Longitude, location.Locality, imageData); err != nil {
			return err
		}
		switch res.Code {
		case 0:
		case 409:
			return NewAPIError(http.StatusConflict, "post_slot_unavailable")
		default:
			return sql.ErrNoRows
		}
		// 投稿直後の画像の確認で本体を DB から引き直さないよう、書いた本体をキャッシュに入れる（中身は変わらない）
		imageObjectCache.put(imageID, "image/jpeg", imageData)
		user := res.User
		if err := user.setImageKey(); err != nil {
			return err
		}
		post := Post{
			ID: postID, AuthorID: userID, PostingWindowID: *res.WindowID, ImageID: imageID,
			Caption: input.Caption, Visibility: input.Visibility, PostLocation: location,
			PostedAt: *res.PostedAt, OnTimeDeadline: *res.OnTimeDeadline,
		}
		return c.JSON(http.StatusCreated, newPostResponse(post, user))
	}
}

func newPostResponse(post Post, user User) createPostResponse {
	response := createPostResponse{
		ID: post.ID,
		Author: postAuthorResponse{
			ID: user.ID, Username: user.Username, DisplayName: user.DisplayName,
			ProfileImageURL: newGetMeResponse(user).ProfileImageURL,
			Bio:             user.Bio, AccountType: user.AccountType,
		},
		PostingWindowID: post.PostingWindowID, ImageURL: signedImageURL(user, post.ImageID),
		Caption: post.Caption, Visibility: post.Visibility, Location: post.PostLocation,
		PostedAt: post.PostedAt, OnTime: !post.PostedAt.After(post.OnTimeDeadline),
	}
	if !response.OnTime {
		response.LateSeconds = int64(post.PostedAt.Sub(post.OnTimeDeadline) / time.Second)
	}
	return response
}

type getMyCurrentPostResponse createPostResponse

func getMyCurrentPostHandler(h *HandlerContext) echo.HandlerFunc {
	return func(c *echo.Context) error {
		ctx := c.Request().Context()
		userID := GetSession(c).UserID
		posts, err := selectPostsWithAuthor(ctx, h.DB, `
   AND p.author_id = $1
   AND p.local_date = (SELECT current.local_date FROM posting_windows current WHERE current.region = u.region AND current.ended_at IS NULL)
  `, userID)
		if err != nil {
			return err
		}
		if len(posts) == 0 {
			return NewAPIError(http.StatusNotFound, "post_not_found")
		}
		return c.JSON(http.StatusOK, getMyCurrentPostResponse(newPostResponse(posts[0].Post, posts[0].Author)))
	}
}

type getPostRequest struct {
	PostID string `param:"postId"`
}

type getPostResponse createPostResponse

func getPostHandler(h *HandlerContext) echo.HandlerFunc {
	return func(c *echo.Context) error {
		var input getPostRequest
		if err := c.Bind(&input); err != nil {
			return err
		}
		ctx := c.Request().Context()
		row, err := getPostForViewer(ctx, h.DB, GetSession(c).UserID, input.PostID)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		if err != nil || !row.canView() {
			return NewAPIError(http.StatusNotFound, "post_not_found")
		}
		return c.JSON(http.StatusOK, getPostResponse(newPostResponse(row.Post, row.Author)))
	}
}

type updatePostRequest struct {
	PostID     string  `param:"postId" json:"-"`
	Caption    *string `json:"caption"`
	Visibility *string `json:"visibility"`
}

type updatePostResponse createPostResponse

func updatePostHandler(h *HandlerContext) echo.HandlerFunc {
	return func(c *echo.Context) error {
		var input updatePostRequest
		if err := c.Bind(&input); err != nil {
			return err
		}
		if input.Caption == nil && input.Visibility == nil {
			return NewAPIError(http.StatusBadRequest, "invalid_post")
		}
		if input.Caption != nil && !validUserText(*input.Caption, 0, 1000) {
			return NewAPIError(http.StatusBadRequest, "invalid_post")
		}
		if input.Visibility != nil {
			switch *input.Visibility {
			case "friends", "friends_of_friends", "public":
			default:
				return NewAPIError(http.StatusBadRequest, "invalid_post")
			}
		}
		ctx := c.Request().Context()
		userID := GetSession(c).UserID
		tx, err := h.DB.BeginTxx(ctx, nil)
		if err != nil {
			return err
		}
		defer tx.Rollback()
		var lockedUserID string
		if err := tx.GetContext(ctx, &lockedUserID, `SELECT id FROM users WHERE id = $1 FOR NO KEY UPDATE`, userID); err != nil {
			return err
		}
		var updated PostWithAuthor
		err = tx.GetContext(ctx, &updated, `
   UPDATE posts p SET caption = COALESCE($3, p.caption), visibility = COALESCE($4, p.visibility)
   FROM posting_windows w, users u
   WHERE p.id = $1 AND p.author_id = $2 AND p.deleted_at IS NULL
     AND w.id = p.posting_window_id AND w.ended_at IS NULL
     AND u.id = p.author_id AND u.status = 'active'
   RETURNING p.id, p.author_id, p.posting_window_id, p.image_id, p.caption, p.visibility,
     p.location_kind, p.latitude, p.longitude, p.locality, p.posted_at,
     w.on_time_deadline, w.ended_at AS window_ended_at, `+userColumnsAs("u", "author"),
			input.PostID, userID, input.Caption, input.Visibility)
		if errors.Is(err, sql.ErrNoRows) {
			// 0 行のときだけ従来の判定
			post, err := getPost(ctx, tx, input.PostID)
			if errors.Is(err, sql.ErrNoRows) {
				return NewAPIError(http.StatusNotFound, "post_not_found")
			}
			if err != nil {
				return err
			}
			if post.AuthorID != userID || post.WindowEndedAt != nil {
				return NewAPIError(http.StatusForbidden, "post_not_editable")
			}
			if _, err := tx.ExecContext(ctx, `UPDATE posts SET caption = COALESCE($2, caption), visibility = COALESCE($3, visibility) WHERE id = $1`, post.ID, input.Caption, input.Visibility); err != nil {
				return err
			}
			updated, err = getPostWithAuthor(ctx, tx, post.ID)
		}
		if err != nil {
			return err
		}
		if err := updated.Author.setImageKey(); err != nil {
			return err
		}
		if err := tx.Commit(); err != nil {
			return err
		}
		return c.JSON(http.StatusOK, updatePostResponse(newPostResponse(updated.Post, updated.Author)))
	}
}

type deletePostRequest struct {
	PostID string `param:"postId" json:"-"`
}

func deletePostHandler(h *HandlerContext) echo.HandlerFunc {
	return func(c *echo.Context) error {
		var input deletePostRequest
		if err := c.Bind(&input); err != nil {
			return err
		}
		ctx := c.Request().Context()
		userID := GetSession(c).UserID
		tx, err := h.DB.BeginTxx(ctx, nil)
		if err != nil {
			return err
		}
		defer tx.Rollback()
		var lockedUserID string
		if err := tx.GetContext(ctx, &lockedUserID, `SELECT id FROM users WHERE id = $1 FOR NO KEY UPDATE`, userID); err != nil {
			return err
		}
		var post struct {
			Post
			Deletions int `db:"deletions"`
		}
		err = tx.GetContext(ctx, &post, `
   SELECT `+postColumns+`, (
     SELECT count(*) FROM posts dp
     WHERE dp.author_id = $2 AND dp.deleted_at IS NOT NULL AND dp.local_date = p.local_date
   ) AS deletions
   FROM posts p JOIN posting_windows w ON w.id = p.posting_window_id
   WHERE p.id = $1 AND p.deleted_at IS NULL
  `, input.PostID, userID)
		if errors.Is(err, sql.ErrNoRows) {
			return NewAPIError(http.StatusNotFound, "post_not_found")
		}
		if err != nil {
			return err
		}
		if post.AuthorID != userID {
			return NewAPIError(http.StatusForbidden, "post_not_editable")
		}
		deletions := post.Deletions
		if deletions >= 2 {
			return NewAPIError(http.StatusConflict, "post_slot_unavailable")
		}
		if _, err := tx.ExecContext(ctx, `UPDATE posts SET deleted_at = clock_timestamp() WHERE id = $1`, post.ID); err != nil {
			return err
		}
		if err := tx.Commit(); err != nil {
			return err
		}
		return c.NoContent(http.StatusNoContent)
	}
}
