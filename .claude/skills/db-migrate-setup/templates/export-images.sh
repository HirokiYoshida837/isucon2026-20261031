#!/usr/bin/env bash
# =====================================================================
# DB の画像（BLOB）をファイルに書き出す
# =====================================================================
# 用途: 画像を nginx から直接返すための初回書き出し。
# 出力: $OUT_DIR/<KEY_EXPR の値><SUFFIX>
# 方式: TO_BASE64 で文字列にして受け取り、base64 -d で戻す。
#       （mysql の出力をそのままファイルにすると改行やタブで壊れるため）
# 変数（SQL の式をそのまま書く。予約語の列名はバッククォートで囲む）:
#   FROM      FROM 句（JOIN も可）
#   KEY_EXPR  ファイル名にする式（URL に出てくる値に合わせる）
#   BLOB_EXPR 画像の列
#   SUFFIX    ファイル名の末尾（拡張子が行ごとに違うなら空にして KEY_EXPR で作る）
#   WHERE     絞り込み条件（同じキーに複数行あるなら最新だけに絞る）
#   OUT_DIR   出力先（初期スナップショットとして残す場所）
# 例 1: private-isu（/image/<id>.<ext>、拡張子は mime 列で分岐）
#   FROM=posts SUFFIX= \
#   KEY_EXPR="CONCAT(id, CASE mime WHEN 'image/png' THEN '.png' WHEN 'image/gif' THEN '.gif' ELSE '.jpg' END)" \
#   BLOB_EXPR=imgdata OUT_DIR=/home/isucon/private_isu/webapp/public/image_init ./export-images.sh
# 例 2: isucon11（isu ごとの画像、キーは jia_isu_uuid）
#   FROM=isu KEY_EXPR=jia_isu_uuid BLOB_EXPR=image OUT_DIR=/home/isucon/webapp/public/isu_image_init ./export-images.sh
# 前提: nginx と同じサーバーで実行する（ファイルは nginx が読む）。
# =====================================================================
set -euo pipefail

if [ -f /home/isucon/env.sh ]; then
  # shellcheck disable=SC1091
  . /home/isucon/env.sh
fi

DB_HOST=${ISUCON_DB_HOST:-127.0.0.1}
DB_PORT=${ISUCON_DB_PORT:-3306}
DB_USER=${ISUCON_DB_USER:-isucon}
DB_PASSWORD=${ISUCON_DB_PASSWORD:-isucon}
DB_NAME=${ISUCON_DB_NAME:-isupipe}

FROM=${FROM:-icons}
KEY_EXPR=${KEY_EXPR:-user_id}
BLOB_EXPR=${BLOB_EXPR:-image}
SUFFIX=${SUFFIX-.jpg}
WHERE=${WHERE:-1=1}
OUT_DIR=${OUT_DIR:-/home/isucon/webapp/public/icons_init}

mkdir -p "$OUT_DIR"

# TO_BASE64 は 76 文字ごとに改行を入れるので CHAR(10) を取り除く
query="SELECT $KEY_EXPR, REPLACE(TO_BASE64($BLOB_EXPR), CHAR(10), '')
FROM $FROM
WHERE $BLOB_EXPR IS NOT NULL AND ($WHERE)"

start=$SECONDS
mysql -u"$DB_USER" -p"$DB_PASSWORD" --host "$DB_HOST" --port "$DB_PORT" \
  --batch --skip-column-names --quick --max-allowed-packet=1G \
  "$DB_NAME" -e "$query" |
while IFS=$'\t' read -r key b64; do
  name="$key$SUFFIX"
  tmp="$OUT_DIR/.$name.tmp"
  printf '%s' "$b64" | base64 -d > "$tmp"
  mv "$tmp" "$OUT_DIR/$name"
done

count=$(find "$OUT_DIR" -maxdepth 1 -type f ! -name '.*' | wc -l)
echo "[export-images] $count files in $OUT_DIR ($((SECONDS - start))s)" >&2
