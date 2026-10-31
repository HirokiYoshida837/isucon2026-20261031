#!/usr/bin/env bash
# ベンチ直前に実行する（nginx のあるサーバー・DB のあるサーバーのそれぞれで）。
#   ./bench-before.sh [ラベル]
#     ラベル: 結果ディレクトリ名に付ける目印（例: add-index-users）。省略可。
#
# やること
# 1. 結果ディレクトリ $BENCH_LOG_ROOT/<日時>[-ラベル]/ を作り、bench-after.sh に引き継ぐ
# 2. nginx アクセスログを空にする（このサーバーに nginx があれば）
# 3. MySQL スロークエリログを空にする（このサーバーに MySQL があれば）
# 4. vmstat を裏で起動し、CPU 使用率を 1 秒ごとに記録する
# 5. resource-sampler.sh（CPU・メモリ・ディスク・NW・TCP・DB 接続数）と pidstat（プロセス別 CPU・メモリ）を裏で起動する
#
# 前回分の生ログは消える。残したいときは bench-after.sh の KEEP_RAW=1 で保存しておく。
set -euo pipefail
# shellcheck source=lib.sh
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"

label="${1:-}"
require_sudo

run_id="$(date +%Y%m%d-%H%M%S)${label:+-$label}"
run_dir="$BENCH_LOG_ROOT/$run_id"
mkdir -p "$run_dir"
echo "$run_id" > "$BENCH_LOG_ROOT/.current"
date '+%Y-%m-%dT%H:%M:%S%z' > "$run_dir/started_at"
log "結果ディレクトリ: $run_dir"

# 前回の記録プロセスが残っていれば止める。
for pf in .vmstat.pid .sampler.pid .pidstat.pid; do
  if [[ -f $BENCH_LOG_ROOT/$pf ]]; then
    kill "$(cat "$BENCH_LOG_ROOT/$pf")" 2>/dev/null || true
    rm -f "$BENCH_LOG_ROOT/$pf"
  fi
done

##
# nginx
##
if as_root test -f "$NGINX_ACCESS_LOG"; then
  # nginx は O_APPEND で書いているので、truncate しても書き込み位置は先頭に戻る。
  as_root truncate -s 0 "$NGINX_ACCESS_LOG"
  # buffer 指定のログを確実に吐き出させるため reopen もしておく。
  as_root nginx -s reopen 2>/dev/null || warn "nginx -s reopen に失敗（nginx が止まっている？）"
  log "nginx アクセスログを空にした: $NGINX_ACCESS_LOG"
else
  log "nginx アクセスログなし（このサーバーに nginx が無い or 計測 OFF）"
fi

##
# MySQL
##
if as_root test -f "$MYSQL_SLOW_LOG"; then
  as_root truncate -s 0 "$MYSQL_SLOW_LOG"
  # ファイルが消されていた場合に備えて開き直させる（MySQL 8 / MariaDB 共通の構文）。
  mysql_root -e 'FLUSH SLOW LOGS' 2>/dev/null || warn "FLUSH SLOW LOGS に失敗（MySQL が止まっている？）"
  log "スロークエリログを空にした: $MYSQL_SLOW_LOG"
else
  log "スロークエリログなし（このサーバーに MySQL が無い or 計測 OFF）"
fi

##
# リソース記録
##
nohup vmstat -n -t 1 > "$run_dir/vmstat.log" 2>&1 &
echo $! > "$BENCH_LOG_ROOT/.vmstat.pid"
disown || true
log "vmstat 記録開始（pid $(cat "$BENCH_LOG_ROOT/.vmstat.pid")）"

# CPU・メモリ・ディスク・NW・TCP・DB 接続数を 2 秒ごとに記録（間隔は SAMPLE_INTERVAL で変更可）
nohup bash "$INFRA_DIR/scripts/resource-sampler.sh" "$run_dir/resources.tsv" "${SAMPLE_INTERVAL:-2}" >/dev/null 2>&1 &
echo $! > "$BENCH_LOG_ROOT/.sampler.pid"
disown || true
log "リソース記録開始（pid $(cat "$BENCH_LOG_ROOT/.sampler.pid")）"

# プロセス別の CPU・メモリ（htop 相当）を 5 秒ごとに記録
if has_cmd pidstat; then
  LC_ALL=C nohup pidstat -h -u -r "${PIDSTAT_INTERVAL:-5}" > "$run_dir/pidstat.log" 2>&1 &
  echo $! > "$BENCH_LOG_ROOT/.pidstat.pid"
  disown || true
  log "pidstat 記録開始（pid $(cat "$BENCH_LOG_ROOT/.pidstat.pid")）"
else
  warn "pidstat が無い（setup-tools.sh で sysstat を入れる）。プロセス別の記録はスキップ"
fi

log "準備完了。ベンチを実行してから bench-after.sh を実行する"
