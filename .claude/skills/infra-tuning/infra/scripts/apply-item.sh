#!/usr/bin/env bash
# カタログ（catalog.md）の項目を 1 つ当てる。差分表示 → バックアップ → 反映 → 検査 → 疎通確認。
#   ./apply-item.sh nginx-main  <directive> <value>   # /etc/nginx/nginx.conf の main / events 文脈の既存行を数値変更（無ければ追記）→ nginx -t → reload
#   ./apply-item.sh nginx-http  <directive> <value>   # http 文脈。nginx.conf に既存行があれば値変更、無ければ conf.d/99-isucon-tuning.conf に追記
#   ./apply-item.sh nginx-measure on|off              # alp 用 JSON ログ（log_format json + 別ファイルへの access_log）を conf.d に追記 / 止める
#   ./apply-item.sh mysql       <key> <value>         # conf.d/99-isucon.cnf に key = value を追記（既にあれば値変更）→ validate → mysqld restart → アプリ再起動
#   ./apply-item.sh mysql       <key>                 # 値なしの項目（disable_log_bin 等）
#   ./apply-item.sh mysql-main  <key> <value>         # 標準の mysqld.cnf / 50-server.cnf の既存行を値変更（bind-address 等）→ restart
#   ./apply-item.sh mysql-measure on|off              # スロークエリログ（long_query_time=0）
#   ./apply-item.sh sysctl      <key> <value>         # /etc/sysctl.d/99-isucon.conf に追記 → 即時反映
#   ./apply-item.sh limits      <nofile>              # /etc/security/limits.d/99-isucon-limits.conf（全ユーザーの nofile）
#   ./apply-item.sh limits-unit <unit> <nofile>       # systemd drop-in LimitNOFILE → daemon-reload → restart
#   オプション: --dry-run（差分だけ見る） --no-restart（mysql / limits-unit で再起動しない。まとめて最後に再起動したいとき）
#   環境変数: ISU_APP_UNIT（mysql のあとに再起動するアプリ）、ISU_CHECK_URL（疎通確認先）
#
# 複数の mysql 項目を入れるときは --no-restart で足していき、最後の 1 つで再起動すると速い。
# 戻し方: ./rollback.sh -y <このとき表示されたバックアップ名>。全部戻すなら ./restore-all.sh
set -euo pipefail
# shellcheck source=lib.sh
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"

dry=0; no_restart=0; args=()
for a in "$@"; do
  case "$a" in
    --dry-run) dry=1 ;;
    --no-restart) no_restart=1 ;;
    *) args+=("$a") ;;
  esac
done
target="${args[0]:-}"; key="${args[1]:-}"; value="${args[2]:-}"
[[ -n $target ]] || { sed -n '2,17p' "$0"; exit 1; }
app_unit="${ISU_APP_UNIT:-}"
require_sudo
[[ $dry -eq 1 ]] || ensure_snapshot

# 追記ファイルの「key = value」行を更新 / 追記する（cnf / sysctl 用）。$4 は区切り（" = " か " = "）
upsert_kv() {
  local file="$1" k="$2" v="$3" header="$4"
  local tmp; tmp="$(mktemp)"
  if as_root test -f "$file"; then as_root cat "$file" > "$tmp"; else printf '%s\n' "$header" > "$tmp"; fi
  if grep -qE "^\s*$k(\s*=|\s*$)" "$tmp"; then
    if [[ -n $v ]]; then sed -i -E "s|^\s*$k(\s*=.*|\s*)$|$k = $v|" "$tmp"; else sed -i -E "s|^\s*$k(\s*=.*|\s*)$|$k|" "$tmp"; fi
  else
    if [[ -n $v ]]; then echo "$k = $v" >> "$tmp"; else echo "$k" >> "$tmp"; fi
  fi
  echo "$tmp"
}

show_diff() { # $1=現在のファイル $2=新しい内容のファイル
  log "差分（- が前、+ が後）: $1"
  if as_root test -f "$1"; then as_root diff -u "$1" "$2" || true; else sed 's/^/+ /' "$2"; fi
}

