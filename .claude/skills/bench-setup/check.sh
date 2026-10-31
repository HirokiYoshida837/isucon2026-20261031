#!/usr/bin/env bash
# ISUCON2026: bench を作る前の事前チェック（読み取りだけ。サーバーにも手元にも書き込まない）
# 使い方: check.sh [ホスト...（既定 ISU_HOSTS）]
# 出力: 1 行 1 項目で OK / WARN / NG。NG が 1 つでもあれば exit 1
set -uo pipefail
ISU_ENV_FILE="${ISU_ENV_FILE:-$HOME/.config/isucon/isucon.env}"
DISCORD_ENV_FILE="${DISCORD_ENV_FILE:-$HOME/.config/isucon/discord.env}"
NR_ENV_FILE="$HOME/.config/isucon/newrelic.env"
[ -f "$ISU_ENV_FILE" ] && { set -a; . "$ISU_ENV_FILE"; set +a; }
: "${ISU_HOSTS:=isu1 isu2 isu3}"
: "${ISU_REMOTE_DIR:=/home/isucon/webapp}"
: "${ISU_DEPLOY_CMD:=make deploy}"
: "${ISU_BEFORE_CMD:=}"
: "${ISU_AFTER_CMD:=}"
HOSTS="${*:-$ISU_HOSTS}"
SKILLS_DIR="$(cd "$(dirname "$0")/.." && pwd)"
SSH_OPTS=(-o ConnectTimeout=10 -o BatchMode=yes)
NG=0
say() { printf '%-4s %-8s %s\n' "$1" "$2" "$3"; [ "$1" = NG ] && NG=$((NG + 1)); return 0; }

# 手元
[ -f "$ISU_ENV_FILE" ] && say OK local "isucon.env あり" || say NG local "isucon.env が無い（${ISU_ENV_FILE}）"
say INFO local "ISU_HOSTS=$ISU_HOSTS / ISU_REMOTE_DIR=$ISU_REMOTE_DIR"
say INFO local "ISU_DEPLOY_CMD=$ISU_DEPLOY_CMD"
case "$ISU_DEPLOY_CMD" in
  "bash -ec "*) ;;
  *'&&'*|*';'*) say NG local "ISU_DEPLOY_CMD が複数コマンドなのに bash -ec で包んでいない（REF が後ろに渡らず、偽の成功になる）" ;;
esac
[ -n "$ISU_BEFORE_CMD" ] && say OK local "ISU_BEFORE_CMD あり" || say WARN local "ISU_BEFORE_CMD が空（リソース記録が始まらない）"
[ -n "$ISU_AFTER_CMD" ] && say OK local "ISU_AFTER_CMD あり" || say WARN local "ISU_AFTER_CMD が空（集計が添付されない）"
grep -qE '^[[:space:]]*(export[[:space:]]+)?DISCORD_WEBHOOK_URL=["'\'']?https://' "$DISCORD_ENV_FILE" 2>/dev/null \
  && say OK local "discord.env に Webhook あり" || say NG local "discord.env に DISCORD_WEBHOOK_URL が無い（${DISCORD_ENV_FILE}）"
for c in jq curl; do command -v "$c" >/dev/null && say OK local "$c あり" || say NG local "$c が無い"; done
[ -f "$NR_ENV_FILE" ] && say OK local "newrelic.env あり" || say WARN local "newrelic.env が無い（bench-report の NewRelic 欄が空になる）"
[ -x "$SKILLS_DIR/newrelic-nrql/nrql.sh" ] && say OK local "nrql.sh あり" || say WARN local "newrelic-nrql/nrql.sh が無い"

# 各ホスト（1 台 1 回の ssh で全部見る）
for h in $HOSTS; do
  if ! ssh "${SSH_OPTS[@]}" "$h" true >/dev/null 2>&1; then
    say NG "$h" "ssh できない（~/.ssh/config と ISU_HOSTS を確認）"
    continue
  fi
  # 最終行の END が来なければ、途中で切れたとみなす（0 行で「NG なし」になるのを防ぐ）
  done_seen=0
  while read -r st msg; do
    case "$st" in
      END) done_seen=1 ;;
      ?*) say "$st" "$h" "$msg" ;;
    esac
  done < <(ssh "${SSH_OPTS[@]}" "$h" "bash -s -- '$ISU_REMOTE_DIR' '${ISU_INFRA_REMOTE_DIR:-/home/isucon/infra}'" 2>/dev/null <<'EOF'
dir=$1; infra=$2
export GIT_OPTIONAL_LOCKS=0  # git status が .git/index を書き換えないようにする（読み取りだけにするため）
if ! cd "$dir" 2>/dev/null; then echo "NG $dir が無い"; echo END; exit 0; fi
if git rev-parse --git-dir >/dev/null 2>&1; then
  echo "OK git $(git log -1 --format='%h' 2>/dev/null) on $(git branch --show-current)"
  git remote get-url origin >/dev/null 2>&1 && echo "OK origin あり" || echo "NG origin が無い"
  [ -z "$(git status --porcelain --untracked-files=no)" ] && echo "OK 追跡ファイルの変更なし" || echo "WARN 追跡ファイルに変更あり（deploy の --force で消える）"
else
  echo "NG $dir が git リポジトリではない"
fi
if command -v gh >/dev/null; then
  gh auth status >/dev/null 2>&1 && echo "OK gh ログイン済み" || echo "NG gh が未ログイン（本人が gh auth login）"
else
  echo "NG gh が無い（SKILL gh-setup）"
fi
if [ -f Makefile ]; then
  make -n deploy REF=main >/dev/null 2>&1 && echo "OK make -n deploy が通る" || echo "NG make -n deploy が失敗"
  grep -qE '^[^#]*\$\(REF\)' Makefile && echo "OK Makefile が REF を使う" || echo "NG Makefile の deploy が REF を使っていない（SKILL makefile-setup）"
else
  echo "NG Makefile が無い（SKILL makefile-setup）"
fi
command -v go >/dev/null && echo "OK go: $(command -v go)" || echo "WARN 非対話 ssh で go が見つからない（ISU_DEPLOY_CMD で PATH を足していれば OK）"
for s in bench-before bench-after; do
  [ -x "$infra/scripts/$s.sh" ] && echo "OK $infra/scripts/$s.sh あり" || echo "WARN $infra/scripts/$s.sh が無い（SKILL infra-tuning で配る）"
done
command -v pidstat >/dev/null && echo "OK pidstat あり" || echo "WARN pidstat が無い（infra/scripts/setup-tools.sh）"
echo END
EOF
  )
  [ $done_seen -eq 1 ] || say NG "$h" "チェックが途中で切れた（ssh が落ちた可能性。もう一度実行する）"
done

echo
if [ $NG -gt 0 ]; then echo "NG が $NG 件。直してからもう一度実行する"; exit 1; fi
echo "NG なし。WARN は内容を見て判断する"
