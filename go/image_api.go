package isureal

import (
	"crypto/hmac"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/labstack/echo/v5"
)

type getImageRequest struct {
	ImageID string `param:"imageId" json:"-"`
	Exp     string `query:"exp" json:"-"`
	Sig     string `query:"sig" json:"-"`
}

func getImageHandler(h *HandlerContext) echo.HandlerFunc {
	return func(c *echo.Context) error {
		c.Response().Header().Set("Cache-Control", "no-store")
		var input getImageRequest
		if err := c.Bind(&input); err != nil {
			return NewAPIError(http.StatusForbidden, "image_forbidden")
		}
		query, err := url.ParseQuery(c.Request().URL.RawQuery)
		if err != nil || len(query["exp"]) != 1 || len(query["sig"]) != 1 {
			return NewAPIError(http.StatusForbidden, "image_forbidden")
		}
		exp, err := strconv.ParseInt(input.Exp, 10, 64)
		if err != nil || strconv.FormatInt(exp, 10) != input.Exp || exp <= time.Now().Unix() {
			return NewAPIError(http.StatusForbidden, "image_forbidden")
		}
		sig, err := base64.RawURLEncoding.Strict().DecodeString(input.Sig)
		if err != nil || len(sig) != sha256.Size || base64.RawURLEncoding.EncodeToString(sig) != input.Sig {
			return NewAPIError(http.StatusForbidden, "image_forbidden")
		}

		ctx := c.Request().Context()
		// 持ち主の鍵と削除状態を毎回確認する（即時反映）。
		// image_objects は id だけだと全パーティションを見るので、対応表 image_keys から引く（#121）
		// 本体がキャッシュに無く署名が検証済みのときだけ、本体も同じ問い合わせで取る（DB 往復を 1 回減らす）。
		// 未検証の署名で本体を取らないのは、不正な署名の要求で大きな本体を引かないため。
		sigKey := newVerifiedSigKey(input.ImageID, exp, sig)
		entry, cached := imageObjectCache.get(input.ImageID)
		withData := !cached && verifiedImageSigs.has(sigKey)
		var object struct {
			SecretKeyHash string    `db:"secret_key_hash"`
			Alive         bool      `db:"alive"`
			CreatedAt     time.Time `db:"created_at"`
			MediaType     *string   `db:"media_type"`
			Data          []byte    `db:"data"`
		}
		err = h.DB.GetContext(ctx, &object, `
   SELECT u.secret_key_hash,
   (SELECT media_type FROM image_objects WHERE $2::boolean AND id = i.id AND created_at = i.created_at) AS media_type,
   (SELECT data FROM image_objects WHERE $2::boolean AND id = i.id AND created_at = i.created_at) AS data, (
     COALESCE(u.profile_image_id = i.id, false)
     OR EXISTS (
       SELECT 1 FROM posts p
       WHERE p.image_id = i.id AND p.author_id = u.id AND p.deleted_at IS NULL
     )
     OR EXISTS (
       SELECT 1 FROM realmojis r
       JOIN posts p ON p.id = r.post_id AND p.deleted_at IS NULL
       JOIN users author ON author.id = p.author_id AND author.status = 'active'
       WHERE r.image_id = i.id AND r.reactor_id = u.id AND r.replaced_at IS NULL AND r.deleted_at IS NULL
     )
   ) AS alive, i.created_at
   FROM image_keys i JOIN users u ON u.id = i.owner_user_id AND u.status = 'active'
   WHERE i.id = $1
  `, input.ImageID, withData)
		if errors.Is(err, sql.ErrNoRows) {
			return NewAPIError(http.StatusForbidden, "image_forbidden")
		}
		if err != nil {
			return err
		}
		if !verifiedImageSigs.has(sigKey) {
			key, err := imageSigningKey(object.SecretKeyHash)
			if err != nil {
				return err
			}
			if !hmac.Equal(sig, imageSignature(key, input.ImageID, exp)) {
				c.Set("abuse", "invalid_signature")
				return NewAPIError(http.StatusForbidden, "image_forbidden")
			}
			verifiedImageSigs.add(sigKey)
		}
		if !object.Alive {
			return NewAPIError(http.StatusForbidden, "image_forbidden")
		}
		if exp <= time.Now().Unix() {
			return NewAPIError(http.StatusForbidden, "image_forbidden")
		}
		if withData {
			// 別々に引いていたときと同じく、本体が無ければ 403
			if object.MediaType == nil {
				return NewAPIError(http.StatusForbidden, "image_forbidden")
			}
			entry = imageCacheEntry{id: input.ImageID, mediaType: *object.MediaType, data: object.Data}
			imageObjectCache.put(entry.id, entry.mediaType, entry.data)
		} else if !cached {
			entry = imageCacheEntry{id: input.ImageID}
			err = h.DB.QueryRowxContext(ctx, `SELECT media_type, data FROM image_objects WHERE id = $1 AND created_at = $2`, input.ImageID, object.CreatedAt).Scan(&entry.mediaType, &entry.data)
			if errors.Is(err, sql.ErrNoRows) {
				return NewAPIError(http.StatusForbidden, "image_forbidden")
			}
			if err != nil {
				return err
			}
			imageObjectCache.put(entry.id, entry.mediaType, entry.data)
		}
		return c.Blob(http.StatusOK, entry.mediaType, entry.data)
	}
}