restart_app() {
  if [[ -n $app_unit ]] && has_unit "${app_unit%.service}"; then
    as_root systemctl restart "${app_unit%.service}"; sleep 2
    log "アプリ再起動: ${app_unit}（active=$(systemctl is-active "${app_unit%.service}")）"
  else
    warn "アプリは再起動していない（ISU_APP_UNIT を設定すると自動で再起動する）"
  fi
}

nginx_finish() {
  log "nginx -t"
  if ! as_root nginx -t; then
    warn "nginx -t に失敗。元に戻します"; "$INFRA_DIR/scripts/rollback.sh" -y "$BACKUP_DIR"; exit 1
  fi
  as_root systemctl reload nginx; sleep 1
  health_check || true
}

mysql_finish() {
  local unit flavor; unit="$(detect_db_unit)"; flavor="$(detect_db_flavor)"
  if [[ $flavor == mysql ]]; then
    log "mysqld --validate-config"
    if ! as_root mysqld --validate-config; then
      warn "設定検査に失敗。元に戻します"; "$INFRA_DIR/scripts/rollback.sh" -y "$BACKUP_DIR"; exit 1
    fi
  fi
  if [[ $no_restart -eq 1 ]]; then warn "--no-restart: mysqld は再起動していない。最後に sudo systemctl restart $unit とアプリ再起動"; return; fi
  log "systemctl restart ${unit}（数秒〜数十秒）"
  if ! as_root systemctl restart "$unit"; then
    warn "起動に失敗。元に戻して再起動します（journalctl -u $unit -n 50）"; "$INFRA_DIR/scripts/rollback.sh" -y "$BACKUP_DIR"; exit 1
  fi
  for _ in $(seq 1 30); do as_root mysqladmin -uroot ping >/dev/null 2>&1 && break; sleep 1; done
  restart_app
  if ! health_check; then warn "DB 変更の直後に疎通 NG。Error 1130 なら mysql.user の host、接続拒否なら bind-address"; exit 1; fi
}

