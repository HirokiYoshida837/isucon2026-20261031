//go:build ignore

// initialize ハンドラに追加処理を足す例（isucon14 の postInitialize がベース）。
// 元: https://github.com/isucon/isucon14/blob/53f8b627e040c30ebec600457c6c97da008b84b0/webapp/go/main.go#L123-L142
// 使い方: 必要なブロックだけ main.go の postInitialize に写す。
// 方針: SQL で書けるものは init.sh 側に寄せ、ここには Go でしかできないことを書く。
package main

import (
	"errors"
	"fmt"
	"log"
	"net/http"
	"os/exec"
	"time"

	"github.com/go-sql-driver/mysql"
)

// 何度流しても良い DDL。既にあるときのエラーは無視する（init.sh に書けない事情があるとき用）。
var idempotentDDL = []string{
	"ALTER TABLE rides ADD INDEX idx_chair_id_updated_at (chair_id, updated_at)",
}

// isAlreadyExists は「既にある」系の MySQL エラーかを返す。
// 1060: Duplicate column name / 1061: Duplicate key name
func isAlreadyExists(err error) bool {
	var me *mysql.MySQLError
	return errors.As(err, &me) && (me.Number == 1060 || me.Number == 1061)
}

func postInitialize(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	start := time.Now()
	req := &postInitializeRequest{}
	if err := bindJSON(r, req); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}

	// 1. スキーマと初期データ（追加 SQL も init.sh の中で流す）
	if out, err := exec.Command("../sql/init.sh").CombinedOutput(); err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Errorf("failed to initialize: %s: %w", string(out), err))
		return
	}

	// 2. 冪等な DDL
	for _, q := range idempotentDDL {
		if _, err := db.ExecContext(ctx, q); err != nil && !isAlreadyExists(err) {
			writeError(w, http.StatusInternalServerError, fmt.Errorf("ddl %q: %w", q, err))
			return
		}
	}

	// 3. オンメモリキャッシュを捨てる（DB を戻したのにキャッシュが古いと FAIL する）
	// chairCache.Purge()

	// 4. 元の処理（回ごとに違う）
	if _, err := db.ExecContext(ctx, "UPDATE settings SET value = ? WHERE name = 'payment_gateway_url'", req.PaymentServer); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}

	// 所要時間をログに残す（タイムアウトの目安と比べる）
	log.Printf("initialize done in %s", time.Since(start))
	writeJSON(w, http.StatusOK, postInitializeResponse{Language: "go"})
}
