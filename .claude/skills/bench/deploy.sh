#!/usr/bin/env bash
# ISUCON2026: 各競技サーバーで ISU_DEPLOY_CMD（Makefile の deploy。gh で取得 → build → 再起動）を実行し、Discord に通知する
# 取得対象は環境変数 REF として Makefile に渡す（Makefile 側で $(REF) を使う。既定 main）
#   - ブランチ名（例: main、add-index-users）→ Makefile 側で `gh repo sync --branch` + `git switch`
#   - PR 番号（数字のみ、例: 42）→ Makefile 側で `gh pr checkout`（そのPRの最新を取り込む）
# ISU_DEPLOY_CMD を複数コマンドにするときは `bash -ec "..."` で包むこと（`REF=x cmd1 && cmd2` だと REF が cmd1 にしか渡らず、途中で落ちても exit 0 になる）
# 使い方: deploy.sh [ブランチ名 or PR番号（既定 main）] [ホスト...（既定 ISU_HOSTS）]
set -uo pipefail
. "$(dirname "$0")/lib.sh"
REF="${1:-main}"; shift || true
HOSTS="${*:-$ISU_HOSTS}"
if [[ $REF =~ ^[0-9]+$ ]]; then LABEL="PR #$REF"; else LABEL="$REF"; fi
TS=$(date +%Y%m%d-%H%M%S); OUT="$ISU_LOG_DIR/deploy-$TS"; mkdir -p "$OUT"
# 設定ミス（ホスト名が引けない等）は Discord に流さず、手元で止める
for h in $HOSTS; do
  ssh "${SSH_OPTS[@]}" "$h" true >/dev/null 2>&1 || { echo "ssh できないホスト: ${h}（~/.ssh/config と ISU_HOSTS を確認）" >&2; exit 2; }
done
LINES=(); FAIL=0; COMMIT=""
for h in $HOSTS; do
  # 取得・ビルド・再起動は Makefile に任せる。終わったら反映されたコミットを最終行に出す
  ssh "${SSH_OPTS[@]}" "$h" "set -e; cd '$ISU_REMOTE_DIR'; REF='$REF' $ISU_DEPLOY_CMD; echo \"__COMMIT__ \$(git log -1 --format='%h %s')\"" > "$OUT/$h.log" 2>&1
  rc=$?
  # 偽の成功を防ぐ: ブランチ指定のときは、サーバーの HEAD がリモートのブランチ先頭と一致することを確かめる
  # （取得方法が gh repo sync でも git fetch でも効くよう、origin/<REF> ではなく ls-remote で見る）
  if [ $rc -eq 0 ] && ! [[ $REF =~ ^[0-9]+$ ]]; then
    ssh "${SSH_OPTS[@]}" "$h" "cd '$ISU_REMOTE_DIR' && [ \"\$(git rev-parse HEAD)\" = \"\$(git ls-remote origin 'refs/heads/$REF' | cut -f1)\" ]" >/dev/null 2>&1 || { rc=3; echo "HEAD がリモートの $REF と不一致" >> "$OUT/$h.log"; }
  fi
  c=$(grep '^__COMMIT__ ' "$OUT/$h.log" | tail -1 | sed 's/^__COMMIT__ //')
  [ -z "$COMMIT" ] && [ $rc -eq 0 ] && COMMIT="$c"
  if [ $rc -eq 0 ]; then LINES+=("✅ $h"); else LINES+=("❌ $h (exit $rc)"); FAIL=1; fi
done
MSG="🚀 **deploy** \`$LABEL\` by $ISU_MEMBER
commit: \`${COMMIT:-不明}\`
$(printf '%s\n' "${LINES[@]}")"
echo "$MSG"
if [ $FAIL -eq 1 ]; then
  ARGS=(); for f in "$OUT"/*.log; do ARGS+=(-f "$f"); done
  "$POST" "${ARGS[@]}" "$MSG"$'\n'"失敗したホストのログを添付"
  exit 1
fi
"$POST" "$MSG"