case "$target" in
  nginx-main)
    [[ -n $key && -n $value ]] || die "使い方: apply-item.sh nginx-main <directive> <value>"
    main=/etc/nginx/nginx.conf; tmp="$(mktemp)"; as_root cat $main > "$tmp"
    if grep -qE "^\s*$key\s" "$tmp"; then
      sed -i -E "s|^(\s*)$key\s.*|\1$key $value;|" "$tmp"
    elif [[ $key == worker_connections || $key == multi_accept || $key == use ]]; then
      sed -i -E "s|^(\s*)events\s*\{|&\n\1    $key $value;|" "$tmp"
    else
      sed -i -E "0,/^\s*worker_processes\s/s|^(\s*worker_processes\s.*)$|\1\n$key $value;|" "$tmp"
    fi
    show_diff $main "$tmp"; [[ $dry -eq 1 ]] && exit 0
    start_backup nginx; install_file "$tmp" $main; nginx_finish ;;

  nginx-http)
    [[ -n $key && -n $value ]] || die "使い方: apply-item.sh nginx-http <directive> <value>"
    main=/etc/nginx/nginx.conf
    if as_root grep -qE "^\s*$key\s" $main; then
      tmp="$(mktemp)"; as_root cat $main > "$tmp"; sed -i -E "s|^(\s*)$key\s.*|\1$key $value;|" "$tmp"
      show_diff $main "$tmp"; [[ $dry -eq 1 ]] && exit 0
      start_backup nginx; install_file "$tmp" $main
    else
      as_root grep -qE '^\s*include\s+/etc/nginx/conf\.d/\*\.conf;' $main || die "nginx.conf に conf.d の include が無い。http { } に include /etc/nginx/conf.d/*.conf; を足してから"
      tmp="$(mktemp)"
      if as_root test -f "$NGINX_TUNING_CONF"; then as_root cat "$NGINX_TUNING_CONF" > "$tmp"; else echo "# 秘伝のタレ: http 文脈のチューニング（apply-item.sh nginx-http が追記）" > "$tmp"; fi
      if grep -qE "^\s*$key\s" "$tmp"; then sed -i -E "s|^\s*$key\s.*|$key $value;|" "$tmp"; else echo "$key $value;" >> "$tmp"; fi
      show_diff "$NGINX_TUNING_CONF" "$tmp"; [[ $dry -eq 1 ]] && exit 0
      start_backup nginx; install_file "$tmp" "$NGINX_TUNING_CONF"
    fi
    nginx_finish ;;

  nginx-measure)
    [[ $key == on || $key == off ]] || die "使い方: apply-item.sh nginx-measure on|off"
    main=/etc/nginx/nginx.conf
    as_root grep -qE '^\s*include\s+/etc/nginx/conf\.d/\*\.conf;' $main || die "nginx.conf に conf.d の include が無い"
    [[ $dry -eq 1 ]] && { log "nginx-measure $key: log_format json と include /etc/nginx/isucon/access_log.conf を $NGINX_TUNING_CONF に追記し、access_log.$key.conf を有効にする"; exit 0; }
    start_backup nginx
    tmp="$(mktemp)"
    if as_root test -f "$NGINX_TUNING_CONF"; then as_root cat "$NGINX_TUNING_CONF" > "$tmp"; else echo "# 秘伝のタレ: http 文脈のチューニング（apply-item.sh が追記）" > "$tmp"; fi
    if ! grep -q 'log_format json' "$tmp"; then
      { echo; echo "# alp 用の JSON ログ。問題側の access_log はそのまま残し、別ファイルに追加で出す"; cat "$INFRA_DIR/nginx/isucon/log_format_json.conf"; echo "include /etc/nginx/isucon/access_log.conf;"; } >> "$tmp"
    fi
    install_file "$tmp" "$NGINX_TUNING_CONF"
    install_file "$INFRA_DIR/nginx/isucon/proxy_keepalive.conf" /etc/nginx/isucon/proxy_keepalive.conf
    install_file "$INFRA_DIR/nginx/isucon/access_log.on.conf" /etc/nginx/isucon/access_log.on.conf
    install_file "$INFRA_DIR/nginx/isucon/access_log.off.conf" /etc/nginx/isucon/access_log.off.conf
    install_file "$INFRA_DIR/nginx/isucon/access_log.$key.conf" /etc/nginx/isucon/access_log.conf
    nginx_finish ;;

  mysql)
    [[ -n $key ]] || die "使い方: apply-item.sh mysql <key> [value]"
    [[ -n $(detect_db_unit) ]] || die "MySQL / MariaDB がありません"
    f="$MYSQL_CONF_DIR/99-isucon.cnf"
    tmp="$(upsert_kv "$f" "$key" "$value" "# 秘伝のタレ: 数値チューニング（apply-item.sh mysql が追記。標準設定は触らない）
