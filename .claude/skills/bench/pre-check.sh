#!/usr/bin/env bash
# ISUCON2026: ポータルで Enqueue を押す直前の確認（#210 #211 #24070）
# 見るもの: ① app ホストで imgextract が走っていない ② DB で 30 秒超のトランザクションが無い ③ DB に INVALID な index が無い ④ 全ホストの HEAD が揃っている
# 1 つでも ❌ なら Enqueue せず原因を見る（imgextract の長い tx が初期化の TRUNCATE / CREATE INDEX を止めて 499・500 → FAIL になった）
# 使い方: pre-check.sh   → 全部 ✅ なら exit 0、❌ があれば exit 1
set -uo pipefail
. "$(dirname "$0")/lib.sh"
: "${ISU_DB_HOST:=asia2}"                        # PostgreSQL のホスト（~/.ssh/config の Host 名）
: "${ISU_DB_NAME:=isureal}"
: "${ISU_LONG_TX_SEC:=30}"
ng=0
# ① imgextract（cmd/imgextract。asia2 の image_objects を全件読み、tx を数分開く）
for h in $ISU_HOSTS; do
  out=$(ssh "${SSH_OPTS[@]}" "$h" 'ps -eo pid,etimes,args | grep -F "[i]mgextract" | grep -v "grep -F"' 2>/dev/null)
  if [ -n "$out" ]; then echo "❌ $h: imgextract が実行中（終わるのを待つか止める）"; echo "$out" | sed 's/^/     /'; ng=1; else echo "✅ $h: imgextract なし"; fi
done
# ② 長いトランザクション
out=$(ssh "${SSH_OPTS[@]}" "$ISU_DB_HOST" "sudo -u postgres psql '$ISU_DB_NAME' -X -At -F' | ' -c \"select pid, coalesce(client_addr::text,'local'), (now()-xact_start)::interval(0), left(query,70) from pg_stat_activity where backend_type='client backend' and xact_start < now() - interval '$ISU_LONG_TX_SEC seconds'\"" 2>&1)
if [ -n "$out" ]; then echo "❌ $ISU_DB_HOST: ${ISU_LONG_TX_SEC} 秒超のトランザクション"; echo "$out" | sed 's/^/     /'; ng=1; else echo "✅ $ISU_DB_HOST: ${ISU_LONG_TX_SEC} 秒超のトランザクションなし"; fi
# ③ INVALID index（CREATE INDEX CONCURRENTLY が初期化とぶつかると残る。init.sh の IF NOT EXISTS では直らない）
out=$(ssh "${SSH_OPTS[@]}" "$ISU_DB_HOST" "sudo -u postgres psql '$ISU_DB_NAME' -X -At -c \"select indexrelid::regclass from pg_index where not indisvalid\"" 2>&1)
if [ -n "$out" ]; then echo "❌ $ISU_DB_HOST: INVALID な index（REINDEX INDEX CONCURRENTLY <名前> で作り直す）"; echo "$out" | sed 's/^/     /'; ng=1; else echo "✅ $ISU_DB_HOST: INVALID な index なし"; fi
# ④ HEAD が揃っているか（deploy と Enqueue の間に他の人がデプロイすることがある）
heads=""
for h in $ISU_HOSTS; do
  c=$(ssh "${SSH_OPTS[@]}" "$h" "cd '$ISU_REMOTE_DIR' && git log -1 --format='%h %s'" 2>/dev/null || echo "取得できず")
  echo "   $h: $c"; heads+="$c"$'\n'
done
if [ "$(printf '%s' "$heads" | sort -u | wc -l | tr -d ' ')" = "1" ]; then echo "✅ HEAD は全ホスト同じ"; else echo "❌ HEAD が揃っていない"; ng=1; fi
[ $ng = 0 ] && echo "→ Enqueue してよい" || echo "→ Enqueue しない。原因を見る"
exit $ng
