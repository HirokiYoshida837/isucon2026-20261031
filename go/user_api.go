package isureal

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/labstack/echo/v5"
)

type registerRequest struct {
	Username    string `json:"username"`
	DisplayName string `json:"displayName"`
	Region      string `json:"region"`
}

type registerResponse struct {
	SecretKey string              `json:"secretKey"`
	User      sessionUserResponse `json:"user"`
}

type loginRequest struct {
	Username  string `json:"username"`
	SecretKey string `json:"secretKey"`
}

type loginResponse struct {
	Token string              `json:"token"`
	User  sessionUserResponse `json:"user"`
}

type sessionUserResponse struct {
	ID          string `json:"id"`
	Username    string `json:"username"`
	DisplayName string `json:"displayName"`
	Bio         string `json:"bio"`
	Region      string `json:"region"`
	AccountType string `json:"accountType"`
	Status      string `json:"status"`
}

type getMeResponse struct {
	sessionUserResponse
	ProfileImageURL *string `json:"profileImageUrl"`
}

type updateMeRequest struct {
	DisplayName *string `json:"displayName"`
	Bio         *string `json:"bio"`
	Region      *string `json:"region"`
}

type updateMeResponse getMeResponse

func newSessionUserResponse(user User) sessionUserResponse {
	return sessionUserResponse{
		ID: user.ID, Username: user.Username, DisplayName: user.DisplayName,
		Bio: user.Bio, Region: user.Region, AccountType: user.AccountType, Status: user.Status,
	}
}

func newGetMeResponse(user User) getMeResponse {
	response := getMeResponse{sessionUserResponse: newSessionUserResponse(user)}
	if user.ProfileImageID != nil {
		url := signedImageURL(user, *user.ProfileImageID)
		response.ProfileImageURL = &url
	}
	return response
}

func registerHandler(h *HandlerContext) echo.HandlerFunc {
	return func(c *echo.Context) error {
		var input registerRequest
		if err := c.Bind(&input); err != nil {
			return err
		}
		c.Set("username", input.Username)
		if !usernamePattern.MatchString(input.Username) || !validUserText(input.DisplayName, 1, 100) || !validRegion(input.Region) {
			return NewAPIError(http.StatusBadRequest, "invalid_registration")
		}
		secretKey := uuid.NewString()
		hash := sha256.Sum256([]byte(secretKey))
		ctx := c.Request().Context()
		// ユーザー名の一意性は users(username) の UNIQUE index（sql/80-add-index.sql）で担保する。
		// テーブルロックと NOT EXISTS は要らない。重複なら ON CONFLICT で 0 行になる
		var user User
		err := h.DB.GetContext(ctx, &user, `
   INSERT INTO users (id, username, secret_key_hash, display_name, region, search_display)
   VALUES ($1::varchar, $2::varchar, $3::text, $4::text, $5::text, $6::text)
   ON CONFLICT (username) DO NOTHING
   RETURNING `+strings.Join(userColumns, ", "),
			uuid.NewString(), input.Username, hex.EncodeToString(hash[:]), input.DisplayName, input.Region, normalizeSearchText(input.DisplayName))
		if errors.Is(err, sql.ErrNoRows) {
			return NewAPIError(http.StatusConflict, "username_taken")
		}
		if err != nil {
			return err
		}
		return c.JSON(http.StatusCreated, registerResponse{SecretKey: secretKey, User: newSessionUserResponse(user)})
	}
}

