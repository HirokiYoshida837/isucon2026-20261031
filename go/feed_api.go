package isureal

import (
	"database/sql"
	"errors"
	"net/http"
	"time"

	"github.com/jmoiron/sqlx"
	"github.com/labstack/echo/v5"
)

type getFeedRequest struct {
	Feed           string     `param:"feed"`
	BeforeScore    *int64     `query:"beforeScore"`
	BeforePostedAt *time.Time `query:"beforePostedAt"`
	BeforeID       string     `query:"beforeId"`
}

type feedItemResponse struct {
	createPostResponse
	Score *int64 `json:"score,omitempty"`
}

type getFeedResponse struct {
	Items   []feedItemResponse `json:"items"`
	HasMore bool               `json:"hasMore"`
}

func getFeedHandler(h *HandlerContext) echo.HandlerFunc {
	return func(c *echo.Context) error {
		var input getFeedRequest
		if err := c.Bind(&input); err != nil {
			return err
		}
		switch input.Feed {
		case "friends", "friends-of-friends", "public", "following":
		default:
			return NewAPIError(http.StatusBadRequest, "invalid_post")
		}
		if (input.BeforePostedAt == nil) != (input.BeforeID == "") {
			return NewAPIError(http.StatusBadRequest, "invalid_pagination")
		}
		ranked := input.Feed == "friends-of-friends"
		if ranked && (input.BeforeScore == nil) != (input.BeforeID == "") {
			return NewAPIError(http.StatusBadRequest, "invalid_pagination")
		}
		ctx := c.Request().Context()
		viewerID := GetSession(c).UserID
		if ranked {
			// 友達の全辺（FriendsOfFriends / MutualFriends）は読まない。共通の友達数・スコアは SQL で出す
			viewer, err := loadViewerContext(ctx, h.DB, viewerID, false)
			if errors.Is(err, sql.ErrNoRows) {
				return c.JSON(http.StatusOK, getFeedResponse{Items: []feedItemResponse{}})
			}
			if err != nil {
				return err
			}
			if !viewer.HasPostedToday {
				return c.JSON(http.StatusOK, getFeedResponse{Items: []feedItemResponse{}})
			}
			blocked := make([]string, 0, len(viewer.Blocked))
			for id := range viewer.Blocked {
				blocked = append(blocked, id)
			}
			return friendsOfFriendsFeed(c, h.DB, viewer, blocked, input)
		}
		// friends / public / following は canView && matchesFeed と同じ絞り込みを SQL 1 本で行う（#173）。
		// 閲覧者が active で今日投稿したか・ブロック・友達も副問い合わせで引くので、DB との往復は 1 回。
		// 退会した人・今日投稿していない人は 0 件になり、200 で空の配列を返す（loadViewerContext のときと同じ）
		const friendIDs = `ARRAY(SELECT friend_id FROM friend_edges WHERE user_id = $1)`
		conditions := `
   AND p.author_id <> $1 AND w.ended_at IS NULL
   AND ($2::timestamptz IS NULL OR (p.posted_at, p.id) < ($2, $3))
   AND EXISTS(
     SELECT 1 FROM users vu
     JOIN posting_windows cur ON cur.region = vu.region AND cur.ended_at IS NULL
     JOIN posts vp ON vp.author_id = vu.id AND vp.local_date = cur.local_date AND vp.deleted_at IS NULL
     WHERE vu.id = $1 AND vu.status = 'active')
   AND p.author_id <> ALL(ARRAY(
     SELECT CASE WHEN blocker_id = $1 THEN blocked_id ELSE blocker_id END
     FROM block_relations WHERE blocker_id = $1 OR blocked_id = $1))
   AND p.visibility IN ('public', 'friends', 'friends_of_friends')`
		switch input.Feed {
		case "public":
			conditions += ` AND p.visibility = 'public'`
		case "friends":
			conditions += ` AND p.author_id = ANY(` + friendIDs + `)`
		case "following":
			// 友達の友達は friend_edges の EXISTS で判定する
			conditions += ` AND p.author_id IN (SELECT followee_id FROM follows WHERE follower_id = $1)
   AND (p.visibility = 'public'
    OR (p.visibility = 'friends' AND p.author_id = ANY(` + friendIDs + `))
    OR (p.visibility = 'friends_of_friends' AND (p.author_id = ANY(` + friendIDs + `) OR EXISTS(
         SELECT 1 FROM friend_edges f WHERE f.user_id = p.author_id AND f.friend_id = ANY(` + friendIDs + `)))))`
		}
		posts, err := selectPostsWithAuthor(ctx, h.DB, conditions+`
   ORDER BY p.posted_at DESC, p.id DESC LIMIT 51`, viewerID, input.BeforePostedAt, input.BeforeID)
		if err != nil {
			return err
		}
		items := make([]feedItemResponse, 0, len(posts))
		for _, post := range posts {
			items = append(items, feedItemResponse{createPostResponse: newPostResponse(post.Post, post.Author)})
		}
		return feedResponse(c, items)
	}
}

