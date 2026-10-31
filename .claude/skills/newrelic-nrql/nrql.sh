#!/usr/bin/env bash
# ISUCON2026: NewRelic NerdGraph に NRQL を投げる（個人アカウント用）
# 使い方:
#   nrql.sh "<NRQL>"            任意の NRQL を実行
#   nrql.sh <preset> [分]       よく使うクエリ（既定は直近 10 分）
#   nrql.sh whoami              キーと接続先の確認
#   NR_SINCE_MS=<ms> NR_UNTIL_MS=<ms> nrql.sh <preset>   期間をエポックミリ秒で指定（分の指定より優先）
#   NEW_RELIC_APP_NAME=<appName> nrql.sh <preset>        アプリで絞る（sql / external は必須。env ファイルに書いてもよい）
#   NEW_RELIC_APP_ID=<appId>                             sql / external の appId 解決を省略したいとき
# 出力は JSON（結果配列）。--table を付けると TSV で出す。
set -euo pipefail

ENV_FILE="${NEW_RELIC_ENV_FILE:-$HOME/.config/isucon/newrelic.env}"
[ -f "$ENV_FILE" ] || { echo "設定ファイルがない: $ENV_FILE" >&2; exit 1; }
PRE_APP_NAME="${NEW_RELIC_APP_NAME:-}"; PRE_APP_ID="${NEW_RELIC_APP_ID:-}"
# shellcheck disable=SC1090
set -a; . "$ENV_FILE"; set +a
# 実行時に渡した値を env ファイルより優先する
[ -n "$PRE_APP_NAME" ] && NEW_RELIC_APP_NAME="$PRE_APP_NAME"
[ -n "$PRE_APP_ID" ] && NEW_RELIC_APP_ID="$PRE_APP_ID"
: "${NEW_RELIC_ACCOUNT_ID:?NEW_RELIC_ACCOUNT_ID が未設定}"
[ -n "${NEW_RELIC_USER_KEY:-}" ] || { echo "NEW_RELIC_USER_KEY が空。$ENV_FILE に User API キーを入れて" >&2; exit 1; }
ENDPOINT="${NEW_RELIC_GRAPHQL_URL:-https://api.newrelic.com/graphql}"

TABLE=0
if [ "${1:-}" = "--table" ]; then TABLE=1; shift; fi
ARG="${1:-}"; MIN="${2:-10}"
[ -n "$ARG" ] || { sed -n 2,7p "$0" >&2; exit 1; }
[[ "$MIN" =~ ^[0-9]+$ ]] || { echo "分は数字で指定して" >&2; exit 1; }
# アプリ名の条件（Transaction / TransactionError 用）。未指定なら全アプリ
APP_COND="appName IS NOT NULL"
[ -n "${NEW_RELIC_APP_NAME:-}" ] && APP_COND="appName = '${NEW_RELIC_APP_NAME}'"
APP_FILTER="WHERE $APP_COND"
SINCE="SINCE ${MIN} minutes ago"
# 期間をぴったり指定したいとき（ベンチの開始〜完了など）は、エポックミリ秒で NR_SINCE_MS / NR_UNTIL_MS を渡す
if [[ "${NR_SINCE_MS:-}" =~ ^[0-9]+$ ]]; then
  SINCE="SINCE ${NR_SINCE_MS}"
  [[ "${NR_UNTIL_MS:-}" =~ ^[0-9]+$ ]] && SINCE="$SINCE UNTIL ${NR_UNTIL_MS}"
fi

# Metric のタイムスライス（sql / external）は appName では引けないので appId で絞る（2026-10-28 に確認）
resolve_app_id() {
  [ -n "${NEW_RELIC_APP_ID:-}" ] && return
  [ -n "${NEW_RELIC_APP_NAME:-}" ] || { echo "sql / external は NEW_RELIC_APP_NAME（または NEW_RELIC_APP_ID）が必要" >&2; exit 1; }
  local body resp
  body=$(jq -n --arg q "domain = 'APM' AND name = '${NEW_RELIC_APP_NAME}'" '{query: "query($q: String!) { actor { entitySearch(query: $q) { results { entities { ... on ApmApplicationEntityOutline { applicationId name } } } } } }", variables: {q: $q}}')
  resp=$(curl -sS --fail-with-body "$ENDPOINT" -H 'Content-Type: application/json' -H "API-Key: $NEW_RELIC_USER_KEY" -d "$body")
  NEW_RELIC_APP_ID=$(jq -r '[.data.actor.entitySearch.results.entities[]? | select(.applicationId != null) | .applicationId] | first // empty' <<<"$resp")
  [ -n "$NEW_RELIC_APP_ID" ] || { echo "appName '${NEW_RELIC_APP_NAME}' の APM エンティティが見つからない（エージェント未接続か名前違い）" >&2; exit 1; }
}

