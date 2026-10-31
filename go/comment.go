package isureal

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/jmoiron/sqlx"
)

type Comment struct {
	ID        string     `db:"id"`
	PostID    string     `db:"post_id"`
	AuthorID  string     `db:"author_id"`
	Body      string     `db:"body"`
	CreatedAt time.Time  `db:"created_at"`
	DeletedAt *time.Time `db:"deleted_at"`
}

type CommentWithAuthor struct {
	Comment
	Author User `db:"author"`
}

func selectCommentsWithAuthor(ctx context.Context, db sqlx.QueryerContext, conditions string, args ...any) ([]CommentWithAuthor, error) {
	var comments []CommentWithAuthor
	if err := sqlx.SelectContext(ctx, db, &comments, `
  SELECT c.id, c.post_id, c.author_id, c.body, c.created_at, c.deleted_at, `+userColumnsAs("u", "author")+`
  FROM comments c JOIN users u ON u.id = c.author_id AND u.status = 'active'
  WHERE c.deleted_at IS NULL `+conditions, args...); err != nil {
		return nil, err
	}
	for i := range comments {
		if err := comments[i].Author.setImageKey(); err != nil {
			return nil, err
		}
	}
	return comments, nil
}

func canComment(ctx context.Context, db sqlx.QueryerContext, viewerID, postID string) (bool, error) {
	row, err := getPostForViewer(ctx, db, viewerID, postID)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if !row.canView() || row.WindowEndedAt != nil {
		return false, nil
	}
	if row.IsAuthor {
		return row.HasPostedToday, nil
	}
	if row.Visibility == "public" {
		return true, nil
	}
	return row.Friends, nil
}