[mysqld]")"
    show_diff "$f" "$tmp"; [[ $dry -eq 1 ]] && exit 0
    start_backup mysql; install_file "$tmp" "$f"; mysql_finish ;;

  mysql-main)
    [[ -n $key && -n $value ]] || die "使い方: apply-item.sh mysql-main <key> <value>"
    flavor="$(detect_db_flavor)"; [[ $flavor != none ]] || die "MySQL / MariaDB がありません"
    [[ $flavor == mariadb ]] && f=/etc/mysql/mariadb.conf.d/50-server.cnf || f=/etc/mysql/mysql.conf.d/mysqld.cnf
    as_root grep -qE "^\s*$key\s*=" "$f" || die "$f に $key の行が無い。mysql（conf.d 追記）を使う"
    tmp="$(mktemp)"; as_root cat "$f" > "$tmp"; sed -i -E "s|^(\s*$key\s*=\s*).*|\1$value|" "$tmp"
    show_diff "$f" "$tmp"; [[ $dry -eq 1 ]] && exit 0
    start_backup mysql; install_file "$tmp" "$f"; mysql_finish ;;

  mysql-measure)
    [[ $key == on || $key == off ]] || die "使い方: apply-item.sh mysql-measure on|off"
    [[ -n $(detect_db_unit) ]] || die "MySQL / MariaDB がありません"
    [[ $dry -eq 1 ]] && { log "mysql-measure $key: 99-isucon-slowlog.cnf を配置 / 削除し、SET GLOBAL で即時切り替え"; exit 0; }
    start_backup mysql
    if [[ $key == on ]]; then
      install_file "$INFRA_DIR/mysql/99-isucon-slowlog.cnf" "$MYSQL_CONF_DIR/99-isucon-slowlog.cnf"
      mysql_root -e "SET GLOBAL slow_query_log_file = '$MYSQL_SLOW_LOG'; SET GLOBAL long_query_time = 0; SET GLOBAL slow_query_log = ON;"
      warn "long_query_time は新しい接続から効く。アプリを再起動する"; [[ $no_restart -eq 1 ]] || restart_app
    else
      if as_root test -f "$MYSQL_CONF_DIR/99-isucon-slowlog.cnf"; then backup_file "$MYSQL_CONF_DIR/99-isucon-slowlog.cnf"; as_root rm -f "$MYSQL_CONF_DIR/99-isucon-slowlog.cnf"; fi
      mysql_root -e "SET GLOBAL slow_query_log = OFF; SET GLOBAL long_query_time = 10;"
    fi
    log "MySQL: slow_query_log $key"; health_check || true ;;

  sysctl)
    [[ -n $key && -n $value ]] || die "使い方: apply-item.sh sysctl <key> <value>"
    f=/etc/sysctl.d/99-isucon.conf
    tmp="$(upsert_kv "$f" "$key" "$value" "# 秘伝のタレ: カーネルパラメータ（apply-item.sh sysctl が追記）")"
    show_diff "$f" "$tmp"; log "現在値: $(sysctl -n "$key" 2>/dev/null | tr '\t' ' ')"; [[ $dry -eq 1 ]] && exit 0
    start_backup sysctl
    sysctl -n "$key" 2>/dev/null | sed "s/^/$key = /" | as_root tee -a "$BACKUP_DIR/sysctl-before.txt" >/dev/null || true
    install_file "$tmp" "$f"
    as_root sysctl -w "$key=$value" >/dev/null || die "sysctl -w に失敗（キー名か値を確認）"
    log "反映: $key = $(sysctl -n "$key" | tr '\t' ' ')" ;;

  limits)
    [[ -n $key ]] || die "使い方: apply-item.sh limits <nofile>"
    f=/etc/security/limits.d/99-isucon-limits.conf; tmp="$(mktemp)"
    printf '# 秘伝のタレ: ログインシェルの fd 上限（apply-item.sh limits）。新しいログインから効く\n*\tsoft\tnofile\t%s\n*\thard\tnofile\t%s\nroot\tsoft\tnofile\t%s\nroot\thard\tnofile\t%s\n' "$key" "$key" "$key" "$key" > "$tmp"
    show_diff "$f" "$tmp"; [[ $dry -eq 1 ]] && exit 0
    start_backup limits; install_file "$tmp" "$f"; log "新しいログインから nofile=$key" ;;

  limits-unit)
    [[ -n $key && -n $value ]] || die "使い方: apply-item.sh limits-unit <unit> <nofile>"
    unit="${key%.service}"; has_unit "$unit" || die "ユニットがありません: $unit"
    f="/etc/systemd/system/$unit.service.d/99-isucon-limits.conf"; tmp="$(mktemp)"
    printf '[Service]\nLimitNOFILE=%s\n' "$value" > "$tmp"
    show_diff "$f" "$tmp"; log "現在: $(systemctl show "$unit" -p LimitNOFILE --value)"; [[ $dry -eq 1 ]] && exit 0
    start_backup limits; install_file "$tmp" "$f"; as_root systemctl daemon-reload
    if [[ $no_restart -eq 1 ]]; then warn "--no-restart: $unit は再起動していない。次の restart から効く"; else as_root systemctl restart "$unit"; sleep 1; log "$unit 再起動。LimitNOFILE=$(systemctl show "$unit" -p LimitNOFILE --value)"; health_check || true; fi ;;

  *) die "不明な target: ${target}（nginx-main / nginx-http / nginx-measure / mysql / mysql-main / mysql-measure / sysctl / limits / limits-unit）" ;;
esac
[[ $dry -eq 1 ]] || log "完了。バックアップ: ${BACKUP_DIR:-（なし）}"
