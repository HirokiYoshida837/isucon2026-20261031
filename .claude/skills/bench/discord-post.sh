#!/usr/bin/env bash
# ISUCON2026: Discord Webhook に投稿する
# 使い方:
#   discord-post.sh "本文"                      本文だけ投稿（2000 文字を超えたら自動で添付ファイルに回す）
#   discord-post.sh -f out.txt [-f b.txt] "本文"  ファイルを添付して投稿（最大 10 個）
#   echo "本文" | discord-post.sh -             標準入力を本文にする
# 環境変数: DRY_RUN=1 で送信せず、送る内容を表示するだけにする
set -euo pipefail

ENV_FILE="${DISCORD_ENV_FILE:-$HOME/.config/isucon/discord.env}"
[ -f "$ENV_FILE" ] && { set -a; . "$ENV_FILE"; set +a; }
USERNAME="${DISCORD_USERNAME:-isucon-bot}"

FILES=()
while getopts "f:" opt; do
  case "$opt" in
    f) [ -f "$OPTARG" ] || { echo "添付ファイルが無い: $OPTARG" >&2; exit 1; }; FILES+=("$OPTARG") ;;
    *) sed -n 2,7p "$0" >&2; exit 1 ;;
  esac
done
shift $((OPTIND - 1))
MSG="${1:-}"
[ "$MSG" = "-" ] && MSG="$(cat)"
[ -n "$MSG" ] || [ ${#FILES[@]} -gt 0 ] || { sed -n 2,7p "$0" >&2; exit 1; }

# Discord の本文上限は 2000 文字。超えたら先頭だけ本文に残し、全文は添付する
TMPD="$(mktemp -d)"
trap 'rm -rf "$TMPD"' EXIT
if [ "${#MSG}" -gt 1900 ]; then
  TMP="$TMPD/message.txt"
  printf '%s\n' "$MSG" > "$TMP"
  FILES+=("$TMP")
  MSG="${MSG:0:1800}"$'\n'"…（全文は添付ファイル）"
fi
[ ${#FILES[@]} -le 10 ] || { echo "添付は 10 個まで" >&2; exit 1; }

PAYLOAD=$(jq -n --arg c "$MSG" --arg u "$USERNAME" '{content: $c, username: $u, allowed_mentions: {parse: []}}')

if [ "${DRY_RUN:-0}" = 1 ]; then
  echo "[DRY_RUN] payload: $PAYLOAD"
  for f in ${FILES[@]+"${FILES[@]}"}; do echo "[DRY_RUN] attach: $f ($(wc -c < "$f") bytes)"; done
  exit 0
fi

: "${DISCORD_WEBHOOK_URL:?DISCORD_WEBHOOK_URL が未設定。$ENV_FILE に書いて}"
ARGS=(-sS --fail-with-body -o /dev/null -w '%{http_code}' -F "payload_json=$PAYLOAD")
i=0
for f in ${FILES[@]+"${FILES[@]}"}; do ARGS+=(-F "files[$i]=@$f"); i=$((i + 1)); done
CODE=$(curl "${ARGS[@]}" "$DISCORD_WEBHOOK_URL") || { echo "Discord への投稿に失敗（HTTP ${CODE:-?}）" >&2; exit 1; }
echo "posted (HTTP $CODE)"
