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
		// 持ち主の鍵と削除状態を毎回 DB で確認する（即時反映）。持ち主は不変なのでメモリに持ち、
		// ヒット時は image_objects（36 パーティション）を引かずに posts / realmojis / users だけで判定する。
		var object struct {
			SecretKeyHash string  `db:"secret_key_hash"`
			Alive         bool    `db:"alive"`
			OwnerUserID   string  `db:"owner_user_id"`
			MediaType     *string `db:"media_type"`
			Data          []byte  `db:"data"`
		}
		ownerID, ownerKnown := imageOwners.get(input.ImageID)
		if ownerKnown {
			err = h.DB.GetContext(ctx, &object, `SELECT u.secret_key_hash, `+imageAliveSQL+` AS alive
   FROM users u WHERE u.id = $2 AND u.status = 'active'`, input.ImageID, ownerID)
		} else {
			// ミス時は生存確認と本体取得を 1 往復にまとめる。死んだ画像の data は読まない。
			err = h.DB.GetContext(ctx, &object, `SELECT i.owner_user_id, u.secret_key_hash, a.alive, i.media_type,
   CASE WHEN a.alive THEN i.data END AS data
   FROM image_objects i JOIN users u ON u.id = i.owner_user_id AND u.status = 'active'
   CROSS JOIN LATERAL (SELECT `+imageAliveSQL+` AS alive) a
   WHERE i.id = $1`, input.ImageID)
		}
		if errors.Is(err, sql.ErrNoRows) {
			return NewAPIError(http.StatusForbidden, "image_forbidden")
		}
		if err != nil {
			return err
		}
		if !ownerKnown {
			imageOwners.put(input.ImageID, object.OwnerUserID)
		}
		sigKey := newVerifiedSigKey(input.ImageID, exp, sig)
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
		entry, ok := imageObjectCache.get(input.ImageID)
		if !ok {
			if object.MediaType == nil || object.Data == nil {
				// 持ち主キャッシュのヒット後にキャッシュから落ちた分だけ、本体を別途取る
				entry = imageCacheEntry{id: input.ImageID}
				err = h.DB.QueryRowxContext(ctx, `SELECT media_type, data FROM image_objects WHERE id = $1`, input.ImageID).Scan(&entry.mediaType, &entry.data)
				if errors.Is(err, sql.ErrNoRows) {
					return NewAPIError(http.StatusForbidden, "image_forbidden")
				}
				if err != nil {
					return err
				}
			} else {
				entry = imageCacheEntry{id: input.ImageID, mediaType: *object.MediaType, data: object.Data}
			}
			imageObjectCache.put(entry.id, entry.mediaType, entry.data)
		}
		return c.Blob(http.StatusOK, entry.mediaType, entry.data)
	}
}

// $1 = image_id、u = 持ち主の users 行。image_objects を参照しない。
const imageAliveSQL = `(
     COALESCE(u.profile_image_id = $1, false)
     OR EXISTS (
       SELECT 1 FROM posts p
       WHERE p.image_id = $1 AND p.author_id = u.id AND p.deleted_at IS NULL
     )
     OR EXISTS (
       SELECT 1 FROM realmojis r
       JOIN posts p ON p.id = r.post_id AND p.deleted_at IS NULL
       JOIN users author ON author.id = p.author_id AND author.status = 'active'
       WHERE r.image_id = $1 AND r.reactor_id = u.id AND r.replaced_at IS NULL AND r.deleted_at IS NULL
     )
   )`
