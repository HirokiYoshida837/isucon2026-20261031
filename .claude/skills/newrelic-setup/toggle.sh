#!/usr/bin/env bash
# ISUCON2026: NewRelic の計測を全台で止める / 戻す（Go agent と infra agent の両方）
#   toggle.sh off [ユニット名] [ホスト...]   最終提出・再起動試験の前。env を NEW_RELIC_ENABLED=false にしてアプリ再起動、infra agent を disable --now
#   toggle.sh on  [ユニット名] [ホスト...]   計測を再開
# ユニット名の既定は ISU_APP_UNIT、ホストの既定は ISU_HOSTS。コードや drop-in は触らない（revert 不要で戻せる）
# nginx のアクセスログ・MySQL のスロークエリログは infra/scripts/measure.sh off で別に止める
set -euo pipefail
. "$(dirname "$0")/lib.sh"
MODE="${1:-}"; shift || true
[[ "$MODE" == on || "$MODE" == off ]] || die "使い方: toggle.sh on|off [ユニット名] [ホスト...]"
UNIT="${1:-$ISU_APP_UNIT}"; shift || true
[ -n "$UNIT" ] || die "ユニット名が必要（引数か isucon.env の ISU_APP_UNIT）"
UNIT="${UNIT%.service}"
HOSTS=$(hosts_or_default "$@")
[ "$MODE" = on ] && VAL=true || VAL=false

REMOTE_SCRIPT=$(cat <<'EOS'
set -euo pipefail
MODE="$1"; VAL="$2"; UNIT="$3"; REMOTE_ENV="$4"
if [ -f "$REMOTE_ENV" ]; then
  if grep -q '^NEW_RELIC_ENABLED=' "$REMOTE_ENV"; then sed -i "s/^NEW_RELIC_ENABLED=.*/NEW_RELIC_ENABLED=$VAL/" "$REMOTE_ENV"; else echo "NEW_RELIC_ENABLED=$VAL" >> "$REMOTE_ENV"; fi
  echo "env: NEW_RELIC_ENABLED=$VAL"
else
  echo "env: $REMOTE_ENV が無い（Go agent は未設定）"
fi
if systemctl list-unit-files "$UNIT.service" --no-legend 2>/dev/null | grep -q "^$UNIT\.service"; then
  sudo systemctl restart "$UNIT"; sleep 3
  echo "app: $UNIT 再起動 active=$(systemctl is-active "$UNIT")"
else
  echo "app: ユニット $UNIT が無い。スキップ"
fi
if systemctl list-unit-files newrelic-infra.service --no-legend 2>/dev/null | grep -q '^newrelic-infra'; then
  if [ "$MODE" = on ]; then sudo systemctl enable --now newrelic-infra; else sudo systemctl disable --now newrelic-infra; fi
  echo "infra: active=$(systemctl is-active newrelic-infra || true) enabled=$(systemctl is-enabled newrelic-infra || true)"
else
  echo "infra: 未導入。スキップ"
fi
EOS
)

FAIL=0
for h in $HOSTS; do
  log "== $h ($MODE)"
  if remote_bash "$h" "$MODE" "$VAL" "$UNIT" "$REMOTE_ENV" <<<"$REMOTE_SCRIPT" 2>&1 | sed "s/^/  /"; then :; else warn "❌ $h: 失敗"; FAIL=1; fi
done
if [ $FAIL -eq 0 ]; then
  if [ "$MODE" = off ]; then log "✅ NewRelic を全台で停止。disable 済みなので OS 再起動後も止まったまま。ログ類は infra/scripts/measure.sh off で止める"
  else log "✅ NewRelic を全台で再開。1〜2 分後に verify.sh で確認"; fi
fi
exit $FAIL
