#!/usr/bin/env bash
# ISUCON2026: 各サーバーに NewRelic infra agent を入れて起動する（CPU・メモリ・プロセス別の計測）
# 使い方: install-infra.sh [ホスト...（既定 ISU_HOSTS）]
# 前提: push-env.sh で /home/isucon/newrelic.env を置いてある（ライセンスキーはそこから読む）
# 2 回目以降は差分だけ反映する（apt は入っていれば飛ばす、設定が同じなら再起動しない）
# 出典: https://docs.newrelic.com/docs/infrastructure/infrastructure-agent/linux-installation/package-manager-install/
set -euo pipefail
. "$(dirname "$0")/lib.sh"
HOSTS=$(hosts_or_default "$@")

REMOTE_SCRIPT=$(cat <<'EOS'
set -euo pipefail
DISPLAY_NAME="$1"; REMOTE_ENV="$2"; CONF="$3"
[ -f "$REMOTE_ENV" ] || { echo "❌ $REMOTE_ENV が無い。先に push-env.sh"; exit 1; }
LK=$(grep '^NEW_RELIC_LICENSE_KEY=' "$REMOTE_ENV" | cut -d= -f2-)
[ "${#LK}" = 40 ] || { echo "❌ ライセンスキーの形式が不正（長さ ${#LK}）"; exit 1; }
CODENAME=$(. /etc/os-release; echo "${VERSION_CODENAME:-noble}")

if dpkg -s newrelic-infra >/dev/null 2>&1; then
  echo "apt: newrelic-infra は導入済み（$(dpkg-query -W -f='${Version}' newrelic-infra)）"
else
  curl -fsSL https://download.newrelic.com/infrastructure_agent/gpg/newrelic-infra.gpg | sudo gpg --dearmor --yes -o /etc/apt/trusted.gpg.d/newrelic-infra.gpg
  echo "deb https://download.newrelic.com/infrastructure_agent/linux/apt/ $CODENAME main" | sudo tee /etc/apt/sources.list.d/newrelic-infra.list >/dev/null
  sudo apt-get update -qq -o Dir::Etc::sourcelist=/etc/apt/sources.list.d/newrelic-infra.list -o Dir::Etc::sourceparts=- 2>&1 | tail -2 || true
  sudo DEBIAN_FRONTEND=noninteractive apt-get install -y -qq newrelic-infra 2>&1 | tail -2
  echo "apt: newrelic-infra を導入（$(dpkg-query -W -f='${Version}' newrelic-infra)、${CODENAME}）"
fi

NEW_CONF=$(printf 'license_key: %s\ndisplay_name: %s\nenable_process_metrics: true\nmetrics_process_sample_rate: 20\nlog:\n  level: info\n' "$LK" "$DISPLAY_NAME")
if [ "$(sudo cat "$CONF" 2>/dev/null || true)" = "$NEW_CONF" ]; then
  echo "conf: 変更なし"
  sudo systemctl enable --now newrelic-infra >/dev/null 2>&1
else
  printf '%s\n' "$NEW_CONF" | sudo tee "$CONF" >/dev/null
  sudo chmod 600 "$CONF"
  sudo systemctl enable newrelic-infra >/dev/null 2>&1
  sudo systemctl restart newrelic-infra
  echo "conf: 書き換えて再起動（display_name=$DISPLAY_NAME, process metrics ON）"
fi
sleep 2
echo "unit: active=$(systemctl is-active newrelic-infra) enabled=$(systemctl is-enabled newrelic-infra)"
if sudo journalctl -u newrelic-infra --since '-2min' --no-pager -o cat 2>/dev/null | grep -q 'connect got id'; then echo "agent: NewRelic に接続済み"; else echo "agent: 接続ログ待ち（verify.sh で確認）"; fi
EOS
)

FAIL=0
for h in $HOSTS; do
  log "== $h"
  if remote_bash "$h" "$h" "$REMOTE_ENV" "$INFRA_CONF" <<<"$REMOTE_SCRIPT" 2>&1 | sed "s/^/  /"; then :; else warn "❌ $h: 失敗"; FAIL=1; fi
done
[ $FAIL -eq 0 ] && log "✅ infra agent: 全台完了。1 分後に verify.sh で hosts / procs を確認"
exit $FAIL
