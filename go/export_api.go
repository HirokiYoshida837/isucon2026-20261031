package isureal

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/labstack/echo/v5"
)

type exportPostResponse struct {
	createPostResponse
	ImagePath string `json:"imagePath"`
}

func exportPostsHandler(h *HandlerContext) echo.HandlerFunc {
	return func(c *echo.Context) error {
		ctx := c.Request().Context()
		userID := GetSession(c).UserID
		var posts []struct {
			Post
			Author    User   `db:"author"`
			ImageData []byte `db:"image_data"`
		}
		if err := h.DB.SelectContext(ctx, &posts, `
   SELECT `+postColumns+`, `+userColumnsAs("u", "author")+`, i.data AS image_data
   FROM posts p
   JOIN posting_windows w ON w.id = p.posting_window_id
   JOIN users u ON u.id = p.author_id AND u.status = 'active'
   JOIN image_objects i ON i.id = p.image_id
   WHERE p.author_id = $1 AND p.deleted_at IS NULL
   ORDER BY p.posted_at, p.id
  `, userID); err != nil {
			return err
		}

		var body bytes.Buffer
		writer := zip.NewWriter(&body)
		defer writer.Close()
		items := make([]exportPostResponse, 0, len(posts))
		for _, post := range posts {
			if err := post.Author.setImageKey(); err != nil {
				return err
			}
			imagePath := fmt.Sprintf("images/%06d.jpg", len(items)+1)
			image, err := writer.CreateHeader(&zip.FileHeader{Name: imagePath, Method: zip.Store})
			if err != nil {
				return err
			}
			if _, err := image.Write(post.ImageData); err != nil {
				return err
			}
			items = append(items, exportPostResponse{createPostResponse: newPostResponse(post.Post, post.Author), ImagePath: imagePath})
		}
		manifest, err := writer.Create("posts.json")
		if err != nil {
			return err
		}
		if err := json.NewEncoder(manifest).Encode(items); err != nil {
			return err
		}
		if err := writer.Close(); err != nil {
			return err
		}
		c.Response().Header().Set(echo.HeaderContentDisposition, `attachment; filename="isureal-posts.zip"`)
		c.Response().Header().Set(echo.HeaderCacheControl, "no-store")
		return c.Blob(http.StatusOK, "application/zip", body.Bytes())
	}
}
