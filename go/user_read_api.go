package isureal

import (
	"database/sql"
	"errors"
	"net/http"
	"strings"

	"github.com/labstack/echo/v5"
	"golang.org/x/text/cases"
	"golang.org/x/text/unicode/norm"
)

type searchUsersRequest struct {
	Q              string `query:"q"`
	BeforeUsername string `query:"beforeUsername"`
	BeforeID       string `query:"beforeId"`
}

type searchUsersResponse struct {
	Items   []relationshipUserResponse `json:"items"`
	HasMore bool                       `json:"hasMore"`
}

func normalizeSearchText(value string) string {
	return norm.NFC.String(cases.Fold().String(norm.NFC.String(value)))
}

// この文字だけで出来た display_name は normalizeSearchText（NFC + case fold）が「ASCII を小文字にする」だけと一致する
// （印字可能 ASCII・ひらがな・カタカナ・長音・CJK 統合漢字・ハングル音節。結合文字・互換文字は含めない）。
// DB の lower() と突き合わせて全行一致を確認済み（#75）
const searchSafeDisplayName = `^[ -~\u3041-\u3096\u30A1-\u30FA\u30FC\u4E00-\u9FFF\uAC00-\uD7A3]*$`

func searchUsersHandler(h *HandlerContext) echo.HandlerFunc {
	return func(c *echo.Context) error {
		var input searchUsersRequest
		if err := c.Bind(&input); err != nil {
			return err
		}
		if !validUserText(input.Q, 1, 100) {
			return NewAPIError(http.StatusBadRequest, "invalid_relationship")
		}
		if (input.BeforeUsername == "") != (input.BeforeID == "") {
			return NewAPIError(http.StatusBadRequest, "invalid_pagination")
		}
		ctx := c.Request().Context()
		viewer := GetSession(c).UserID
		query := normalizeSearchText(input.Q)
		// username は ^[a-z0-9_.-]+$ なので正規化しても変わらず、strpos で同じ判定になる。
		// display_name は正規化済みの search_display（#122）で判定するので、SQL の条件が下の Go 側の判定と一致し、LIMIT 51 で止められる。
		// search_display が NULL（初期化前）の行だけ従来の候補条件（searchSafeDisplayName なら lower()、それ以外は Go 側で判定）
		// 絞り込みは id だけを返す内側で行い、idx_users_username_id_search_cover（sql/80-add-index.sql）の Index Only Scan に乗せる（#248）。
		// 全列を持つ外側は内側の 51 件だけ users_pkey で引く。並び順・件数は変えない
		var users []User
		if err := h.DB.SelectContext(ctx, &users, `
   SELECT `+strings.Join(userColumns, ", ")+` FROM users
   WHERE id IN (
     SELECT id FROM users
     WHERE status='active' AND id<>$1 AND ($2='' OR (username,id)>($2,$3))
       AND id NOT IN (SELECT CASE WHEN blocker_id=$1 THEN blocked_id ELSE blocker_id END FROM block_relations WHERE blocker_id=$1 OR blocked_id=$1)
       AND (strpos(username, $4) > 0 OR CASE WHEN search_display IS NULL THEN strpos(lower(display_name), $4) > 0 OR display_name !~ $5
                                            ELSE strpos(search_display, $4) > 0 END)
     ORDER BY username, id LIMIT 51)
   ORDER BY username, id
  `, viewer, input.BeforeUsername, input.BeforeID, query, searchSafeDisplayName); err != nil {
			return err
		}
		items := make([]relationshipUserResponse, 0)
		for _, user := range users {
			if !strings.Contains(normalizeSearchText(user.Username), query) && !strings.Contains(normalizeSearchText(user.DisplayName), query) {
				continue
			}
			if err := user.setImageKey(); err != nil {
				return err
			}
			if user.AccountType != "certified_public" {
				user.Bio = ""
			}
			items = append(items, newRelationshipUserResponse(user))
			if len(items) == 51 {
				break
			}
		}
		hasMore := len(items) > 50
		if hasMore {
			items = items[:50]
		}
		return c.JSON(http.StatusOK, searchUsersResponse{Items: items, HasMore: hasMore})
	}
}

type getUserRequest struct {
	UserID string `param:"userId"`
}

type getUserResponse relationshipUserResponse

func getUserHandler(h *HandlerContext) echo.HandlerFunc {
	return func(c *echo.Context) error {
		var input getUserRequest
		if err := c.Bind(&input); err != nil {
			return err
		}
		ctx := c.Request().Context()
		viewer := GetSession(c).UserID
		var row struct {
			User
			Blocked bool `db:"blocked"`
			Friend  bool `db:"friend"`
		}
		err := h.DB.GetContext(ctx, &row, `
   SELECT `+strings.Join(userColumns, ", ")+`,
    EXISTS(SELECT 1 FROM block_relations WHERE (blocker_id=$1 AND blocked_id=$2) OR (blocker_id=$2 AND blocked_id=$1)) AS blocked,
    EXISTS(SELECT 1 FROM friend_edges WHERE user_id=$1 AND friend_id=$2) AS friend
   FROM users WHERE id = $2 AND status = 'active'
  `, viewer, input.UserID)
		if errors.Is(err, sql.ErrNoRows) || row.Blocked {
			return NewAPIError(http.StatusNotFound, "relationship_not_found")
		}
		if err != nil {
			return err
		}
		user := row.User
		if err := user.setImageKey(); err != nil {
			return err
		}
		if user.AccountType != "certified_public" && !row.Friend {
			user.Bio = ""
		}
		return c.JSON(http.StatusOK, getUserResponse(newRelationshipUserResponse(user)))
	}
}

func newRelationshipUserResponse(user User) relationshipUserResponse {
	return relationshipUserResponse{ID: user.ID, Username: user.Username, DisplayName: user.DisplayName, ProfileImageURL: newGetMeResponse(user).ProfileImageURL, Bio: user.Bio, AccountType: user.AccountType}
}
