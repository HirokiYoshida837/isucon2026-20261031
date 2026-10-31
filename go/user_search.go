package isureal

import (
	"context"

	"github.com/jmoiron/sqlx"
)

// fillSearchDisplay は users.search_display が NULL の行（列の追加直後・reset.sql で display_name を戻した行）を
// normalizeSearchText(display_name) で埋める。登録と PATCH /api/me は同じ文で書くので、ここで埋めるのは初期化の分だけ（#122）
func fillSearchDisplay(ctx context.Context, db *sqlx.DB) error {
	var rows []struct {
		ID          string `db:"id"`
		DisplayName string `db:"display_name"`
	}
	if err := db.SelectContext(ctx, &rows, `SELECT id, display_name FROM users WHERE search_display IS NULL`); err != nil {
		return err
	}
	if len(rows) == 0 {
		return nil
	}
	ids := make([]string, len(rows))
	values := make([]string, len(rows))
	for i, row := range rows {
		ids[i] = row.ID
		values[i] = normalizeSearchText(row.DisplayName)
	}
	_, err := db.ExecContext(ctx, `
  UPDATE users u SET search_display = v.search_display
  FROM unnest($1::text[], $2::text[]) AS v(id, search_display)
  WHERE u.id = v.id
 `, ids, values)
	return err
}
