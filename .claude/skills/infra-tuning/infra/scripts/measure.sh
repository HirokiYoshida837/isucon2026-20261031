#!/usr/bin/env bash
# 計測（ログ・APM）の ON/OFF を切り替える（各サーバーで実行。無いものは飛ばす）。
#   ./measure.sh on       # 計測する: アクセスログ ON、スロークエリログ ON、NewRelic infra ON
#   ./measure.sh off      # 最終提出用: すべて OFF
#   ./measure.sh status   # 今の状態を表示
#
# 再起動試験の前に必ず off にする。off は OS 再起動後も OFF のまま残る（設定ファイルごと切り替えるため）。
# mysqld の再起動はしない（SET GLOBAL で即時に切り替える）。
#   ただし long_query_time は「接続ごと」の値。on にした後は、アプリの既存接続を入れ替えるためアプリを再起動する。
# NewRelic Go agent（アプリ側）はここでは止めない。アプリの環境変数で無効化する（README 参照）。
set -euo pipefail
# shellcheck source=lib.sh
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"

mode="${1:-status}"

status() {
  echo "== nginx access_log"
  if as_root test -f /etc/nginx/isucon/access_log.conf; then
    as_root grep -v '^#' /etc/nginx/isucon/access_log.conf | sed 's/^/  /'
  else
    echo "  (isucon 設定なし)"
  fi
  echo "== MySQL slow log"
  if [[ -n $(detect_db_unit) ]]; then
    mysql_root -N -e "SELECT CONCAT('  slow_query_log=', @@slow_query_log, ' long_query_time=', @@long_query_time)" 2>/dev/null || echo "  (取得失敗)"
    if as_root test -f "$MYSQL_CONF_DIR/99-isucon-slowlog.cnf"; then echo "  設定ファイル: あり（再起動後も ON）"; else echo "  設定ファイル: なし（再起動後は OFF）"; fi
  else
    echo "  (DB なし)"
  fi
  echo "== NewRelic infra agent"
  if has_unit newrelic-infra; then
    printf '  active=%s enabled=%s\n' "$(systemctl is-active newrelic-infra || true)" "$(systemctl is-enabled newrelic-infra || true)"
  else
    echo "  (未導入)"
  fi
}

switch_nginx() {
  local onoff="$1"
  as_root test -f "/etc/nginx/isucon/access_log.$onoff.conf" || { log "nginx: isucon 設定なし。スキップ"; return; }
  as_root cp "/etc/nginx/isucon/access_log.$onoff.conf" /etc/nginx/isucon/access_log.conf
  as_root nginx -t 2>/dev/null || die "nginx -t に失敗"
  as_root systemctl reload nginx
  log "nginx: access_log $onoff"
}

switch_mysql() {
  local onoff="$1"
  [[ -n $(detect_db_unit) ]] || { log "MySQL: なし。スキップ"; return; }
  if [[ $onoff == on ]]; then
    as_root install -m 0644 "$INFRA_DIR/mysql/99-isucon-slowlog.cnf" "$MYSQL_CONF_DIR/99-isucon-slowlog.cnf"
    mysql_root -e "SET GLOBAL slow_query_log_file = '$MYSQL_SLOW_LOG'; SET GLOBAL long_query_time = 0; SET GLOBAL slow_query_log = ON;"
    warn "MySQL: long_query_time は新しい接続から効く。アプリを再起動すること"
  else
    as_root rm -f "$MYSQL_CONF_DIR/99-isucon-slowlog.cnf"
    mysql_root -e "SET GLOBAL slow_query_log = OFF; SET GLOBAL long_query_time = 10;"
  fi
  log "MySQL: slow_query_log $onoff"
}

switch_newrelic_infra() {
  local onoff="$1"
  has_unit newrelic-infra || { log "NewRelic infra: 未導入。スキップ"; return; }
  if [[ $onoff == on ]]; then
    as_root systemctl enable --now newrelic-infra
  else
    as_root systemctl disable --now newrelic-infra
  fi
  log "NewRelic infra: $onoff"
}

case "$mode" in
  on|off)
    require_sudo
    switch_nginx "$mode"
    switch_mysql "$mode"
    switch_newrelic_infra "$mode"
    status
    if [[ $mode == off ]]; then
      warn "アプリ側の NewRelic Go agent と pprof 等は別途止める（README の「再起動試験の前に戻すもの」）"
    fi
    ;;
  status) status ;;
  *) die "使い方: $0 on|off|status" ;;
esac
