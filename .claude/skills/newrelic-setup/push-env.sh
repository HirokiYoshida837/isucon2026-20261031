#!/usr/bin/env bash
# ISUCON2026: 各サーバーに /home/isucon/newrelic.env（権限 600）を配置する
# 使い方: push-env.sh [ホスト...（既定 ISU_HOSTS）]
# 中身: ライセンスキー、アプリ名（全台同じ）、ホスト表示名（ssh の Host 名）。値は画面に出さない
set -euo pipefail
. "$(dirname "$0")/lib.sh"
need_license
HOSTS=$(hosts_or_default "$@")

for h in $HOSTS; do
  printf 'NEW_RELIC_LICENSE_KEY=%s\nNEW_RELIC_APP_NAME=%s\nNEW_RELIC_PROCESS_HOST_DISPLAY_NAME=%s\nNEW_RELIC_ENABLED=true\nNEW_RELIC_DISTRIBUTED_TRACING_ENABLED=true\nNEW_RELIC_LOG=stderr\nNEW_RELIC_LOG_LEVEL=info\n' \
    "$NEW_RELIC_LICENSE_KEY" "$NEW_RELIC_APP_NAME" "$h" \
    | ssh "${SSH_OPTS[@]}" "$h" "umask 077; cat > $REMOTE_ENV && chmod 600 $REMOTE_ENV && echo \"\$(hostname): $REMOTE_ENV \$(grep -c = $REMOTE_ENV) 行\"" \
    && log "✅ $h: env 配置（app=$NEW_RELIC_APP_NAME, display=${h}）" \
    || warn "❌ $h: 配置に失敗"
done