case "$ARG" in
  whoami)
    BODY=$(jq -n '{query: "{ actor { user { name email } } }"}')
    curl -sS --fail-with-body "$ENDPOINT" -H 'Content-Type: application/json' -H "API-Key: $NEW_RELIC_USER_KEY" -d "$BODY" \
      | jq '{user: .data.actor.user, errors: .errors}'
    exit 0 ;;
  # エンドポイント別: 合計時間が大きい順（どこを直すと効くか）
  endpoints) Q="SELECT sum(duration) AS 'total_s', count(*) AS 'calls', average(duration)*1000 AS 'avg_ms', percentile(duration, 95)*1000 AS 'p95_ms', max(duration)*1000 AS 'max_ms' FROM Transaction $APP_FILTER FACET name $SINCE LIMIT 30" ;;
  # SQL 別: 合計時間が大きい順（index / N+1 の当たりをつける）
  sql) resolve_app_id; Q="SELECT sum(newrelic.timeslice.value) AS 'total_s', count(newrelic.timeslice.value) AS 'calls', average(newrelic.timeslice.value)*1000 AS 'avg_ms' FROM Metric WHERE appId = $NEW_RELIC_APP_ID AND metricTimesliceName LIKE 'Datastore/statement/%' FACET metricTimesliceName $SINCE LIMIT 30" ;;
  # 外部 HTTP 呼び出し別
  external) resolve_app_id; Q="SELECT sum(newrelic.timeslice.value) AS 'total_s', count(newrelic.timeslice.value) AS 'calls', average(newrelic.timeslice.value)*1000 AS 'avg_ms' FROM Metric WHERE appId = $NEW_RELIC_APP_ID AND metricTimesliceName LIKE 'External/%/all' FACET metricTimesliceName $SINCE LIMIT 30" ;;
  # エラー
  errors) Q="SELECT count(*) FROM TransactionError $APP_FILTER FACET transactionName, error.message $SINCE LIMIT 30" ;;
  # サーバー別リソース（p90 / p95 / max）
  hosts) Q="SELECT percentile(cpuPercent, 90, 95) AS 'cpu', max(cpuPercent) AS 'cpu_max', percentile(memoryUsedPercent, 95) AS 'mem_p95', max(memoryUsedPercent) AS 'mem_max', percentile(loadAverageOneMinute, 95) AS 'load1_p95' FROM SystemSample FACET entityName $SINCE" ;;
  # プロセス別 CPU（どのミドルウェアが重いか）
  procs) Q="SELECT average(cpuPercent) AS 'cpu_avg', max(cpuPercent) AS 'cpu_max', average(memoryResidentSizeBytes)/1e6 AS 'rss_mb' FROM ProcessSample FACET entityName, processDisplayName $SINCE LIMIT 30" ;;
  *) Q="$ARG" ;;
esac

BODY=$(jq -n --argjson acct "$NEW_RELIC_ACCOUNT_ID" --arg q "$Q" \
  '{query: "query($acct: Int!, $q: Nrql!) { actor { account(id: $acct) { nrql(query: $q, timeout: 60) { results } } } }", variables: {acct: $acct, q: $q}}')
RESP=$(curl -sS --fail-with-body "$ENDPOINT" -H 'Content-Type: application/json' -H "API-Key: $NEW_RELIC_USER_KEY" -d "$BODY")

if [ "$(jq '.errors | length // 0' <<<"$RESP")" != "0" ]; then
  jq '.errors' <<<"$RESP" >&2; exit 1
fi
echo "# NRQL: $Q" >&2
if [ "$TABLE" = 1 ]; then
  jq -r '.data.actor.account.nrql.results as $r | if ($r | length) == 0 then "(データなし)" else (($r[0] | keys_unsorted) as $k | ($k | @tsv), ($r[] | [ $k[] as $x | (.[$x] | if type=="object" or type=="array" then tojson elif type=="number" then (.*1000|round/1000) else . end) ] | @tsv)) end' <<<"$RESP"
else
  jq '.data.actor.account.nrql.results' <<<"$RESP"
fi
