package isureal

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"

	"github.com/jmoiron/sqlx"
)

type PostLocation struct {
	Kind      string   `json:"kind" db:"location_kind"`
	Latitude  *float64 `json:"latitude,omitempty" db:"latitude"`
	Longitude *float64 `json:"longitude,omitempty" db:"longitude"`
	Locality  *string  `json:"locality,omitempty" db:"locality"`
}

func (location PostLocation) valid() bool {
	switch location.Kind {
	case "none":
		return location.Latitude == nil && location.Longitude == nil && location.Locality == nil
	case "exact":
		return location.Latitude != nil && location.Longitude != nil && location.Locality == nil &&
			*location.Latitude >= -90 && *location.Latitude <= 90 && *location.Longitude >= -180 && *location.Longitude <= 180
	case "approximate":
		return location.Latitude == nil && location.Longitude == nil && location.Locality != nil && validUserText(*location.Locality, 1, 100)
	default:
		return false
	}
}

type Post struct {
	ID              string `db:"id"`
	AuthorID        string `db:"author_id"`
	PostingWindowID string `db:"posting_window_id"`
	ImageID         string `db:"image_id"`
	Caption         string `db:"caption"`
	Visibility      string `db:"visibility"`
	PostLocation
	PostedAt       time.Time  `db:"posted_at"`
	OnTimeDeadline time.Time  `db:"on_time_deadline"`
	WindowEndedAt  *time.Time `db:"window_ended_at"`
}

const postColumns = `p.id, p.author_id, p.posting_window_id, p.image_id, p.caption, p.visibility,
  p.location_kind, p.latitude, p.longitude, p.locality, p.posted_at,
  w.on_time_deadline, w.ended_at AS window_ended_at`

func getPost(ctx context.Context, db sqlx.QueryerContext, postID string) (Post, error) {
	var post Post
	err := sqlx.GetContext(ctx, db, &post, `
  SELECT `+postColumns+`
  FROM posts p JOIN posting_windows w ON w.id = p.posting_window_id
  WHERE p.id = $1 AND p.deleted_at IS NULL
 `, postID)
	return post, err
}

type PostWithAuthor struct {
	Post
	Author User `db:"author"`
}

func selectPostsWithAuthor(ctx context.Context, db sqlx.QueryerContext, conditions string, args ...any) ([]PostWithAuthor, error) {
	var posts []PostWithAuthor
	if err := sqlx.SelectContext(ctx, db, &posts, `
  SELECT `+postColumns+`, `+userColumnsAs("u", "author")+`
  FROM posts p
  JOIN posting_windows w ON w.id = p.posting_window_id
  JOIN users u ON u.id = p.author_id AND u.status = 'active'
  WHERE p.deleted_at IS NULL `+conditions, args...); err != nil {
		return nil, err
	}
	reqs := make([]sigReq, 0, len(posts)*2)
	for i := range posts {
		if err := posts[i].Author.setImageKey(); err != nil {
			return nil, err
		}
		reqs = append(reqs, imageSigReqs(posts[i].ImageID, posts[i].Author)...)
	}
	prewarmSigs(reqs) // 画像の署名を 16 個ずつまとめて作る（あとの newPostResponse はキャッシュに当たる）
	return posts, nil
}

func getPostWithAuthor(ctx context.Context, db sqlx.QueryerContext, postID string) (PostWithAuthor, error) {
	posts, err := selectPostsWithAuthor(ctx, db, `AND p.id = $1`, postID)
	if err != nil {
		return PostWithAuthor{}, err
	}
	if len(posts) == 0 {
		return PostWithAuthor{}, sql.ErrNoRows
	}
	return posts[0], nil
}

type viewerContext struct {
	User
	HasPostedToday   bool
	Friends          map[string]bool
	FriendsOfFriends map[string]bool
	MutualFriends    map[string]int
	Blocked          map[string]bool
}

func splitIDs(ids string) []string {
	if ids == "" {
		return nil
	}
	return strings.Split(ids, ",")
}

