package isureal

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/jmoiron/sqlx"
	"github.com/labstack/echo/v5"
)

type getMemoriesRequest struct {
	Month          string     `query:"month"`
	BeforePostedAt *time.Time `query:"beforePostedAt"`
	BeforeID       string     `query:"beforeId"`
}

type memoryPostResponse struct {
	createPostResponse
	RealMojis []replaceRealMojiResponse `json:"realmojis"`
	Comments  []createCommentResponse   `json:"comments"`
}

type getMemoriesResponse struct {
	Items   []memoryPostResponse `json:"items"`
	HasMore bool                 `json:"hasMore"`
}

type getMemoriesByDateRequest struct {
	Date           string     `param:"date"`
	BeforePostedAt *time.Time `query:"beforePostedAt"`
	BeforeID       string     `query:"beforeId"`
}

type getMemoriesByDateResponse getMemoriesResponse

func getMemoriesHandler(h *HandlerContext) echo.HandlerFunc {
	return func(c *echo.Context) error {
		var input getMemoriesRequest
		if err := c.Bind(&input); err != nil {
			return err
		}
		start, err := time.Parse("2006-01", input.Month)
		if err != nil {
			return NewAPIError(http.StatusBadRequest, "invalid_memory_period")
		}
		response, err := listMemories(c.Request().Context(), h.DB, GetSession(c).UserID, start, start.AddDate(0, 1, 0), input.BeforePostedAt, input.BeforeID)
		if err != nil {
			return err
		}
		return c.JSON(http.StatusOK, response)
	}
}

func getMemoriesByDateHandler(h *HandlerContext) echo.HandlerFunc {
	return func(c *echo.Context) error {
		var input getMemoriesByDateRequest
		if err := c.Bind(&input); err != nil {
			return err
		}
		start, err := time.Parse(time.DateOnly, input.Date)
		if err != nil {
			return NewAPIError(http.StatusBadRequest, "invalid_memory_period")
		}
		response, err := listMemories(c.Request().Context(), h.DB, GetSession(c).UserID, start, start.AddDate(0, 0, 1), input.BeforePostedAt, input.BeforeID)
		if err != nil {
			return err
		}
		return c.JSON(http.StatusOK, getMemoriesByDateResponse(response))
	}
}

// 投稿一覧と、その realmojis / comments を 1 文で取る（#238。europe / america は往復 1 回 ≒ 100ms）。
// 3 種類の行を UNION ALL で同じ列に寄せ、整形は今までどおり Go 側（newPostResponse など）で行う
type memoryRow struct {
	Kind            string     `db:"kind"`
	ID              string     `db:"id"`
	PostID          *string    `db:"post_id"`
	UserID          string     `db:"user_id"`
	PostingWindowID *string    `db:"posting_window_id"`
	ImageID         *string    `db:"image_id"`
	Text            string     `db:"text"`
	Visibility      *string    `db:"visibility"`
	LocationKind    *string    `db:"location_kind"`
	Latitude        *float64   `db:"latitude"`
	Longitude       *float64   `db:"longitude"`
	Locality        *string    `db:"locality"`
	TS              time.Time  `db:"ts"`
	OnTimeDeadline  *time.Time `db:"on_time_deadline"`
	WindowEndedAt   *time.Time `db:"window_ended_at"`
	ReplacedAt      *time.Time `db:"replaced_at"`
	DeletedAt       *time.Time `db:"deleted_at"`
	User            User       `db:"u"`
}

