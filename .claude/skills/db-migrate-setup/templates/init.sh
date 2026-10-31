#!/usr/bin/env bash
# =====================================================================
# init.sh テンプレート（~/webapp/sql/init.sh を置き換える想定）
# =====================================================================
# 役割: POST /api/initialize から呼ばれ、DB を初期状態に戻す。
# 特徴:
#   - SQL_FILES に書いた順に流す（.sql と .sql.gz に対応）
#   - DB ホストを環境変数で切り替える（DB を別サーバーに分けたとき用）
#   - ファイルごとの所要秒数を stderr に出す（タイムアウト対策）
# 使い方:
#   1. 元の init.sh を読み、環境変数名と SQL ファイル名をこのファイルに写す
#   2. 追加の SQL（80-add-index.sql など）を SQL_FILES の後ろに足す
#   3. time ./init.sh で所要時間を測る
# 注意: このスクリプトは Go アプリと同じサーバーで動く。
#       DB が別サーバーでも、ここから mysql クライアントでリモート接続する。
# =====================================================================
set -euo pipefail
cd "$(dirname "$0")"

# systemd の EnvironmentFile と同じファイルを読む（無ければ既定値）
if [ -f /home/isucon/env.sh ]; then
  # shellcheck disable=SC1091
  . /home/isucon/env.sh
fi

# 環境変数名は回ごとに違う。元の init.sh に合わせて書き換えること。
#   isucon14: ISUCON_DB_HOST など / isucon13: ISUCON13_MYSQL_DIALCONFIG_ADDRESS など
DB_HOST=${ISUCON_DB_HOST:-127.0.0.1}
DB_PORT=${ISUCON_DB_PORT:-3306}
DB_USER=${ISUCON_DB_USER:-isucon}
DB_PASSWORD=${ISUCON_DB_PASSWORD:-isucon}
DB_NAME=${ISUCON_DB_NAME:-isuride}

# 流す順番に並べる。初期データの後ろに index・再計算を置く。
SQL_FILES=(
  1-schema.sql
  2-master-data.sql
  3-initial-data.sql.gz
  80-add-index.sql
  90-recompute.sql
)

mysql_exec() {
  mysql -u"$DB_USER" -p"$DB_PASSWORD" \
    --host "$DB_HOST" --port "$DB_PORT" \
    "$DB_NAME"
}

run_sql() {
  local f=$1
  local start=$SECONDS
  if [ ! -f "$f" ]; then
    echo "[init.sh] ERROR: $f が見つからない" >&2
    exit 1
  fi
  case "$f" in
    *.gz) gzip -dc "$f" | mysql_exec ;;
    *)    mysql_exec < "$f" ;;
  esac
  echo "[init.sh] $f: $((SECONDS - start))s (host=$DB_HOST)" >&2
}

total_start=$SECONDS
for f in "${SQL_FILES[@]}"; do
  run_sql "$f"
done

# ---------------------------------------------------------------------
# 画像を DB の外に出した場合: 競技中に増えたファイルを消し、初期状態に戻す
#   IMAGE_INIT_DIR は export-images.sh で作った初期スナップショット
# ---------------------------------------------------------------------
IMAGE_DIR=${IMAGE_DIR:-}
IMAGE_INIT_DIR=${IMAGE_INIT_DIR:-}
if [ -n "$IMAGE_DIR" ] && [ -n "$IMAGE_INIT_DIR" ]; then
  start=$SECONDS
  # 事故防止: /home/isucon 配下の深い階層以外は消さない
  case "$IMAGE_DIR" in
    /home/isucon/?*/?*) ;;
    *) echo "[init.sh] IMAGE_DIR が想定外の場所なので中止: $IMAGE_DIR" >&2; exit 1 ;;
  esac
  [ -d "$IMAGE_INIT_DIR" ] || { echo "[init.sh] IMAGE_INIT_DIR が無い: $IMAGE_INIT_DIR" >&2; exit 1; }
  rm -rf "$IMAGE_DIR"
  cp -a "$IMAGE_INIT_DIR" "$IMAGE_DIR"
  echo "[init.sh] images reset: $((SECONDS - start))s" >&2
fi

echo "[init.sh] total: $((SECONDS - total_start))s" >&2
