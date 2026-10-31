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
			Author User `db:"author"`
		}
		if err := h.DB.SelectContext(ctx, &posts, `
   SELECT `+postColumns+`, `+userColumnsAs("u", "author")+`
   FROM posts p
   JOIN posting_windows w ON w.id = p.posting_window_id
   JOIN users u ON u.id = p.author_id AND u.status = 'active'
   WHERE p.author_id = $1 AND p.deleted_at IS NULL
   ORDER BY p.posted_at, p.id
  `, userID); err != nil {
			return err
		}

		// 画像本体は imageObjectCache から取り、無い分だけまとめて 1 クエリで取る
		images := make(map[string][]byte, len(posts))
		var missing []string
		for _, post := range posts {
			if _, ok := images[post.ImageID]; ok {
				continue
			}
			if entry, ok := imageObjectCache.get(post.ImageID); ok {
				images[post.ImageID] = entry.data
			} else {
				images[post.ImageID] = nil
				missing = append(missing, post.ImageID)
			}
		}
		if len(missing) > 0 {
			rows, err := h.DB.QueryxContext(ctx, `SELECT id, media_type, data FROM image_objects WHERE id = ANY($1)`, missing)
			if err != nil {
				return err
			}
			for rows.Next() {
				var id, mediaType string
				var data []byte
				if err := rows.Scan(&id, &mediaType, &data); err != nil {
					rows.Close()
					return err
				}
				images[id] = data
				imageObjectCache.put(id, mediaType, data)
			}
			err = rows.Err()
			rows.Close()
			if err != nil {
				return err
			}
		}

		var body bytes.Buffer
		writer := zip.NewWriter(&body)
		defer writer.Close()
		items := make([]exportPostResponse, 0, len(posts))
		for _, post := range posts {
			imageData := images[post.ImageID]
			if imageData == nil {
				continue // 画像が無い投稿は出さない（以前の JOIN image_objects と同じ挙動）
			}
			if err := post.Author.setImageKey(); err != nil {
				return err
			}
			imagePath := fmt.Sprintf("images/%06d.jpg", len(items)+1)
			image, err := writer.CreateHeader(&zip.FileHeader{Name: imagePath, Method: zip.Store})
			if err != nil {
				return err
			}
			if _, err := image.Write(imageData); err != nil {
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
