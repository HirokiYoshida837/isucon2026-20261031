package isureal

import (
	"bytes"
	"image/jpeg"
	"io"
	"net/http"

	"github.com/google/uuid"
	"github.com/labstack/echo/v5"
)

func putProfileImageHandler(h *HandlerContext) echo.HandlerFunc {
	return func(c *echo.Context) error {
		if c.Request().Header.Get("Content-Type") != "image/jpeg" {
			// body を読み捨ててから 415 を返す（未読のまま返すと Go が接続を閉じ、nginx 側で 502 になる）
			io.Copy(io.Discard, c.Request().Body)
			return NewAPIError(http.StatusUnsupportedMediaType, "unsupported_media_type")
		}
		data, err := io.ReadAll(c.Request().Body)
		if err != nil {
			return err
		}
		img, err := jpeg.Decode(bytes.NewReader(data))
		if err != nil {
			c.Set("abuse", "invalid_image")
			return NewAPIError(http.StatusBadRequest, "invalid_profile_image")
		}
		if img.Bounds().Dx() > 512 || img.Bounds().Dy() > 512 {
			return NewAPIError(http.StatusBadRequest, "invalid_profile_image")
		}

		ctx := c.Request().Context()
		userID := GetSession(c).UserID
		var n int
		if err := h.DB.GetContext(ctx, &n, `
WITH old AS (SELECT profile_image_id FROM users WHERE id=$1 FOR NO KEY UPDATE),
ins AS (INSERT INTO image_objects(id,owner_user_id,data,media_type) SELECT $2,$1,$3,'image/jpeg' FROM old),
upd AS (UPDATE users SET profile_image_id=$2,updated_at=clock_timestamp() WHERE id=$1),
del AS (DELETE FROM image_objects WHERE id=(SELECT profile_image_id FROM old))
SELECT count(*) FROM old`, userID, uuid.NewString(), data); err != nil {
			return err
		}
		if n == 0 {
			return NewAPIError(http.StatusNotFound, "account_not_found")
		}
		return c.NoContent(http.StatusNoContent)
	}
}

func deleteProfileImageHandler(h *HandlerContext) echo.HandlerFunc {
	return func(c *echo.Context) error {
		ctx := c.Request().Context()
		userID := GetSession(c).UserID
		var n int
		if err := h.DB.GetContext(ctx, &n, `
WITH old AS (SELECT profile_image_id FROM users WHERE id=$1 AND profile_image_id IS NOT NULL FOR NO KEY UPDATE),
upd AS (UPDATE users SET profile_image_id=NULL,updated_at=clock_timestamp() WHERE id=$1 AND EXISTS(SELECT 1 FROM old)),
del AS (DELETE FROM image_objects WHERE id=(SELECT profile_image_id FROM old))
SELECT count(*) FROM old`, userID); err != nil {
			return err
		}
		if n == 0 {
			return NewAPIError(http.StatusNotFound, "account_not_found")
		}
		return c.NoContent(http.StatusNoContent)
	}
}
