package isureal

import (
	"context"
	"database/sql"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
)

type FriendRequest struct {
	ID          string    `db:"id"`
	RequesterID string    `db:"requester_id"`
	RecipientID string    `db:"recipient_id"`
	Status      string    `db:"status"`
	CreatedAt   time.Time `db:"created_at"`
	UpdatedAt   time.Time `db:"updated_at"`
}

func getFriendRequest(ctx context.Context, db sqlx.QueryerContext, id string) (FriendRequest, error) {
	var request FriendRequest
	err := sqlx.GetContext(ctx, db, &request, `SELECT id, requester_id, recipient_id, status, created_at, updated_at FROM friend_requests WHERE id=$1`, id)
	return request, err
}

// 2 人のユーザー間の操作（friend-request / follow / block / realmoji / comment）を直列化するロック。
// users の行を FOR NO KEY UPDATE で握ると、相手が有名人のとき全員がその 1 行に並ぶので、
// 「ペア」単位の advisory lock（トランザクション終了で解放）に置き換えている。
// 同じ 2 人の間の操作は今まで通り排他され、別のペアは並行して進む。
const pairLockCTE = `WITH pair_lock AS MATERIALIZED (
    SELECT pg_advisory_xact_lock(hashtextextended(least($1::text, $2::text) || '|' || greatest($1::text, $2::text), 0))
  )
  `

func beginRelationshipTx(ctx context.Context, db *sqlx.DB, actor, target string) (*sqlx.Tx, error) {
	if actor == target || target == "" {
		return nil, NewAPIError(http.StatusBadRequest, "invalid_relationship")
	}
	tx, err := db.BeginTxx(ctx, nil)
	if err != nil {
		return nil, err
	}
	var lockedIDs []string
	if err := tx.SelectContext(ctx, &lockedIDs, pairLockCTE+`SELECT u.id FROM users u, pair_lock WHERE u.id IN ($1, $2) AND u.status='active' ORDER BY u.id`, actor, target); err != nil {
		tx.Rollback()
		return nil, err
	}
	if len(lockedIDs) != 2 {
		tx.Rollback()
		return nil, NewAPIError(http.StatusNotFound, "relationship_not_found")
	}
	return tx, nil
}

func isBlocked(ctx context.Context, db sqlx.QueryerContext, actor, target string) (bool, error) {
	var blocked bool
	err := sqlx.GetContext(ctx, db, &blocked, `SELECT EXISTS(SELECT 1 FROM block_relations WHERE (blocker_id=$1 AND blocked_id=$2) OR (blocker_id=$2 AND blocked_id=$1))`, actor, target)
	return blocked, err
}

const friendRequestColumns = `id, requester_id, recipient_id, status, created_at, updated_at`

func acceptFriendRequest(ctx context.Context, tx *sqlx.Tx, request FriendRequest) (FriendRequest, error) {
	var accepted FriendRequest
	err := tx.GetContext(ctx, &accepted, `
  WITH friendship AS (
    INSERT INTO friendships (id, user_low_id, user_high_id, source_request_id) VALUES ($1,$2,$3,$4)
  )
  UPDATE friend_requests SET status='accepted', updated_at=clock_timestamp() WHERE id=$4
  RETURNING `+friendRequestColumns, uuid.NewString(), min(request.RequesterID, request.RecipientID), max(request.RequesterID, request.RecipientID), request.ID)
	return accepted, err
}

func requireRelationshipChange(result sql.Result, err error, status int, code string) error {
	if err != nil {
		return err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if count == 0 {
		return NewAPIError(status, code)
	}
	return nil
}
