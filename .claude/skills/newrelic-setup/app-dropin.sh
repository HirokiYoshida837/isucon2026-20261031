#!/usr/bin/env bash
# ISUCON2026: アプリの systemd ユニットに /home/isucon/newrelic.env を読ませ、再起動して NewRelic 接続を確認する
# 使い方: app-dropin.sh [ユニット名（既定 ISU_APP_UNIT）] [ホスト...（既定 ISU_HOSTS）]
# 前提: push-env.sh 済み、アプリに Go agent のコード差分（go-agent-snippet.md）を入れてデプロイ済み
# ユニット本体は書き換えず、drop-in（<unit>.service.d/newrelic.conf）だけ足す。2 回目以降は差分が無ければ再起動しない
set -euo pipefail
. "$(dirname "$0")/lib.sh"
UNIT="${1:-$ISU_APP_UNIT}"; shift || true
[ -n "$UNIT" ] || die "ユニット名が必要（引数か isucon.env の ISU_APP_UNIT）"
UNIT="${UNIT%.service}"
HOSTS=$(hosts_or_default "$@")

REMOTE_SCRIPT=$(cat <<'EOS'
set -euo pipefail
UNIT="$1"; REMOTE_ENV="$2"
systemctl list-unit-files "$UNIT.service" --no-legend 2>/dev/null | grep -q "^$UNIT\.service" || { echo "❌ ユニット $UNIT.service が無い"; systemctl list-unit-files --no-legend 2>/dev/null | grep -iE 'isu|app|web' | awk '{print "   候補:", $1}'; exit 1; }
[ -f "$REMOTE_ENV" ] || { echo "❌ $REMOTE_ENV が無い。先に push-env.sh"; exit 1; }
D="/etc/systemd/system/$UNIT.service.d"; F="$D/newrelic.conf"
NEW=$(printf '[Service]\nEnvironmentFile=-%s\n' "$REMOTE_ENV")
if [ "$(sudo cat "$F" 2>/dev/null || true)" = "$NEW" ]; then
  echo "drop-in: 変更なし（${F}）。再起動して再接続する"
else
  sudo mkdir -p "$D"; printf '%s\n' "$NEW" | sudo tee "$F" >/dev/null
  sudo systemctl daemon-reload
  echo "drop-in: 作成（${F}）"
fi
sudo systemctl restart "$UNIT"
sleep 4
echo "unit: active=$(systemctl is-active "$UNIT")"
LOG=$(sudo journalctl -u "$UNIT" --since '-1min' --no-pager -o cat 2>/dev/null || true)
if grep -q 'application connected' <<<"$LOG"; then echo "agent: NewRelic に接続済み（$(grep -o '"app":"[^"]*"' <<<"$LOG" | head -1)）"
elif grep -qi 'NewRelic disabled' <<<"$LOG"; then echo "agent: 無効で起動（$(grep -i 'NewRelic disabled' <<<"$LOG" | tail -1 | cut -c1-120)）"
elif grep -qi 'newrelic' <<<"$LOG"; then echo "agent: ログあり、接続は未確認"; grep -i newrelic <<<"$LOG" | tail -3 | cut -c1-160 | sed 's/^/   /'
else echo "agent: NewRelic のログが出ていない。コード差分が入っているか、env が読めているか確認（systemctl show $UNIT -p EnvironmentFiles）"; fi
EOS
)

FAIL=0
for h in $HOSTS; do
  log "== $h ($UNIT)"
  if remote_bash "$h" "$UNIT" "$REMOTE_ENV" <<<"$REMOTE_SCRIPT" 2>&1 | sed "s/^/  /"; then :; else warn "❌ $h: 失敗"; FAIL=1; fi
done
[ $FAIL -eq 0 ] && log "✅ app: 全台完了。1〜2 分後に verify.sh で endpoints / sql を確認"
exit $FAIL
