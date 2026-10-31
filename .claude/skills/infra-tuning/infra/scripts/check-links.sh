#!/usr/bin/env bash
# サーバー間の疎通を一覧で確認する（どの台からでも実行できる。TCP 接続できるかだけを見る）。
#   ./check-links.sh <host:port>...
#   例: ./check-links.sh 192.168.0.12:8080 192.168.0.13:3306 192.168.0.13:11211
#   引数なしのときは ISU_LINKS（スペース区切り）を使う
#
# NG のときに見るところ: 相手側の待ち受け（ss -ltn）、bind-address（MySQL は 127.0.0.1 が既定）、セキュリティグループ
set -uo pipefail
# shellcheck source=lib.sh
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"

targets=("$@")
# shellcheck disable=SC2206
[[ ${#targets[@]} -eq 0 && -n ${ISU_LINKS:-} ]] && targets=($ISU_LINKS)
[[ ${#targets[@]} -gt 0 ]] || { sed -n '2,6p' "$0"; exit 1; }

ng=0
printf '%-28s %s\n' "target" "result"
for t in "${targets[@]}"; do
  host="${t%:*}"; port="${t##*:}"
  if timeout 3 bash -c "exec 3<>/dev/tcp/$host/$port" 2>/dev/null; then
    printf '%-28s \033[32mOK\033[0m\n' "$t"
  else
    printf '%-28s \033[31mNG\033[0m\n' "$t"; ng=1
  fi
done
[[ $ng -eq 0 ]] && log "全部つながる" || warn "NG がある。相手側で ss -ltn と bind-address、セキュリティグループを確認"
exit $ng