func loadViewerContext(ctx context.Context, db sqlx.QueryerContext, viewerID string, withEdges bool) (viewerContext, error) {
	viewer := viewerContext{Friends: map[string]bool{}, FriendsOfFriends: map[string]bool{}, MutualFriends: map[string]int{}, Blocked: map[string]bool{}}
	var row struct {
		User
		HasPostedToday bool   `db:"has_posted_today"`
		BlockedIDs     string `db:"blocked_ids"`
		FriendIDs      string `db:"friend_ids"`
		FriendEdges    string `db:"friend_edges"`
	}
	if err := sqlx.GetContext(ctx, db, &row, `
  WITH friend_ids AS MATERIALIZED (
    SELECT friend_id AS id FROM friend_edges WHERE user_id = $1
  )
  SELECT `+strings.Join(userColumns, ", ")+`,
    EXISTS(
      SELECT 1 FROM posts p
      JOIN posting_windows current ON current.region = u.region AND current.ended_at IS NULL
      WHERE p.author_id = u.id AND p.local_date = current.local_date AND p.deleted_at IS NULL
    ) AS has_posted_today,
    COALESCE((
      SELECT string_agg(CASE WHEN blocker_id = $1 THEN blocked_id ELSE blocker_id END, ',')
      FROM block_relations WHERE blocker_id = $1 OR blocked_id = $1
    ), '') AS blocked_ids,
    COALESCE((SELECT string_agg(id, ',') FROM friend_ids), '') AS friend_ids,
    CASE WHEN $2::bool THEN COALESCE((
      SELECT string_agg(friend_id, ',') FROM friend_edges
      WHERE user_id = ANY(ARRAY(SELECT id FROM friend_ids))
    ), '') ELSE '' END AS friend_edges
  FROM users u WHERE u.id = $1 AND u.status = 'active'
 `, viewerID, withEdges); err != nil {
		return viewer, err
	}
	viewer.User = row.User
	if err := viewer.User.setImageKey(); err != nil {
		return viewer, err
	}
	viewer.HasPostedToday = row.HasPostedToday
	for _, id := range splitIDs(row.BlockedIDs) {
		viewer.Blocked[id] = true
	}
	for _, id := range splitIDs(row.FriendIDs) {
		viewer.Friends[id] = true
	}
	// friend_edges は両方向の行なので、友達 f の友達 x を 1 行ずつ数える（f-x の辺 1 本につき x 側に 1 回）
	for _, id := range splitIDs(row.FriendEdges) {
		viewer.FriendsOfFriends[id] = true
		if id != viewer.ID {
			viewer.MutualFriends[id]++
		}
	}
	return viewer, nil
}

func (viewer viewerContext) canView(post Post) bool {
	if post.AuthorID == viewer.ID {
		return true
	}
	if post.WindowEndedAt != nil || !viewer.HasPostedToday || viewer.Blocked[post.AuthorID] {
		return false
	}
	switch post.Visibility {
	case "public":
		return true
	case "friends":
		return viewer.Friends[post.AuthorID]
	case "friends_of_friends":
		return viewer.Friends[post.AuthorID] || viewer.FriendsOfFriends[post.AuthorID]
	default:
		return false
	}
}

// postAccess は投稿 1 件の閲覧判定に必要な、閲覧者と投稿者の関係だけ。
// loadViewerContext は友達の全辺まで組み立てるので、1 投稿の判定には重い（#76）
type postAccess struct {
	ViewerActive   bool `db:"viewer_active"`
	IsAuthor       bool `db:"is_author"`
	HasPostedToday bool `db:"has_posted_today"`
	Blocked        bool `db:"blocked"`
	Friends        bool `db:"friends"`
	FriendOfFriend bool `db:"friend_of_friend"`
}

type postForViewer struct {
	PostWithAuthor
	postAccess
}

