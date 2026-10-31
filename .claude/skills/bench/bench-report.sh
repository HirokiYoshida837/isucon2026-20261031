#!/usr/bin/env bash
# ISUCON2026: ベンチ後に集計（各サーバーの ISU_AFTER_CMD と NewRelic）して Discord に結果を投稿する
# 使い方: bench-report.sh <スコア> [メモ] [集計する直近の分数]
# 期間: bench-start.sh の開始時刻 〜 bench-end.sh の完了時刻（無ければ今）。NewRelic もこの期間で集計する
set -uo pipefail
. "$(dirname "$0")/lib.sh"
SCORE="${1:?スコアを指定して（ポータルの値）}"; MEMO="${2:-}"; MIN="${3:-}"
START=$(cat "$ISU_LOG_DIR/.bench-start" 2>/dev/null || echo "")
END=$(cat "$ISU_LOG_DIR/.bench-end" 2>/dev/null || date +%s)
if [ -z "$MIN" ] && [ -n "$START" ]; then
  # NewRelic はベンチ期間ぴったりで集計する（データの到着遅れを見込んで完了 +30 秒まで）
  export NR_SINCE_MS="${START}000" NR_UNTIL_MS="$((END + 30))000"
  MIN=$(( (END - START) / 60 + 1 ))
  PERIOD="$(jst "$START") 〜 $(jst "$END" | cut -d' ' -f2)（$((END - START)) 秒）"
else
  MIN="${MIN:-5}"
  PERIOD="直近 ${MIN} 分（開始時刻の記録なし）"
fi
first=${ISU_HOSTS%% *}
COMMIT=$(ssh "${SSH_OPTS[@]}" "$first" "cd '$ISU_REMOTE_DIR' && git log -1 --format='%h %s'" 2>/dev/null || echo 不明)

# REPOST_DIR=<保存先> なら集計し直さず、保存済みの結果から投稿だけやり直す（履歴にも追記しない）
if [ -n "${REPOST_DIR:-}" ]; then
  OUT="$REPOST_DIR"; [ -d "$OUT" ] || { echo "保存先が無い: $OUT" >&2; exit 1; }
  COMMIT=$(awk -F'\t' -v s="$SCORE" '$2 == s {c = $3} END {print c}' "$ISU_LOG_DIR/scores.tsv")
  : "${COMMIT:=不明}"
else
TS=$(date +%Y%m%d-%H%M%S); OUT="$ISU_LOG_DIR/bench-$TS"; mkdir -p "$OUT"

# 各サーバーの集計（alp / pt-query-digest など）
if [ -n "$ISU_AFTER_CMD" ]; then
  for h in $ISU_HOSTS; do
    ssh "${SSH_OPTS[@]}" "$h" "$ISU_AFTER_CMD" > "$OUT/$h-after.txt" 2>&1 || echo "(集計コマンド失敗)" >> "$OUT/$h-after.txt"
  done
fi
# NewRelic（キー未設定やデータなしでも止めない）
if [ -x "$NRQL" ]; then
  "$NRQL" --table endpoints "$MIN" > "$OUT/nr-endpoints.tsv" 2>/dev/null || echo "(取得失敗)" > "$OUT/nr-endpoints.tsv"
  "$NRQL" --table sql "$MIN"       > "$OUT/nr-sql.tsv"       2>/dev/null || echo "(取得失敗)" > "$OUT/nr-sql.tsv"
  "$NRQL" --table hosts "$MIN"     > "$OUT/nr-hosts.tsv"     2>/dev/null || echo "(取得失敗)" > "$OUT/nr-hosts.tsv"
fi

# pprof（bench-start.sh が仕込んだ CPU プロファイル）を回収して集計
PPROF=$("$SKILL_DIR/pprof-collect.sh" "$OUT" 2>/dev/null)
fi
[ -z "${REPOST_DIR:-}" ] || PPROF=$(PPROF_LOCAL=1 "$SKILL_DIR/pprof-collect.sh" "$OUT" 2>/dev/null)

