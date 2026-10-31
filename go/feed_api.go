package isureal

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"sort"
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
		viewer, err := loadViewerContext(ctx, h.DB, GetSession(c).UserID)
		if errors.Is(err, sql.ErrNoRows) {
			return c.JSON(http.StatusOK, getFeedResponse{Items: []feedItemResponse{}})
		}
		if err != nil {
			return err
		}
		following := map[string]bool{}
		if input.Feed == "following" {
			var followeeIDs []string
			if err := h.DB.SelectContext(ctx, &followeeIDs, `SELECT followee_id FROM follows WHERE follower_id = $1`, viewer.ID); err != nil {
				return err
			}
			for _, id := range followeeIDs {
				following[id] = true
			}
		}
		beforePostedAt, beforeID := input.BeforePostedAt, input.BeforeID
		if ranked {
			beforePostedAt, beforeID = nil, ""
		}
		// canView / matchesFeed と同じ絞り込みを SQL に寄せる（Go 側の判定も残す）。
		// 今日投稿していない人は自分以外の投稿を見られない
		if !viewer.HasPostedToday {
			return c.JSON(http.StatusOK, getFeedResponse{Items: []feedItemResponse{}})
		}
		blocked := make([]string, 0, len(viewer.Blocked))
		for id := range viewer.Blocked {
			blocked = append(blocked, id)
		}
		conditions := `
   AND p.author_id <> $1 AND w.ended_at IS NULL
   AND ($2::timestamptz IS NULL OR (p.posted_at, p.id) < ($2, $3))
   AND p.author_id <> ALL($4::text[])
   AND p.visibility IN ('public', 'friends', 'friends_of_friends')`
		args := []any{viewer.ID, beforePostedAt, beforeID, blocked}
		limit := ""
		authors := make([]string, 0)
		switch input.Feed {
		case "public":
			conditions += ` AND p.visibility = 'public'`
			limit = " LIMIT 51" // SQL の条件が canView && matchesFeed と一致するので先頭 51 件で足りる
		case "friends":
			for id := range viewer.Friends {
				authors = append(authors, id)
			}
			limit = " LIMIT 51"
		case "friends-of-friends":
			for id := range viewer.FriendsOfFriends {
				if !viewer.Friends[id] {
					authors = append(authors, id)
				}
			}
		case "following":
			for id := range following {
				authors = append(authors, id)
			}
		}
		if input.Feed != "public" {
			conditions += ` AND p.author_id = ANY($5::text[])`
			args = append(args, authors)
		}
		if input.Feed == "following" {
			// canView の公開範囲判定を SQL に寄せる（フォロー先の見えない投稿を読まない）。
			// ranked ではなく SQL の条件が canView && matchesFeed と一致するので、先頭 51 件で足りる
			friends := make([]string, 0, len(viewer.Friends))
			friendsOrFoF := make([]string, 0, len(viewer.Friends)+len(viewer.FriendsOfFriends))
			for id := range viewer.Friends {
				friends = append(friends, id)
				friendsOrFoF = append(friendsOrFoF, id)
			}
			for id := range viewer.FriendsOfFriends {
				friendsOrFoF = append(friendsOrFoF, id)
			}
			conditions += ` AND (p.visibility = 'public'
    OR (p.visibility = 'friends' AND p.author_id = ANY($6::text[]))
    OR (p.visibility = 'friends_of_friends' AND p.author_id = ANY($7::text[])))`
			args = append(args, friends, friendsOrFoF)
			limit = " LIMIT 51"
		}
		posts, err := selectPostsWithAuthor(ctx, h.DB, conditions+`
   ORDER BY p.posted_at DESC, p.id DESC`+limit, args...)
		if err != nil {
			return err
		}
		candidates := make([]PostWithAuthor, 0)
		for _, post := range posts {
			if viewer.canView(post.Post) && matchesFeed(viewer, following, post.Post, input.Feed) {
				candidates = append(candidates, post)
			}
		}
		var scores map[string]int64
		if ranked {
			if scores, err = recommendationScores(ctx, h.DB, viewer, candidates); err != nil {
				return err
			}
			sort.SliceStable(candidates, func(i, j int) bool {
				return scores[candidates[i].ID] > scores[candidates[j].ID]
			})
		}
		items := make([]feedItemResponse, 0)
		for _, post := range candidates {
			score := scores[post.ID]
			if ranked && input.BeforeScore != nil && !rankedBefore(score, post.Post, *input.BeforeScore, *input.BeforePostedAt, input.BeforeID) {
				continue
			}
			item := feedItemResponse{createPostResponse: newPostResponse(post.Post, post.Author)}
			if ranked {
				item.Score = &score
			}
			items = append(items, item)
			if len(items) == 51 {
				break
			}
		}
		hasMore := len(items) > 50
		if hasMore {
			items = items[:50]
		}
		return c.JSON(http.StatusOK, getFeedResponse{Items: items, HasMore: hasMore})
	}
}

