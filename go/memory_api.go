package isureal

import (
	"context"
	"encoding/json"
	"net/http"
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

// memoryRow は投稿 1 件と、その realmojis / comments（JSON 配列）を 1 行で受ける（#238: 2 往復 → 1 往復）
type memoryRow struct {
	PostWithAuthor
	RealMojisJSON []byte `db:"realmojis"`
	CommentsJSON  []byte `db:"comments"`
}

// memoryUserJSON は SQL の jsonb_build_object（users の列名がキー）を受ける
type memoryUserJSON struct {
	ID             string  `json:"id"`
	Username       string  `json:"username"`
	SecretKeyHash  string  `json:"secret_key_hash"`
	DisplayName    string  `json:"display_name"`
	Bio            string  `json:"bio"`
	ProfileImageID *string `json:"profile_image_id"`
	Region         string  `json:"region"`
	AccountType    string  `json:"account_type"`
	Status         string  `json:"status"`
}

func (u memoryUserJSON) user() User {
	return User{ID: u.ID, Username: u.Username, SecretKeyHash: u.SecretKeyHash, DisplayName: u.DisplayName, Bio: u.Bio,
		ProfileImageID: u.ProfileImageID, Region: u.Region, AccountType: u.AccountType, Status: u.Status}
}

type memoryRealMojiJSON struct {
	ID           string         `json:"id"`
	ImageID      string         `json:"image_id"`
	ReactionType string         `json:"reaction_type"`
	CreatedAt    time.Time      `json:"created_at"`
	Reactor      memoryUserJSON `json:"reactor"`
}

type memoryCommentJSON struct {
	ID        string         `json:"id"`
	Body      string         `json:"body"`
	CreatedAt time.Time      `json:"created_at"`
	Author    memoryUserJSON `json:"author"`
}

const memoryUserJSONObject = `jsonb_build_object('id', u.id, 'username', u.username, 'secret_key_hash', u.secret_key_hash,
  'display_name', u.display_name, 'bio', u.bio, 'profile_image_id', u.profile_image_id, 'region', u.region,
  'account_type', u.account_type, 'status', u.status)`

func listMemories(ctx context.Context, db sqlx.QueryerContext, userID string, start, end time.Time, beforeAt *time.Time, beforeID string) (getMemoriesResponse, error) {
	var response getMemoriesResponse
	if (beforeAt == nil) != (beforeID == "") {
		return response, NewAPIError(http.StatusBadRequest, "invalid_pagination")
	}
	// 投稿一覧（51 件）と、各投稿の realmojis（新しい順 50 件）・comments（古い順 50 件）を 1 文で取る。
	// 並び順・件数・JOIN 条件は selectPostsWithAuthor / selectCurrentRealMojis / selectCommentsWithAuthor と同じ
	var rows []memoryRow
	if err := sqlx.SelectContext(ctx, db, &rows, `
  WITH p AS (
    SELECT `+postColumns+`, `+userColumnsAs("u", "author")+`
    FROM posts p
    JOIN posting_windows w ON w.id = p.posting_window_id
    JOIN users u ON u.id = p.author_id AND u.status = 'active'
    WHERE p.deleted_at IS NULL
      AND p.author_id = $1 AND p.local_date >= $2 AND p.local_date < $3
      AND ($4::timestamptz IS NULL OR (p.posted_at, p.id) < ($4, $5))
    ORDER BY p.posted_at DESC, p.id DESC LIMIT 51
  )
  SELECT p.*,
    (SELECT COALESCE(jsonb_agg(x.o ORDER BY x.created_at DESC, x.id DESC), '[]'::jsonb)::text FROM (
       SELECT r.created_at, r.id, jsonb_build_object('id', r.id, 'image_id', r.image_id, 'reaction_type', r.reaction_type,
                'created_at', r.created_at, 'reactor', `+memoryUserJSONObject+`) AS o
       FROM realmojis r JOIN users u ON u.id = r.reactor_id AND u.status = 'active'
       WHERE r.post_id = p.id AND r.replaced_at IS NULL AND r.deleted_at IS NULL
       ORDER BY r.created_at DESC, r.id DESC LIMIT 50) x) AS realmojis,
    (SELECT COALESCE(jsonb_agg(x.o ORDER BY x.created_at, x.id), '[]'::jsonb)::text FROM (
       SELECT c.created_at, c.id, jsonb_build_object('id', c.id, 'body', c.body, 'created_at', c.created_at,
                'author', `+memoryUserJSONObject+`) AS o
       FROM comments c JOIN users u ON u.id = c.author_id AND u.status = 'active'
       WHERE c.post_id = p.id AND c.deleted_at IS NULL
       ORDER BY c.created_at, c.id LIMIT 50) x) AS comments
  FROM p
  ORDER BY p.posted_at DESC, p.id DESC
 `, userID, start, end, beforeAt, beforeID); err != nil {
		return response, err
	}
	response.HasMore = len(rows) > 50
	if response.HasMore {
		rows = rows[:50]
	}
	items := make([]memoryPostResponse, 0, len(rows))
	reqs := make([]sigReq, 0, len(rows)*4)
	type pending struct {
		reactions []RealMojiWithReactor
		comments  []CommentWithAuthor
	}
	pendings := make([]pending, 0, len(rows))
	for i := range rows {
		row := &rows[i]
		if err := row.Author.setImageKey(); err != nil {
			return response, err
		}
		reqs = append(reqs, imageSigReqs(row.ImageID, row.Author)...)
		var rj []memoryRealMojiJSON
		if err := json.Unmarshal(row.RealMojisJSON, &rj); err != nil {
			return response, err
		}
		var cj []memoryCommentJSON
		if err := json.Unmarshal(row.CommentsJSON, &cj); err != nil {
			return response, err
		}
		pd := pending{reactions: make([]RealMojiWithReactor, 0, len(rj)), comments: make([]CommentWithAuthor, 0, len(cj))}
		for _, r := range rj {
			reactor := r.Reactor.user()
			if err := reactor.setImageKey(); err != nil {
				return response, err
			}
			// pgx が timestamptz を time.Local で返すのに合わせる（JSON の表記を変えない）
			pd.reactions = append(pd.reactions, RealMojiWithReactor{
				RealMoji: RealMoji{ID: r.ID, PostID: row.ID, ReactorID: reactor.ID, ImageID: r.ImageID, ReactionType: r.ReactionType, CreatedAt: r.CreatedAt.In(time.Local)},
				Reactor:  reactor,
			})
			reqs = append(reqs, imageSigReqs(r.ImageID, reactor)...)
		}
		for _, c := range cj {
			author := c.Author.user()
			if err := author.setImageKey(); err != nil {
				return response, err
			}
			pd.comments = append(pd.comments, CommentWithAuthor{
				Comment: Comment{ID: c.ID, PostID: row.ID, AuthorID: author.ID, Body: c.Body, CreatedAt: c.CreatedAt.In(time.Local)},
				Author:  author,
			})
			reqs = append(reqs, profileSigReqs(author)...)
		}
		pendings = append(pendings, pd)
	}
	prewarmSigs(reqs) // 画像の署名を 16 個ずつまとめて作る（あとの new*Response はキャッシュに当たる）
	for i := range rows {
		item := memoryPostResponse{
			createPostResponse: newPostResponse(rows[i].Post, rows[i].Author),
			RealMojis:          make([]replaceRealMojiResponse, 0, len(pendings[i].reactions)),
			Comments:           make([]createCommentResponse, 0, len(pendings[i].comments)),
		}
		for _, reaction := range pendings[i].reactions {
			item.RealMojis = append(item.RealMojis, newRealMojiResponse(reaction.RealMoji, reaction.Reactor))
		}
		for _, comment := range pendings[i].comments {
			item.Comments = append(item.Comments, newCommentResponse(comment.Comment, comment.Author))
		}
		items = append(items, item)
	}
	response.Items = items
	return response, nil
}
