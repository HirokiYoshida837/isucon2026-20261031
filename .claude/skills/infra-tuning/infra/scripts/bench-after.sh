#!/usr/bin/env bash
# ベンチ直後に実行する（bench-before.sh を実行したサーバーで）。
#   ./bench-after.sh [-m '正規表現1,正規表現2'] [-f matching-groups.txt] [-s sum|count|avg|max|p99] [-r run_id]
#
#   -m  alp の matching groups をカンマ区切りで直接渡す（カンマを含む正規表現は -f を使う）
#   -f  matching groups を 1 行 1 正規表現で書いたファイル（既定: infra/alp/matching-groups.txt）
#   -s  alp の並び順（既定: sum = 合計時間の大きい順。ここが一番効くエンドポイント）
#   -r  集計する結果ディレクトリ名（既定: 直前の bench-before.sh が作ったもの）
#
#   環境変数 KEEP_RAW=1  生ログを gzip して結果ディレクトリに残す（ディスク 8GB なので既定は残さない）
#   環境変数 PT_LIMIT=30 pt-query-digest に出すクエリ数
#
# 出力（$BENCH_LOG_ROOT/<run_id>/）
#   alp.txt              エンドポイント別の集計
#   alp-config.yml       alp に渡した設定（matching groups の記録）
#   alp-dump.yml         alp の集計結果（alp diff 用）
#   pt-query-digest.txt  クエリ別の集計
#   cpu.txt              ベンチ中の CPU 使用率（平均・p50・p95・最大）
#   resources.txt        CPU・メモリ・ディスク・NW・TCP・DB 接続数の avg / p50 / p95 / max
#   procs.txt            プロセス別 CPU・メモリの上位（htop 相当）
#   summary.txt          上の 3 つの先頭をまとめたもの（Discord に貼る用）
set -euo pipefail
# shellcheck source=lib.sh
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"

matching_csv=""
matching_file="$INFRA_DIR/alp/matching-groups.txt"
sort_key="sum"
run_id=""
while getopts "m:f:s:r:h" opt; do
  case "$opt" in
    m) matching_csv="$OPTARG" ;;
    f) matching_file="$OPTARG" ;;
    s) sort_key="$OPTARG" ;;
    r) run_id="$OPTARG" ;;
    h) sed -n '2,22p' "$0"; exit 0 ;;
    *) exit 1 ;;
  esac
done

require_sudo

if [[ -z $run_id ]]; then
  [[ -f $BENCH_LOG_ROOT/.current ]] || die "bench-before.sh を先に実行してください"
  run_id="$(cat "$BENCH_LOG_ROOT/.current")"
fi
run_dir="$BENCH_LOG_ROOT/$run_id"
mkdir -p "$run_dir"
date '+%Y-%m-%dT%H:%M:%S%z' > "$run_dir/finished_at"
log "結果ディレクトリ: $run_dir"

# 取れなかった項目を summary.txt の末尾に載せるために集める。
skipped=()
skip() { skipped+=("$1"); warn "$1"; }

