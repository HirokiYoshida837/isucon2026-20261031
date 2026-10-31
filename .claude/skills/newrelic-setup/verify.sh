#!/usr/bin/env bash
# ISUCON2026: NewRelic の疎通を確認する（各サーバーの agent 状態 + NRQL でデータが来ているか）
# 使い方: verify.sh [直近の分数（既定 5）] [ホスト...（既定 ISU_HOSTS）]
set -uo pipefail
. "$(dirname "$0")/lib.sh"
MIN="${1:-5}"; shift || true
[[ "$MIN" =~ ^[0-9]+$ ]] || die "分は数字で"
HOSTS=$(hosts_or_default "$@")
UNIT="${ISU_APP_UNIT%.service}"

echo "## サーバー側"
for h in $HOSTS; do
  ssh "${SSH_OPTS[@]}" "$h" "
    printf '%-12s infra=%s' '$h' \"\$(systemctl is-active newrelic-infra 2>/dev/null || echo none)\"
    if [ -n '$UNIT' ]; then
      printf ' app(%s)=%s' '$UNIT' \"\$(systemctl is-active '$UNIT' 2>/dev/null || echo none)\"
      since=\$(systemctl show '$UNIT' -p ActiveEnterTimestamp --value 2>/dev/null); [ -n \"\$since\" ] || since='-30min'
      if sudo journalctl -u '$UNIT' --since \"\$since\" --no-pager -o cat 2>/dev/null | grep -q 'application connected'; then printf ' apm=connected'; else printf ' apm=未接続'; fi
    fi
    printf ' env=%s\n' \"\$([ -f $REMOTE_ENV ] && grep -o '^NEW_RELIC_ENABLED=.*' $REMOTE_ENV || echo none)\"
  " 2>/dev/null || echo "$h   ssh 失敗"
done

echo
echo "## NewRelic（直近 ${MIN} 分、app=${NEW_RELIC_APP_NAME}）"
[ -x "$NRQL" ] || die "nrql.sh が無い: $NRQL"
export NEW_RELIC_APP_NAME
show() { # $1=見出し $2=プリセット $3=表示行数
  echo "### $1"
  out=$("$NRQL" --table "$2" "$MIN" 2>&1 | grep -v '^# NRQL') || true
  if [ -z "$out" ] || [ "$out" = "(データなし)" ]; then echo "  (データなし)"; else echo "$out" | head -"$3" | sed 's/^/  /' | cut -c1-160; fi
}
show "hosts（CPU / メモリ）" hosts 6
show "procs（プロセス別 CPU 上位）" procs 8
show "endpoints（合計時間順）" endpoints 8
show "sql（合計時間順）" sql 8
cat <<EOF

## 空だったとき
- hosts / procs が空: infra agent 未起動（install-infra.sh）か display_name 未設定
- endpoints が空: アプリが NewRelic に未接続（app-dropin.sh の agent 行を見る）か NEW_RELIC_APP_NAME が違う
- sql が空: ドライバが nrmysql になっていない、またはクエリが Context なし（go-agent-snippet.md）
- 全部空: データ到着まで 1〜2 分かかる。少し待って再実行
EOF
