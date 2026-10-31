#!/usr/bin/env bash
# 各サーバーに infra/scripts を配ってから setup-gh.sh（gh の導入。認証はしない）を実行する。
# 使い方: install.sh [ホスト...（既定 ISU_HOSTS）]
set -uo pipefail
. "$(dirname "$0")/lib.sh"
HOSTS="${*:-$ISU_HOSTS}"
LOCAL_INFRA_SCRIPTS="$(cd "$SKILL_DIR/../infra-tuning/infra/scripts" && pwd)"

FAIL=0
for h in $HOSTS; do
  echo "== $h =="
  if ! ssh "${SSH_OPTS[@]}" "$h" "mkdir -p '$ISU_INFRA_REMOTE_DIR/scripts'"; then
    echo "❌ $h: ssh 接続に失敗"
    FAIL=1
    continue
  fi
  if ! scp -rq "${SSH_OPTS[@]}" "$LOCAL_INFRA_SCRIPTS"/* "$h:$ISU_INFRA_REMOTE_DIR/scripts/"; then
    echo "❌ $h: infra/scripts の配布に失敗"
    FAIL=1
    continue
  fi
  if ssh "${SSH_OPTS[@]}" "$h" "cd '$ISU_INFRA_REMOTE_DIR/scripts' && chmod +x setup-gh.sh lib.sh && ./setup-gh.sh"; then
    echo "✅ $h: gh 導入 OK"
  else
    echo "❌ $h: setup-gh.sh が失敗"
    FAIL=1
  fi
done
exit $FAIL