##
# まとめ（途中で何が失敗しても summary.txt は必ず書く）
##
write_summary() {
  {
    echo "== run: $run_id  host: $(hostname)"
    [[ -s $run_dir/cpu.txt ]] && head -2 "$run_dir/cpu.txt"
    if [[ -s $run_dir/resources.txt ]]; then
      echo "== リソース（avg / p50 / p95 / max）"
      cat "$run_dir/resources.txt"
    fi
    if [[ -s $run_dir/procs.txt ]]; then
      echo "== プロセス別 CPU・メモリ上位"
      cat "$run_dir/procs.txt"
    fi
    echo "== ディスク残量"
    df -h / 2>/dev/null | tail -1
    if [[ -s $run_dir/alp.txt ]]; then
      echo "== alp（sort=$sort_key 上位 15）"
      head -18 "$run_dir/alp.txt"
    fi
    if [[ -s $run_dir/pt-query-digest.txt ]]; then
      echo "== pt-query-digest Profile（上位）"
      # "# Profile" から次の空行までがランキング表。
      awk '/^# Profile/ { p = 1 } p && /^$/ { exit } p { print; if (++k >= 20) exit }' "$run_dir/pt-query-digest.txt"
    fi
    if [[ ${#skipped[@]} -gt 0 ]]; then
      echo "== 取れなかった項目"
      printf -- '- %s\n' "${skipped[@]}"
    fi
  } > "$run_dir/summary.txt"
}

# 想定外のエラーで止まったときも、取れた分で summary.txt を書いてから終了する。
on_error() {
  local line="$1" cmd="$2"
  trap - ERR
  skip "行 $line でエラー（${cmd}）。取れた分だけで summary.txt を書いた"
  write_summary || true
  cat "$run_dir/summary.txt" >&2 || true
  exit 1
}
set -E
trap 'on_error "$LINENO" "$BASH_COMMAND"' ERR

##
# vmstat を止めて CPU を集計
##
for pf in .sampler.pid .pidstat.pid; do
  if [[ -f $BENCH_LOG_ROOT/$pf ]]; then
    kill "$(cat "$BENCH_LOG_ROOT/$pf")" 2>/dev/null || true
    rm -f "$BENCH_LOG_ROOT/$pf"
  fi
done
bash "$INFRA_DIR/scripts/resource-summary.sh" "$run_dir" || skip "リソース集計（resource-summary.sh）に失敗"
[[ -s $run_dir/resources.txt ]] || skip "resources.txt: サンプルが無い（ベンチが短すぎた、または resource-sampler.sh が動いていない）"
[[ -s $run_dir/procs.txt ]] || skip "procs.txt: pidstat のサンプルが無い"
if [[ -f $BENCH_LOG_ROOT/.vmstat.pid ]]; then
  kill "$(cat "$BENCH_LOG_ROOT/.vmstat.pid")" 2>/dev/null || true
  rm -f "$BENCH_LOG_ROOT/.vmstat.pid"
fi
if [[ -s $run_dir/vmstat.log ]]; then
  # ヘッダから us / sy / id / wa の列位置を探す（procps のバージョンで列が増減するため）。
  # 1 行目のデータは起動時からの平均なので捨てる。
  awk '
    /us +sy +id/ { for (i = 1; i <= NF; i++) col[$i] = i; next }
    /^ *[0-9]/ && ("id" in col) {
      n++; if (n == 1) next
      busy = 100 - $(col["id"]); print busy, $(col["us"]), $(col["sy"]), $(col["wa"])
    }' "$run_dir/vmstat.log" > "$run_dir/.cpu.tmp"
  if [[ -s $run_dir/.cpu.tmp ]]; then
    {
      echo "# CPU 使用率（100 - idle, %）host=$(hostname) nproc=$(nproc)"
      sort -n "$run_dir/.cpu.tmp" | awk '
        { v[NR] = $1; s += $1; us += $2; sy += $3; wa += $4 }
        END {
          p50 = v[int(NR * 0.50) > 0 ? int(NR * 0.50) : 1]
          p95 = v[int(NR * 0.95) > 0 ? int(NR * 0.95) : 1]
          printf "samples=%d avg=%.1f p50=%d p95=%d max=%d (avg us=%.1f sy=%.1f wa=%.1f)\n",
                 NR, s / NR, p50, p95, v[NR], us / NR, sy / NR, wa / NR
        }'
      echo "# メモリ"
      free -m
    } > "$run_dir/cpu.txt"
  else
    skip "cpu.txt: vmstat のサンプルが 2 行未満（ベンチが短すぎた）"
  fi
  rm -f "$run_dir/.cpu.tmp"
else
  skip "cpu.txt: vmstat.log が無い"
fi

##
# alp
##
# access_log の buffer に残っているログを書き出させる（空判定より前に行う）。
if as_root test -f "$NGINX_ACCESS_LOG"; then
  as_root nginx -s reopen 2>/dev/null || true
  sleep 1
fi
# JSON 形式の行だけを alp に渡す（ロールバック後の標準形式の行が混ざると alp json が止まるため）。
json_lines=0
total_lines=0
if as_root test -s "$NGINX_ACCESS_LOG"; then
  json_lines="$(as_root grep -c '^{' "$NGINX_ACCESS_LOG" 2>/dev/null || true)"
  total_lines="$(as_root wc -l "$NGINX_ACCESS_LOG" 2>/dev/null | awk '{print $1}' || true)"
fi
json_lines="${json_lines:-0}"
total_lines="${total_lines:-0}"
if [[ $json_lines -eq 0 ]]; then
  if [[ $total_lines -eq 0 ]]; then
    skip "alp: nginx アクセスログが空"
  else
    skip "alp: アクセスログに JSON 形式の行が無い（apply-item.sh nginx-measure on で JSON ログを出す）"
  fi
elif ! has_cmd alp; then
  skip "alp: コマンドが無い（setup-tools.sh を実行する）"
else
  [[ $json_lines -eq $total_lines ]] || warn "alp: JSON 以外の $((total_lines - json_lines)) 行を除外した"

  # 設定は YAML で渡す（--config 指定時は他のフラグが無視されるため、すべて YAML に書く）。
  cfg="$run_dir/alp-config.yml"
  {
    echo "sort: $sort_key"
    echo "reverse: true"
    echo "format: table"
    echo "show_footers: true"
    echo "percentiles: [50, 90, 99]"
    # alp diff <前回の alp-dump.yml> <今回の alp-dump.yml> で前回比を出せる。
    echo "dump: $run_dir/alp-dump.yml"
    echo "matching_groups:"
    if [[ -n $matching_csv ]]; then
      IFS=',' read -r -a groups <<< "$matching_csv"
      for g in "${groups[@]}"; do
        [[ -n $g ]] && printf "  - '%s'\n" "${g//\'/\'\'}"
      done
    elif [[ -f $matching_file ]]; then
      # 全行コメントのときは grep が 1 を返すので、|| true で続ける。
      { grep -vE '^[[:space:]]*(#|$)' "$matching_file" || true; } | while IFS= read -r g; do
        printf "  - '%s'\n" "${g//\'/\'\'}"
      done
    fi
  } > "$cfg"
  # matching groups が 0 件のとき YAML 上 null になるので、空配列にする。
  if ! grep -q "^  - " "$cfg"; then
    sed -i 's/^matching_groups:$/matching_groups: []/' "$cfg"
    warn "alp: matching groups が 0 件（URI をそのまま集計する）"
  fi

  if as_root grep '^{' "$NGINX_ACCESS_LOG" | alp json --config "$cfg" > "$run_dir/alp.txt" 2> "$run_dir/.alp.err"; then
    log "alp: $run_dir/alp.txt"
  else
    skip "alp: 集計に失敗（alp-config.yml を確認）: $(head -1 "$run_dir/.alp.err")"
    rm -f "$run_dir/alp.txt"
  fi
  rm -f "$run_dir/.alp.err"

  if [[ ${KEEP_RAW:-0} == 1 ]]; then
    as_root cat "$NGINX_ACCESS_LOG" | gzip -1 > "$run_dir/access.log.gz"
  fi
fi

##
# pt-query-digest
##
if ! as_root test -s "$MYSQL_SLOW_LOG"; then
  skip "pt-query-digest: スロークエリログが空か無い（apply-item.sh mysql-measure on で有効にする）"
elif ! has_cmd pt-query-digest; then
  skip "pt-query-digest: コマンドが無い（setup-tools.sh を実行する）"
else
  if as_root cat "$MYSQL_SLOW_LOG" | pt-query-digest --limit "${PT_LIMIT:-30}" > "$run_dir/pt-query-digest.txt" 2> "$run_dir/.pt.err"; then
    log "pt-query-digest: $run_dir/pt-query-digest.txt"
  else
    skip "pt-query-digest: 集計に失敗: $(head -1 "$run_dir/.pt.err")"
    rm -f "$run_dir/pt-query-digest.txt"
  fi
  rm -f "$run_dir/.pt.err"

  if [[ ${KEEP_RAW:-0} == 1 ]]; then
    as_root cat "$MYSQL_SLOW_LOG" | gzip -1 > "$run_dir/mysql-slow.log.gz"
  fi
fi

##
# まとめ
##
trap - ERR
write_summary
cat "$run_dir/summary.txt"
log "完了: $run_dir"