func rankedBefore(score int64, post Post, beforeScore int64, beforePostedAt time.Time, beforeID string) bool {
	if score != beforeScore {
		return score < beforeScore
	}
	if !post.PostedAt.Equal(beforePostedAt) {
		return post.PostedAt.Before(beforePostedAt)
	}
	return post.ID < beforeID
}

func recommendationScores(ctx context.Context, db sqlx.QueryerContext, viewer viewerContext, posts []PostWithAuthor) (map[string]int64, error) {
	scores := make(map[string]int64, len(posts))
	if len(posts) == 0 {
		return scores, nil
	}
	authorIDs := make([]string, 0, len(posts))
	postIDs := make([]string, 0, len(posts))
	for _, post := range posts {
		authorIDs = append(authorIDs, post.AuthorID)
		postIDs = append(postIDs, post.ID)
	}

	query, args, err := sqlx.In(`SELECT user_low_id, user_high_id FROM friendships WHERE user_low_id IN (?) OR user_high_id IN (?)`, authorIDs, authorIDs)
	if err != nil {
		return nil, err
	}
	var friendships []struct {
		UserLowID  string `db:"user_low_id"`
		UserHighID string `db:"user_high_id"`
	}
	if err := sqlx.SelectContext(ctx, db, &friendships, sqlx.Rebind(sqlx.DOLLAR, query), args...); err != nil {
		return nil, err
	}
	connections := map[string]int64{}
	for _, friendship := range friendships {
		connections[friendship.UserLowID] += int64(viewer.MutualFriends[friendship.UserHighID])
		connections[friendship.UserHighID] += int64(viewer.MutualFriends[friendship.UserLowID])
	}

	query, args, err = sqlx.In(`
  SELECT r.post_id, r.reactor_id AS user_id FROM realmojis r
  JOIN users u ON u.id = r.reactor_id AND u.status = 'active'
  WHERE r.post_id IN (?) AND r.replaced_at IS NULL AND r.deleted_at IS NULL
  UNION
  SELECT c.post_id, c.author_id AS user_id FROM comments c
  JOIN users u ON u.id = c.author_id AND u.status = 'active'
  WHERE c.post_id IN (?) AND c.deleted_at IS NULL
 `, postIDs, postIDs)
	if err != nil {
		return nil, err
	}
	var engagements []struct {
		PostID string `db:"post_id"`
		UserID string `db:"user_id"`
	}
	if err := sqlx.SelectContext(ctx, db, &engagements, sqlx.Rebind(sqlx.DOLLAR, query), args...); err != nil {
		return nil, err
	}
	friendEngagements := map[string]int64{}
	for _, engagement := range engagements {
		if viewer.Friends[engagement.UserID] {
			friendEngagements[engagement.PostID]++
		}
	}

	for _, post := range posts {
		scores[post.ID] = 10*friendEngagements[post.ID] + 3*int64(viewer.MutualFriends[post.AuthorID]) + min(connections[post.AuthorID], 30)
	}
	return scores, nil
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
