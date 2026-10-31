#!/usr/bin/env bash
# DB の index を見る・EXPLAIN する・冪等に貼る（ssh 越しに DB サーバーの mysql を叩く）
#   idx.sh show [table]                   index 一覧とテーブルの行数・サイズ
#   idx.sh explain "<SELECT ...>"         EXPLAIN（SELECT だけ受け付ける）
#   idx.sh apply <table> <name> "<cols>"  無ければ ALTER TABLE ADD INDEX。所要秒を表示（人の OK を得てから使う）
# 環境変数: ISU_DB_SSH（DB サーバーの ssh 名）、ISU_DB_NAME（DB 名）、ISU_DB_MYSQL（既定 "sudo mysql"）
set -euo pipefail

HOST="${ISU_DB_SSH:-<<DB サーバーの ssh 名>>}"
DB="${ISU_DB_NAME:-<<DB 名>>}"
MYSQL="${ISU_DB_MYSQL:-sudo mysql}"

q() { ssh "$HOST" "$MYSQL -t $DB"; }

case "${1:-}" in
show)
	where=""
	if [ -n "${2:-}" ]; then
		[[ "$2" =~ ^[A-Za-z0-9_]+$ ]] || { echo "bad table: $2" >&2; exit 1; }
		where="AND table_name = '$2'"
	fi
	q <<SQL
SELECT table_name, index_name, GROUP_CONCAT(column_name ORDER BY seq_in_index) AS cols, MAX(non_unique) AS non_unique
FROM information_schema.statistics WHERE table_schema = DATABASE() $where
GROUP BY table_name, index_name ORDER BY table_name, index_name;
SELECT table_name, table_rows, ROUND(data_length/1024/1024) AS data_mb, ROUND(index_length/1024/1024) AS idx_mb
FROM information_schema.tables WHERE table_schema = DATABASE() $where ORDER BY table_name;
SQL
	;;
explain)
	[ -n "${2:-}" ] || { echo "usage: idx.sh explain \"<SELECT ...>\"" >&2; exit 1; }
	[[ "$2" =~ ^[[:space:]]*[Ss][Ee][Ll][Ee][Cc][Tt][[:space:]] ]] || { echo "SELECT だけ受け付ける" >&2; exit 1; }
	printf 'EXPLAIN %s;\n' "$2" | q
	;;
apply)
	[ $# -eq 4 ] || { echo "usage: idx.sh apply <table> <name> \"<cols>\"" >&2; exit 1; }
	table="$2" name="$3" cols="$4"
	[[ "$table" =~ ^[A-Za-z0-9_]+$ && "$name" =~ ^[A-Za-z0-9_]+$ ]] || { echo "bad table/name" >&2; exit 1; }
	[[ "$cols" =~ ^[A-Za-z0-9_,\ \(\)]+$ ]] || { echo "bad cols: $cols" >&2; exit 1; }
	n=$(ssh "$HOST" "$MYSQL -N $DB" <<<"SELECT COUNT(*) FROM information_schema.statistics WHERE table_schema = DATABASE() AND table_name = '$table' AND index_name = '$name';")
	if [ "$n" != "0" ]; then
		echo "skip: $table.$name は既にある"
		exit 0
	fi
	start=$(date +%s)
	q <<<"ALTER TABLE $table ADD INDEX $name ($cols);"
	echo "added: $table.$name ($cols) in $(( $(date +%s) - start ))s"
	;;
*)
	sed -n '2,6p' "$0"
	exit 1
	;;
esac
