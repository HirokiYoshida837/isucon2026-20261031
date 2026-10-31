#!/usr/bin/env bash
# DB サーバーの事実を集める（読み取りだけ）。db-migrate を作る材料にする
#   inspect.sh <ssh 名> [DB 名] [webapp のパス]
#   DB 名を省くと、DB の一覧だけ出して終わる
# 環境変数: ISU_DB_MYSQL（既定 "sudo mysql"）
set -euo pipefail

HOST="${1:?usage: inspect.sh <ssh 名> [DB 名] [webapp のパス]}"
DB="${2:-}"
WEBAPP="${3:-/home/isucon/webapp}"
MYSQL="${ISU_DB_MYSQL:-sudo mysql}"

[[ -z "$DB" || "$DB" =~ ^[A-Za-z0-9_]+$ ]] || { echo "bad DB: $DB" >&2; exit 1; }

echo "## エンジンとバージョン"
ssh "$HOST" "$MYSQL -N -e 'SELECT VERSION()'" || echo "（mysql に入れない。ISU_DB_MYSQL を変えるか、DB サーバーか確かめる）"

echo; echo "## DB 一覧"
ssh "$HOST" "$MYSQL -N -e 'SHOW DATABASES'" | grep -vE '^(information_schema|mysql|performance_schema|sys)$' || true

if [ -n "$DB" ]; then
	echo; echo "## テーブルの行数・サイズ（${DB}）"
	ssh "$HOST" "$MYSQL -t $DB" <<'SQL'
SELECT table_name, table_rows, ROUND(data_length/1024/1024) AS data_mb, ROUND(index_length/1024/1024) AS idx_mb
FROM information_schema.tables WHERE table_schema = DATABASE() ORDER BY data_length DESC;
SQL
	echo; echo "## 既存 index（${DB}）"
	ssh "$HOST" "$MYSQL -t $DB" <<'SQL'
SELECT table_name, index_name, GROUP_CONCAT(column_name ORDER BY seq_in_index) AS cols, MAX(non_unique) AS non_unique
FROM information_schema.statistics WHERE table_schema = DATABASE()
GROUP BY table_name, index_name ORDER BY table_name, index_name;
SQL
fi

echo; echo "## webapp の SQL まわり（${WEBAPP}）"
ssh "$HOST" "ls -la '$WEBAPP/sql' 2>/dev/null || echo '（sql/ が無い）'"
echo; echo "## 初期化で流れる破壊的な文（DROP / TRUNCATE / DELETE / CREATE TABLE）"
ssh "$HOST" "grep -rnE --exclude='*.gz' --exclude='*.bz2' 'DROP TABLE|TRUNCATE|DELETE FROM|CREATE TABLE' '$WEBAPP/sql' 2>/dev/null | head -40 | grep . || echo '（sql/ に無い。初期化は Go などアプリの中かもしれない。initialize ハンドラを読む）'"