func loginHandler(h *HandlerContext) echo.HandlerFunc {
	return func(c *echo.Context) error {
		var input loginRequest
		if err := c.Bind(&input); err != nil {
			return err
		}
		c.Set("username", input.Username)
		if !usernamePattern.MatchString(input.Username) {
			return NewAPIError(http.StatusUnauthorized, "invalid_credentials")
		}
		ctx := c.Request().Context()
		hash := sha256.Sum256([]byte(input.SecretKey))
		token := uuid.NewString()
		tokenHashBytes := sha256.Sum256([]byte(token))
		tokenHash := hex.EncodeToString(tokenHashBytes[:])
		var res struct {
			Code    int        `db:"code"`
			UID     *string    `db:"uid"`
			Uname   *string    `db:"uname"`
			Dname   *string    `db:"dname"`
			Ubio    *string    `db:"ubio"`
			Uregion *string    `db:"uregion"`
			Utype   *string    `db:"utype"`
			Ustatus *string    `db:"ustatus"`
			Created *time.Time `db:"screated"`
		}
		if err := h.DB.GetContext(ctx, &res, `SELECT * FROM login_user($1, $2, $3, $4)`, input.Username, hex.EncodeToString(hash[:]), uuid.NewString(), tokenHash); err != nil {
			return err
		}
		if res.Code != 0 {
			return NewAPIError(http.StatusUnauthorized, "invalid_credentials")
		}
		user := User{ID: *res.UID, Username: *res.Uname, DisplayName: *res.Dname, Bio: *res.Ubio, Region: *res.Uregion, AccountType: *res.Utype, Status: *res.Ustatus}
		session := &Session{UserID: user.ID, TokenHash: tokenHash, CreatedAt: *res.Created, Token: token}
		cached := *session
		cached.Region = user.Region
		sessionCache.putLogin(cached)
		return c.JSON(http.StatusOK, loginResponse{Token: session.Token, User: newSessionUserResponse(user)})
	}
}

func logoutHandler(h *HandlerContext) echo.HandlerFunc {
	return func(c *echo.Context) error {
		session := GetSession(c)
		if session == nil {
			return NewAPIError(http.StatusUnauthorized, "invalid_session")
		}
		if err := InvalidateLoginSession(h.DB, session.UserID, session.TokenHash); err != nil {
			return err
		}
		return c.NoContent(http.StatusNoContent)
	}
}

func getMeHandler(h *HandlerContext) echo.HandlerFunc {
	return func(c *echo.Context) error {
		session := GetSession(c)
		if session == nil {
			return NewAPIError(http.StatusUnauthorized, "invalid_session")
		}
		user, err := getUser(c.Request().Context(), h.DB, session.UserID)
		if errors.Is(err, sql.ErrNoRows) {
			return NewAPIError(http.StatusNotFound, "account_not_found")
		}
		if err != nil {
			return err
		}
		return c.JSON(http.StatusOK, newGetMeResponse(user))
	}
}

func updateMeHandler(h *HandlerContext) echo.HandlerFunc {
	return func(c *echo.Context) error {
		session := GetSession(c)
		if session == nil {
			return NewAPIError(http.StatusUnauthorized, "invalid_session")
		}
		var input updateMeRequest
		if err := c.Bind(&input); err != nil {
			return err
		}
		if (input.DisplayName == nil && input.Bio == nil && input.Region == nil) ||
			(input.DisplayName != nil && !validUserText(*input.DisplayName, 1, 100)) ||
			(input.Bio != nil && !validUserText(*input.Bio, 0, 500)) ||
			(input.Region != nil && !validRegion(*input.Region)) {
			return NewAPIError(http.StatusBadRequest, "invalid_profile")
		}
		ctx := c.Request().Context()
		var searchDisplay *string
		if input.DisplayName != nil {
			normalized := normalizeSearchText(*input.DisplayName)
			searchDisplay = &normalized
		}
		var user User
		err := h.DB.GetContext(ctx, &user, `UPDATE users SET display_name = COALESCE($2, display_name), search_display = COALESCE($5, search_display), bio = COALESCE($3, bio), region = COALESCE($4, region), updated_at = clock_timestamp() WHERE id = $1 AND status = 'active' RETURNING `+strings.Join(userColumns, ", "), session.UserID, input.DisplayName, input.Bio, input.Region, searchDisplay)
		if errors.Is(err, sql.ErrNoRows) {
			return NewAPIError(http.StatusNotFound, "account_not_found")
		}
		if err != nil {
			return err
		}
		if err := user.setImageKey(); err != nil {
			return err
		}
		return c.JSON(http.StatusOK, updateMeResponse(newGetMeResponse(user)))
	}
}
