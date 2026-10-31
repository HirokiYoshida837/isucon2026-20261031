#!/usr/bin/env bash
# ISUCON2026: ポータルでベンチの完了を確認したら、すぐに実行する。完了時刻を記録して Discord に投稿する
# 使い方: bench-end.sh [スコア] [PASS|FAIL]
set -uo pipefail
. "$(dirname "$0")/lib.sh"
END=$(date +%s)
echo "$END" > "$ISU_LOG_DIR/.bench-end"
START=$(cat "$ISU_LOG_DIR/.bench-start" 2>/dev/null || echo "")
DUR=""; [ -n "$START" ] && DUR="（所要 $((END - START)) 秒）"
"$POST" "🏁 **bench end** by $ISU_MEMBER
完了: $(jst "$END")${DUR}（epoch ms: ${END}000）${1:+
score: $1}${2:+ / $2}
集計中…"
