package isureal

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/jmoiron/sqlx"
)

type User struct {
	ID             string  `db:"id" json:"id"`
	Username       string  `db:"username" json:"username"`
	SecretKeyHash  string  `db:"secret_key_hash" json:"-"`
	DisplayName    string  `db:"display_name" json:"displayName"`
	Bio            string  `db:"bio" json:"bio"`
	ProfileImageID *string `db:"profile_image_id" json:"-"`
	Region         string  `db:"region" json:"region"`
	AccountType    string  `db:"account_type" json:"accountType"`
	Status         string  `db:"status" json:"status"`
	ImageKey       []byte  `db:"-" json:"-"`
}

var usernamePattern = regexp.MustCompile(`^[a-z0-9_.-]{1,64}$`)

func validUserText(value string, minimum, maximum int) bool {
	length := utf8.RuneCountInString(value)
	return utf8.ValidString(value) && length >= minimum && length <= maximum
}

func validRegion(region string) bool {
	switch region {
	case "america", "europe", "asia":
		return true
	default:
		return false
	}
}

var userColumns = []string{"id", "username", "secret_key_hash", "display_name", "bio", "profile_image_id", "region", "account_type", "status"}

// 列名の前に "u." などの別名を付ける（JOIN したときに素の列名で受けるため）
func prefixColumns(prefix string, columns []string) []string {
	out := make([]string, len(columns))
	for i, column := range columns {
		out[i] = prefix + column
	}
	return out
}

func userColumnsAs(alias, prefix string) string {
	columns := make([]string, len(userColumns))
	for i, column := range userColumns {
		columns[i] = fmt.Sprintf(`%s.%s AS "%s.%s"`, alias, column, prefix, column)
	}
	return strings.Join(columns, ", ")
}

// PL/pgSQL 関数が u_<列> で返す users の列を、userColumnsAs と同じ "<prefix>.<列>" に読み替える（sql/90-interaction-func.sql）
func funcUserColumnsAs(columnPrefix, prefix string) string {
	columns := make([]string, len(userColumns))
	for i, column := range userColumns {
		columns[i] = fmt.Sprintf(`%s%s AS "%s.%s"`, columnPrefix, column, prefix, column)
	}
	return strings.Join(columns, ", ")
}

func (user *User) setImageKey() error {
	key, err := imageSigningKey(user.SecretKeyHash)
	user.ImageKey = key
	return err
}

func getUser(ctx context.Context, db sqlx.QueryerContext, userID string) (User, error) {
	var user User
	err := sqlx.GetContext(ctx, db, &user, `SELECT `+strings.Join(userColumns, ", ")+` FROM users WHERE id = $1 AND status = 'active'`, userID)
	if err != nil {
		return user, err
	}
	return user, user.setImageKey()
}
