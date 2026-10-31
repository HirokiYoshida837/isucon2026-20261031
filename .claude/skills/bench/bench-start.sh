#!/usr/bin/env bash
# ISUCON2026: ベンチ直前に各サーバーで準備コマンド（ログローテート等）を流し、開始を Discord に通知する
# 使い方: bench-start.sh [メモ]   → このあとポータルからベンチを実行する
set -uo pipefail
. "$(dirname "$0")/lib.sh"
MEMO="${1:-}"
if [ -n "$ISU_BEFORE_CMD" ]; then
  for h in $ISU_HOSTS; do
    ssh "${SSH_OPTS[@]}" "$h" "$ISU_BEFORE_CMD" >/dev/null 2>&1 || echo "⚠️ $h で準備コマンドが失敗" >&2
  done
fi
# ベンチ中の CPU プロファイル（pprof）の待機を各アプリサーバーで始める（回収は bench-report.sh）
"$SKILL_DIR/pprof-start.sh"
START=$(date +%s)
echo "$START" > "$ISU_LOG_DIR/.bench-start"
rm -f "$ISU_LOG_DIR/.bench-end"
first=${ISU_HOSTS%% *}
COMMIT=$(ssh "${SSH_OPTS[@]}" "$first" "cd '$ISU_REMOTE_DIR' && git log -1 --format='%h %s'" 2>/dev/null || echo 不明)
"$POST" "⏱️ **bench start** by $ISU_MEMBER
開始: $(jst "$START")（epoch ms: ${START}000）
commit: \`$COMMIT\`${MEMO:+
memo: $MEMO}"