func feedResponse(c *echo.Context, items []feedItemResponse) error {
	hasMore := len(items) > 50
	if hasMore {
		items = items[:50]
	}
	return c.JSON(http.StatusOK, getFeedResponse{Items: items, HasMore: hasMore})
}

// friends-of-friends のスコア（10*友達のエンゲージメント数 + 3*共通の友達数 + min(connections, 30)）と
// 並び（score DESC, posted_at DESC, id DESC）、カーソル、先頭 51 件を SQL 1 本で出し、投稿本体を引き直す。
// 著者は友達と辺を持つ（共通の友達がいる）友達以外のユーザー。canView から公開範囲は public / friends_of_friends。
// connections は著者の各友達 Y について Y と viewer の共通の友達数を足したもの（Y = viewer は 0）。
// 共通の友達数は mutual_friends（friendships のトリガーで同じトランザクションの中で追従する）を PK で引く
func friendsOfFriendsFeed(c *echo.Context, db sqlx.QueryerContext, viewer viewerContext, blocked []string, input getFeedRequest) error {
	ctx := c.Request().Context()
	friends := make([]string, 0, len(viewer.Friends))
	for id := range viewer.Friends {
		friends = append(friends, id)
	}
	var ranked []struct {
		ID    string `db:"id"`
		Score int64  `db:"score"`
	}
	if err := sqlx.SelectContext(ctx, db, &ranked, `
  WITH cand AS MATERIALIZED (
    SELECT p.id, p.author_id, p.posted_at, m.n AS mutual
    FROM posting_windows w
    JOIN posts p ON p.posting_window_id = w.id AND p.deleted_at IS NULL
    JOIN mutual_friends m ON m.user_id = $1 AND m.other_id = p.author_id AND m.n > 0
    JOIN users u ON u.id = p.author_id AND u.status = 'active'
    WHERE w.ended_at IS NULL
      AND p.author_id <> ALL($2::text[]) AND p.author_id <> ALL($3::text[])
      AND p.visibility IN ('public', 'friends_of_friends')
  ), conn AS (
    SELECT a.author_id, sum(m.n) AS n
    FROM (SELECT DISTINCT author_id FROM cand) a
    JOIN friend_edges f ON f.user_id = a.author_id
    JOIN mutual_friends m ON m.user_id = $1 AND m.other_id = f.friend_id
    GROUP BY a.author_id
  ), eng AS (
    SELECT e.post_id, count(*) AS n FROM (
      SELECT r.post_id, r.reactor_id AS user_id FROM realmojis r
      WHERE r.post_id IN (SELECT id FROM cand) AND r.reactor_id = ANY($2::text[])
        AND r.replaced_at IS NULL AND r.deleted_at IS NULL
      UNION
      SELECT cm.post_id, cm.author_id FROM comments cm
      WHERE cm.post_id IN (SELECT id FROM cand) AND cm.author_id = ANY($2::text[]) AND cm.deleted_at IS NULL
    ) e JOIN users u ON u.id = e.user_id AND u.status = 'active'
    GROUP BY e.post_id
  ), scored AS (
    SELECT c.id, c.posted_at,
      (10*COALESCE(e.n, 0) + 3*c.mutual + LEAST(COALESCE(cn.n, 0), 30))::bigint AS score
    FROM cand c
    LEFT JOIN eng e ON e.post_id = c.id
    LEFT JOIN conn cn ON cn.author_id = c.author_id
  )
  SELECT id, score FROM scored
  WHERE $4::bigint IS NULL OR score < $4
     OR (score = $4 AND (posted_at < $5::timestamptz OR (posted_at = $5::timestamptz AND id COLLATE "C" < $6::text)))
  ORDER BY score DESC, posted_at DESC, id COLLATE "C" DESC
  LIMIT 51`, viewer.ID, friends, blocked, input.BeforeScore, input.BeforePostedAt, input.BeforeID); err != nil {
		return err
	}
	ids := make([]string, 0, len(ranked))
	for _, r := range ranked {
		ids = append(ids, r.ID)
	}
	posts, err := selectPostsWithAuthor(ctx, db, `AND p.id = ANY($1::text[])`, ids)
	if err != nil {
		return err
	}
	byID := make(map[string]PostWithAuthor, len(posts))
	for _, post := range posts {
		byID[post.ID] = post
	}
	items := make([]feedItemResponse, 0, len(ranked))
	for _, r := range ranked {
		post, ok := byID[r.ID]
		if !ok {
			continue
		}
		score := r.Score
		items = append(items, feedItemResponse{createPostResponse: newPostResponse(post.Post, post.Author), Score: &score})
	}
	return feedResponse(c, items)
}

func matchesFeed(viewer viewerContext, following map[string]bool, post Post, feed string) bool {
	switch feed {
	case "public":
		return post.Visibility == "public"
	case "following":
		return following[post.AuthorID]
	case "friends":
		return viewer.Friends[post.AuthorID]
	case "friends-of-friends":
		return !viewer.Friends[post.AuthorID] && viewer.FriendsOfFriends[post.AuthorID]
	default:
		return false
	}
}