func listMemories(ctx context.Context, db sqlx.QueryerContext, userID string, start, end time.Time, beforeAt *time.Time, beforeID string) (getMemoriesResponse, error) {
	var response getMemoriesResponse
	if (beforeAt == nil) != (beforeID == "") {
		return response, NewAPIError(http.StatusBadRequest, "invalid_pagination")
	}
	userCols := userColumnsAs("u", "u")
	psUserCols := make([]string, len(userColumns))
	for i, column := range userColumns {
		psUserCols[i] = `"u.` + column + `"`
	}
	var rows []memoryRow
	if err := sqlx.SelectContext(ctx, db, &rows, `
  WITH ps AS (
    SELECT `+postColumns+`, `+userCols+`,
           row_number() OVER (ORDER BY p.posted_at DESC, p.id DESC) AS ord
    FROM posts p
    JOIN posting_windows w ON w.id = p.posting_window_id
    JOIN users u ON u.id = p.author_id AND u.status = 'active'
    WHERE p.deleted_at IS NULL
      AND p.author_id = $1 AND p.local_date >= $2 AND p.local_date < $3
      AND ($4::timestamptz IS NULL OR (p.posted_at, p.id) < ($4, $5))
    ORDER BY p.posted_at DESC, p.id DESC LIMIT 51
  ), top AS (SELECT id FROM ps WHERE ord <= 50)
  SELECT * FROM (
    SELECT 'p' AS kind, id, NULL::varchar AS post_id, author_id AS user_id, posting_window_id, image_id, caption AS text,
           visibility, location_kind, latitude, longitude, locality, posted_at AS ts, on_time_deadline, window_ended_at,
           NULL::timestamptz AS replaced_at, NULL::timestamptz AS deleted_at, `+strings.Join(psUserCols, ", ")+`
    FROM ps
    UNION ALL
    SELECT 'r', r.id, r.post_id, r.reactor_id, NULL, r.image_id, r.reaction_type,
           NULL, NULL, NULL, NULL, NULL, r.created_at, NULL, NULL,
           r.replaced_at, r.deleted_at, `+userCols+`
    FROM realmojis r JOIN users u ON u.id = r.reactor_id AND u.status = 'active'
    WHERE r.replaced_at IS NULL AND r.deleted_at IS NULL AND r.post_id IN (SELECT id FROM top)
    UNION ALL
    SELECT 'c', c.id, c.post_id, c.author_id, NULL, NULL, c.body,
           NULL, NULL, NULL, NULL, NULL, c.created_at, NULL, NULL,
           NULL, c.deleted_at, `+userCols+`
    FROM comments c JOIN users u ON u.id = c.author_id AND u.status = 'active'
    WHERE c.deleted_at IS NULL AND c.post_id IN (SELECT id FROM top)
  ) x
  ORDER BY kind, CASE WHEN kind = 'c' THEN ts END, ts DESC, CASE WHEN kind = 'c' THEN id END, id DESC
 `, userID, start, end, beforeAt, beforeID); err != nil {
		return response, err
	}

	// 署名は先に 16 個ずつまとめて作る（selectPostsWithAuthor と同じ。あとの newXxxResponse はキャッシュに当たる）
	reqs := make([]sigReq, 0, len(rows)*2)
	for i := range rows {
		row := &rows[i]
		if err := row.User.setImageKey(); err != nil {
			return response, err
		}
		if row.Kind == "c" {
			reqs = append(reqs, profileSigReqs(row.User)...)
		} else {
			reqs = append(reqs, imageSigReqs(deref(row.ImageID), row.User)...)
		}
	}
	prewarmSigs(reqs)

	items := make([]memoryPostResponse, 0, 51)
	for i := range rows {
		row := &rows[i]
		if row.Kind != "p" {
			continue
		}
		post := Post{
			ID: row.ID, AuthorID: row.UserID, PostingWindowID: deref(row.PostingWindowID), ImageID: deref(row.ImageID),
			Caption: row.Text, Visibility: deref(row.Visibility),
			PostLocation: PostLocation{Kind: deref(row.LocationKind), Latitude: row.Latitude, Longitude: row.Longitude, Locality: row.Locality},
			PostedAt: row.TS, OnTimeDeadline: derefTime(row.OnTimeDeadline), WindowEndedAt: row.WindowEndedAt,
		}
		items = append(items, memoryPostResponse{
			createPostResponse: newPostResponse(post, row.User),
			RealMojis:          make([]replaceRealMojiResponse, 0),
			Comments:           make([]createCommentResponse, 0),
		})
	}
	response.HasMore = len(items) > 50
	if response.HasMore {
		items = items[:50]
	}
	itemByPostID := make(map[string]*memoryPostResponse, len(items))
	for i := range items {
		itemByPostID[items[i].ID] = &items[i]
	}
	for i := range rows {
		row := &rows[i]
		if row.Kind == "p" {
			continue
		}
		item := itemByPostID[deref(row.PostID)]
		if item == nil {
			continue
		}
		switch row.Kind {
		case "r":
			if len(item.RealMojis) < 50 {
				reaction := RealMoji{ID: row.ID, PostID: deref(row.PostID), ReactorID: row.UserID, ImageID: deref(row.ImageID), ReactionType: row.Text, CreatedAt: row.TS, ReplacedAt: row.ReplacedAt, DeletedAt: row.DeletedAt}
				item.RealMojis = append(item.RealMojis, newRealMojiResponse(reaction, row.User))
			}
		case "c":
			if len(item.Comments) < 50 {
				comment := Comment{ID: row.ID, PostID: deref(row.PostID), AuthorID: row.UserID, Body: row.Text, CreatedAt: row.TS, DeletedAt: row.DeletedAt}
				item.Comments = append(item.Comments, newCommentResponse(comment, row.User))
			}
		}
	}
	response.Items = items
	return response, nil
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func derefTime(t *time.Time) time.Time {
	if t == nil {
		return time.Time{}
	}
	return *t
}
