#!/usr/bin/env bash
# ISUCON2026: NewRelic のライセンスキー（INGEST - LICENSE）を NerdGraph で取り、~/.config/isucon/newrelic.env に保存する
# 使い方: fetch-license-key.sh        （User API キーとアカウント ID が newrelic.env にあること）
# 値は画面に出さない。既に保存済みなら置き換える
set -euo pipefail
. "$(dirname "$0")/lib.sh"
: "${NEW_RELIC_ACCOUNT_ID:?NEW_RELIC_ACCOUNT_ID が未設定（newrelic.env）}"
: "${NEW_RELIC_USER_KEY:?NEW_RELIC_USER_KEY が未設定（newrelic.env）}"

BODY=$(jq -n --argjson acct "$NEW_RELIC_ACCOUNT_ID" '{query: "query($acct: Int!) { actor { apiAccess { keySearch(query: {types: [INGEST], scope: {accountIds: [$acct], ingestTypes: [LICENSE]}}) { keys { name ... on ApiAccessIngestKey { key } } } } } }", variables: {acct: $acct}}')
RESP=$(curl -sS --fail-with-body "$NR_GRAPHQL" -H 'Content-Type: application/json' -H "API-Key: $NEW_RELIC_USER_KEY" -d "$BODY")
[ "$(jq '.errors | length // 0' <<<"$RESP")" = 0 ] || { jq '.errors' <<<"$RESP" >&2; die "NerdGraph がエラーを返した"; }
KEY=$(jq -r '[.data.actor.apiAccess.keySearch.keys[]? | select(.key != null) | .key] | first // empty' <<<"$RESP")
NAME=$(jq -r '[.data.actor.apiAccess.keySearch.keys[]? | select(.key != null) | .name] | first // empty' <<<"$RESP")
[ "${#KEY}" = 40 ] || die "ライセンスキーが取れなかった（長さ ${#KEY}）。アカウント ID と User キーの権限を確認"

touch "$NR_ENV_FILE"; chmod 600 "$NR_ENV_FILE"
if grep -q '^NEW_RELIC_LICENSE_KEY=' "$NR_ENV_FILE"; then
  tmp=$(mktemp); sed "s|^NEW_RELIC_LICENSE_KEY=.*|NEW_RELIC_LICENSE_KEY=$KEY|" "$NR_ENV_FILE" > "$tmp"; cat "$tmp" > "$NR_ENV_FILE"; rm -f "$tmp"
  log "置き換えた: $NR_ENV_FILE の NEW_RELIC_LICENSE_KEY（${NAME}、${KEY:0:4}...${KEY: -4}）"
else
  printf 'NEW_RELIC_LICENSE_KEY=%s\n' "$KEY" >> "$NR_ENV_FILE"
  log "追加した: $NR_ENV_FILE に NEW_RELIC_LICENSE_KEY（${NAME}、${KEY:0:4}...${KEY: -4}）"
fi
grep -q '^NEW_RELIC_APP_NAME=' "$NR_ENV_FILE" || warn "NEW_RELIC_APP_NAME が無い。$NR_ENV_FILE に NEW_RELIC_APP_NAME=<当日のアプリ名> を書く（既定 isucon2026）"