# スコア履歴（TSV）
HIST="$ISU_LOG_DIR/scores.tsv"
[ -f "$HIST" ] || printf 'time\tscore\tcommit\tmember\tmemo\tstart_epoch\tend_epoch\n' > "$HIST"
if [ -n "${REPOST_DIR:-}" ]; then
  # 再投稿: 今回の行を除いた履歴からベストと前回を出す
  BEST=$(awk -F'\t' -v s="$SCORE" 'NR>1 && $2 != s && $2+0>m {m=$2+0} END {print m+0}' "$HIST")
  PREV=$(awk -F'\t' -v s="$SCORE" 'NR>1 && $2 != s {p=$2} END {print p+0}' "$HIST")
else
BEST=$(awk -F'\t' 'NR>1 && $2+0>m {m=$2+0} END {print m+0}' "$HIST")
PREV=$(awk -F'\t' 'NR>1 {p=$2} END {print p+0}' "$HIST")
printf '%s\t%s\t%s\t%s\t%s\t%s\t%s\n' "$(date '+%H:%M')" "$SCORE" "$COMMIT" "$ISU_MEMBER" "$MEMO" "$START" "$END" >> "$HIST"
fi
MARK=""; [ "$SCORE" -gt "$BEST" ] 2>/dev/null && MARK=" 🏆 自己ベスト"

top() { [ -f "$1" ] && head -6 "$1" | cut -c1-160; }
# 各サーバーの集計（resources.txt の表）から、主要項目の p95 / max を 1 行ずつ抜き出す
host_table() {
  printf '%-8s %9s %9s %9s %8s %8s %8s\n' host cpu_p95 cpu_max mem_max load_max tcp_max db_conn
  for h in $ISU_HOSTS; do
    f="$OUT/$h-after.txt"
    [ -f "$f" ] || continue
    awk -v h="$h" '
      $1 == "cpu_busy" {cp = $4; cm = $5} $1 == "mem_used_mb" {mm = $5} $1 == "load1" {lm = $5}
      $1 == "tcp_estab" {tm = $5} $1 == "db_conn" {dm = $5}
      END {printf "%-8s %9s %9s %9s %8s %8s %8s\n", h, cp, cm, mm, lm, tm, (dm == "" ? "-" : dm)}' "$f"
  done
}
MSG="📊 **bench result: $SCORE**${MARK}（前回 $PREV / ベスト ${BEST}）
commit: \`$COMMIT\` by $ISU_MEMBER${MEMO:+
memo: $MEMO}
期間: ${PERIOD}${NR_SINCE_MS:+
NewRelic: \`NR_SINCE_MS=$NR_SINCE_MS NR_UNTIL_MS=$NR_UNTIL_MS\`（NRQL なら \`SINCE $NR_SINCE_MS UNTIL $NR_UNTIL_MS\`）}
**endpoints 上位（NewRelic）**
\`\`\`
$(top "$OUT/nr-endpoints.tsv")
\`\`\`
**サーバー別リソース（ベンチ中・%, MB）**
\`\`\`
$(host_table)
\`\`\`
**アプリの CPU 内訳（pprof、負荷中 ${ISU_PPROF_SECONDS} 秒）**
\`\`\`
${PPROF:-（取得なし）}
\`\`\`
詳細（プロセス別 CPU・メモリ、DB 接続数、ディスク・NW、alp、pt-query-digest、pprof の上位 40）は添付"
# 添付は 9 個まで（Discord の上限 10 個から、本文が長いときの全文添付の分を空ける。.prof と、プロファイルが取れなかったホストの pprof ログは外す）
ARGS=(); n=0
for f in "$OUT"/*; do
  case "$f" in *.prof) continue;; *pprof-*.txt) grep -q "flat 上位" "$f" || continue;; esac
  [ -s "$f" ] || continue
  [ "$n" -lt 9 ] || { echo "添付が 9 個を超えたので省略: $(basename "$f")" >&2; continue; }
  ARGS+=(-f "$f"); n=$((n + 1))
done
"$POST" ${ARGS[@]+"${ARGS[@]}"} "$MSG" || echo "投稿に失敗。REPOST_DIR=$OUT $0 $SCORE で再投稿できる" >&2
echo "saved: $OUT"