// getPostForViewer は投稿・投稿者・閲覧者との関係を 1 クエリで取る。投稿が無い（削除済み・投稿者が退会）なら sql.ErrNoRows
func getPostForViewer(ctx context.Context, db sqlx.QueryerContext, viewerID, postID string) (postForViewer, error) {
	var row postForViewer
	err := sqlx.GetContext(ctx, db, &row, `
  SELECT `+postColumns+`, `+userColumnsAs("u", "author")+`,
    v.id IS NOT NULL AS viewer_active,
    p.author_id = $2 AS is_author,
    COALESCE(v.has_posted_today, false) AS has_posted_today,
    EXISTS(
      SELECT 1 FROM block_relations b
      WHERE (b.blocker_id = $2 AND b.blocked_id = p.author_id) OR (b.blocker_id = p.author_id AND b.blocked_id = $2)
    ) AS blocked,
    EXISTS(
      SELECT 1 FROM friend_edges f WHERE f.user_id = $2 AND f.friend_id = p.author_id
    ) AS friends,
    EXISTS(
      SELECT 1 FROM friend_edges a
      JOIN friend_edges b ON b.user_id = a.friend_id AND b.friend_id = p.author_id
      WHERE a.user_id = $2
    ) AS friend_of_friend
  FROM posts p
  JOIN posting_windows w ON w.id = p.posting_window_id
  JOIN users u ON u.id = p.author_id AND u.status = 'active'
  LEFT JOIN LATERAL (
    SELECT v.id, EXISTS(
      SELECT 1 FROM posts vp
      JOIN posting_windows current ON current.region = v.region AND current.ended_at IS NULL
      WHERE vp.author_id = v.id AND vp.local_date = current.local_date AND vp.deleted_at IS NULL
    ) AS has_posted_today
    FROM users v WHERE v.id = $2 AND v.status = 'active'
  ) v ON true
  WHERE p.id = $1 AND p.deleted_at IS NULL
 `, postID, viewerID)
	if err != nil {
		return row, err
	}
	return row, row.Author.setImageKey()
}

// canView は viewerContext.canView と同じ判定
func (row postForViewer) canView() bool {
	if !row.ViewerActive {
		return false
	}
	if row.IsAuthor {
		return true
	}
	if row.WindowEndedAt != nil || !row.HasPostedToday || row.Blocked {
		return false
	}
	switch row.Visibility {
	case "public":
		return true
	case "friends":
		return row.Friends
	case "friends_of_friends":
		return row.Friends || row.FriendOfFriend
	default:
		return false
	}
}

func canViewPost(ctx context.Context, db sqlx.QueryerContext, viewerID, postID string) (bool, error) {
	row, err := getPostForViewer(ctx, db, viewerID, postID)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return row.canView(), nil
}

func getFriendIDs(ctx context.Context, db sqlx.QueryerContext, userID string) ([]string, error) {
	var friendships []struct {
		UserLowID  string `db:"user_low_id"`
		UserHighID string `db:"user_high_id"`
	}
	if err := sqlx.SelectContext(ctx, db, &friendships, `SELECT user_low_id, user_high_id FROM friendships WHERE user_low_id = $1 OR user_high_id = $1`, userID); err != nil {
		return nil, err
	}
	var ids []string
	for _, friendship := range friendships {
		if friendship.UserLowID == userID {
			ids = append(ids, friendship.UserHighID)
		} else {
			ids = append(ids, friendship.UserLowID)
		}
	}
	return ids, nil
}

func getBlockedUserIDs(ctx context.Context, db sqlx.QueryerContext, userID string) (map[string]bool, error) {
	var ids []string
	if err := sqlx.SelectContext(ctx, db, &ids, `
  SELECT CASE WHEN blocker_id = $1 THEN blocked_id ELSE blocker_id END
  FROM block_relations WHERE blocker_id = $1 OR blocked_id = $1
 `, userID); err != nil {
		return nil, err
	}
	blocked := make(map[string]bool, len(ids))
	for _, id := range ids {
		blocked[id] = true
	}
	return blocked, nil
}
