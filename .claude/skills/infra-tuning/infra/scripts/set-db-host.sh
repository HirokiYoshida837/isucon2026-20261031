#!/usr/bin/env bash
# アプリの DB 接続先を別サーバーに切り替える（アプリの台で実行）。env ファイルの 1 変数を書き換えてアプリを再起動する。
#   ./set-db-host.sh <env ファイル> <変数名> <値> [アプリのユニット名]
#   例: ./set-db-host.sh /home/isucon/env.sh ISUCONP_DB_HOST 192.168.0.13 isu-go
#
# - env ファイルはバックアップしてから書き換える（./rollback.sh で戻せる）
# - 変数が無ければ末尾に追記する
# - 再起動後に疎通確認（HTTP 200 かつ body あり）をする。NG なら Error 1130 / 接続拒否をアプリのログで見る
set -euo pipefail
# shellcheck source=lib.sh
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"

env_file="${1:?env ファイルを指定}"; var="${2:?変数名を指定}"; val="${3:?値を指定}"
app_unit="${4:-${ISU_APP_UNIT:-}}"
as_root test -f "$env_file" || die "env ファイルがありません: $env_file"
require_sudo
ensure_snapshot
start_backup dbhost
backup_file "$env_file"

if as_root grep -qE "^(export[[:space:]]+)?$var=" "$env_file"; then
  as_root sed -i -E "s|^((export[[:space:]]+)?$var=).*|\1$val|" "$env_file"
  log "変更: $env_file の $var=$val"
else
  echo "$var=$val" | as_root tee -a "$env_file" >/dev/null
  log "追記: $env_file に $var=$val"
fi
as_root grep -nE "^(export[[:space:]]+)?$var=" "$env_file"

# 先に DB へ届くか見る（3306 想定。別ポートなら DB_PORT を確認）
port="${ISU_DB_PORT:-3306}"
if timeout 3 bash -c "exec 3<>/dev/tcp/$val/$port" 2>/dev/null; then
  log "TCP 疎通 OK: $val:$port"
else
  warn "TCP 疎通 NG: $val:${port}（DB 側の bind-address / セキュリティグループ / IP を確認）"
fi

if [[ -n $app_unit ]] && has_unit "${app_unit%.service}"; then
  as_root systemctl restart "${app_unit%.service}"; sleep 2
  log "アプリ再起動: ${app_unit}（active=$(systemctl is-active "${app_unit%.service}")）"
else
  warn "アプリは再起動していない（ユニット名を 4 番目の引数か ISU_APP_UNIT で渡す）"
fi
health_check || { warn "戻すなら ./rollback.sh -y $BACKUP_DIR のあとアプリ再起動"; exit 1; }
log "完了。バックアップ: $BACKUP_DIR"
