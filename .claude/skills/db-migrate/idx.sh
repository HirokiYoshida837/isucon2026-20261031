#!/usr/bin/env bash
# DB の index を見る・EXPLAIN する・冪等に貼る（ssh 越しに DB サーバーの psql を叩く）
#   idx.sh show [table]                               index 一覧とテーブルの行数・サイズ
#   idx.sh explain "<SELECT ...>"                     EXPLAIN（実行はしない。SELECT / WITH だけ受け付ける）
#   idx.sh apply <table> <name> "<cols>" ["<where>"]  無ければ CREATE INDEX。所要秒を表示（人の OK を得てから使う）
# 環境変数: ISU_DB_SSH（DB サーバーの ssh 名）、ISU_DB_NAME（DB 名）、ISU_DB_PSQL（既定 "sudo -u postgres psql"）
set -euo pipefail

HOST="${ISU_DB_SSH:-asia2}"
DB="${ISU_DB_NAME:-isureal}"
PSQL="${ISU_DB_PSQL:-sudo -u postgres psql}"

q() { ssh "$HOST" "cd /tmp && $PSQL -X -v ON_ERROR_STOP=1 -d $DB"; }
v() { ssh "$HOST" "cd /tmp && $PSQL -X -v ON_ERROR_STOP=1 -Atq -d $DB"; }

case "${1:-}" in
show)
	where=""
	if [ -n "${2:-}" ]; then
		[[ "$2" =~ ^[a-z0-9_]+$ ]] || { echo "bad table: $2" >&2; exit 1; }
		where="AND t.relname = '$2'"
	fi
	q <<SQL
WITH t AS (
  SELECT t.oid, t.relname FROM pg_class t
  WHERE t.relnamespace = 'public'::regnamespace AND t.relkind IN ('r', 'p') AND NOT t.relispartition $where
), leaf AS (
  SELECT t.oid AS root, t.relname, COALESCE(x.relid, t.oid) AS leaf
  FROM t LEFT JOIN LATERAL pg_partition_tree(t.oid) x ON x.isleaf
)
SELECT l.relname AS table_name, sum(greatest(c.reltuples, 0))::bigint AS est_rows,
       pg_size_pretty(sum(pg_table_size(l.leaf))) AS table_size, pg_size_pretty(sum(pg_indexes_size(l.leaf))) AS index_size
FROM leaf l JOIN pg_class c ON c.oid = l.leaf
GROUP BY l.relname ORDER BY sum(pg_table_size(l.leaf)) DESC;
SELECT t.relname AS table_name, c.relname AS index_name, i.indisvalid AS valid,
       pg_size_pretty(COALESCE((SELECT sum(pg_relation_size(x.relid)) FROM pg_partition_tree(c.oid) x), pg_relation_size(c.oid))) AS size,
       regexp_replace(pg_get_indexdef(c.oid), '^.* USING ', '') AS def
FROM pg_index i JOIN pg_class c ON c.oid = i.indexrelid JOIN pg_class t ON t.oid = i.indrelid
WHERE t.relnamespace = 'public'::regnamespace AND NOT t.relispartition $where
ORDER BY t.relname, c.relname;
SQL
	;;
explain)
	[ -n "${2:-}" ] || { echo "usage: idx.sh explain \"<SELECT ...>\"" >&2; exit 1; }
	[[ "$2" =~ ^[[:space:]]*([Ss][Ee][Ll][Ee][Cc][Tt]|[Ww][Ii][Tt][Hh])[[:space:]] ]] || { echo "SELECT / WITH だけ受け付ける" >&2; exit 1; }
	[[ "$2" != *";"* && "$2" != *"\\"* ]] || { echo "; と \\ は受け付けない（1 文だけ渡す）" >&2; exit 1; }
	printf 'EXPLAIN %s;\n' "$2" | q
	;;
apply)
	[ $# -eq 4 ] || [ $# -eq 5 ] || { echo "usage: idx.sh apply <table> <name> \"<cols>\" [\"<where>\"]" >&2; exit 1; }
	table="$2" name="$3" cols="$4" where="${5:-}"
	[[ "$table" =~ ^[a-z0-9_]+$ && "$name" =~ ^[a-z0-9_]+$ ]] || { echo "bad table/name（小文字・数字・_ だけ）" >&2; exit 1; }
	[[ "$cols" =~ ^[A-Za-z0-9_,\ \(\)]+$ ]] || { echo "bad cols: $cols" >&2; exit 1; }
	where_re="^[A-Za-z0-9_,\ \(\)=<>'!.:-]*$"
	[[ "$where" =~ $where_re ]] || { echo "bad where: $where" >&2; exit 1; }
	state=$(v <<<"SELECT t.relname || ' ' || i.indisvalid FROM pg_class c JOIN pg_index i ON i.indexrelid = c.oid JOIN pg_class t ON t.oid = i.indrelid WHERE c.relnamespace = 'public'::regnamespace AND c.relname = '$name';")
	case "$state" in
	"$table true") echo "skip: $table.$name は既にある"; exit 0 ;;
	"$table false") echo "stop: $table.$name は INVALID（前回の失敗の残り）。消して貼り直すなら人の OK を待つ" >&2; exit 1 ;;
	"") ;;
	*) echo "stop: 同じ名前の index が別のテーブルにある: $name ($state)" >&2; exit 1 ;;
	esac
	kind=$(v <<<"SELECT relkind FROM pg_class WHERE relnamespace = 'public'::regnamespace AND relname = '$table';")
	case "$kind" in
	r) create="CREATE INDEX CONCURRENTLY" ;;
	p) create="CREATE INDEX"; echo "warn: パーティションの親は CONCURRENTLY を使えない。貼り終わるまで $table の書き込みが止まる" >&2 ;;
	*) echo "bad table: $table" >&2; exit 1 ;;
	esac
	start=$(date +%s)
	q <<<"$create $name ON $table ($cols)${where:+ WHERE $where};"
	echo "added: $table.$name ($cols)${where:+ WHERE $where} in $(( $(date +%s) - start ))s"
	;;
*)
	sed -n '2,6p' "$0"
	exit 1
	;;
esac
