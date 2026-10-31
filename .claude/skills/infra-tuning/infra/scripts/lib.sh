#!/usr/bin/env bash
# 共通関数。各スクリプトから source して使う（単体では実行しない）。
# shellcheck disable=SC2034  # 呼び出し側で使う変数を定義している

# infra/ ディレクトリの絶対パス（このファイルの 1 つ上）。
INFRA_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

# 設定ファイルのバックアップ置き場。apply-*.sh が 1 回ごとにサブディレクトリを作る。
BACKUP_ROOT="${BACKUP_ROOT:-/home/isucon/infra-backup}"
# 初手スナップショット（触る前の /etc 一式）。snapshot.sh が作り、restore-all.sh が戻す。
SNAPSHOT_DIR="$BACKUP_ROOT/snapshot"
# opt-in で作った MySQL ユーザーの記録（restore-all.sh が消す）。
CREATED_USERS_LIST="$BACKUP_ROOT/created-users.list"

# ベンチ結果の置き場。
BENCH_LOG_ROOT="${BENCH_LOG_ROOT:-/home/isucon/bench-logs}"

# alp 用の JSON アクセスログ。問題側の access.log は触らず、別ファイルに追加出力する。
NGINX_ACCESS_LOG="${NGINX_ACCESS_LOG:-/var/log/nginx/access.json.log}"
NGINX_TUNING_CONF="${NGINX_TUNING_CONF:-/etc/nginx/conf.d/99-isucon-tuning.conf}"
MYSQL_SLOW_LOG="${MYSQL_SLOW_LOG:-/var/log/mysql/mysql-slow.log}"
MYSQL_CONF_DIR="${MYSQL_CONF_DIR:-/etc/mysql/conf.d}"

# apply 後の疎通確認先。200 かつ body が空でないことを見る（DB に繋がらないと 200 で空になる問題がある）。
CHECK_URL="${ISU_CHECK_URL:-http://localhost/}"

log()  { printf '\033[36m[%s]\033[0m %s\n' "$(date +%H:%M:%S)" "$*" >&2; }
warn() { printf '\033[33m[WARN]\033[0m %s\n' "$*" >&2; }
die()  { printf '\033[31m[ERROR]\033[0m %s\n' "$*" >&2; exit 1; }

has_cmd() { command -v "$1" >/dev/null 2>&1; }

# systemd にユニットが存在するか（起動していなくてもよい）。
has_unit() { systemctl list-unit-files "$1.service" --no-legend 2>/dev/null | grep -q "^$1\.service"; }

# sudo を事前に確認する（パスワードが要る環境で途中停止しないように）。
require_sudo() {
  if [[ $EUID -ne 0 ]]; then
    sudo -v || die "sudo が使えません"
  fi
}

# root ならそのまま、そうでなければ sudo を付けて実行する。
as_root() {
  if [[ $EUID -eq 0 ]]; then "$@"; else sudo "$@"; fi
}

# MySQL か MariaDB かを判定して echo する（mysql / mariadb / none）。
detect_db_flavor() {
  local v=""
  if has_cmd mariadbd; then v="$(mariadbd --version 2>/dev/null || true)"
  elif has_cmd mysqld; then v="$(mysqld --version 2>/dev/null || true)"
  elif [[ -x /usr/sbin/mysqld ]]; then v="$(/usr/sbin/mysqld --version 2>/dev/null || true)"
  fi
  if [[ -z $v ]]; then echo none
  elif [[ $v == *MariaDB* ]]; then echo mariadb
  else echo mysql
  fi
}

# DB のサービス名（mysql / mariadb）を echo する。
detect_db_unit() {
  if has_unit mysql; then echo mysql
  elif has_unit mariadb; then echo mariadb
  else echo ""
  fi
}

# root で mysql クライアントを実行する（Ubuntu は root が auth_socket なのでパスワード不要）。
mysql_root() { as_root mysql -uroot "$@"; }

# 初手スナップショットが無ければ取る。apply-*.sh の最初で呼ぶ。
ensure_snapshot() {
  if as_root test -d "$SNAPSHOT_DIR"; then return 0; fi
  log "初手スナップショットが無いので先に取る（${SNAPSHOT_DIR}）"
  bash "$INFRA_DIR/scripts/snapshot.sh"
}

# バックアップ用ディレクトリを 1 つ作り、パスを BACKUP_DIR に入れる。
# 中身: 置き換え前のファイルを元のパスのまま保存。新規作成したファイルは created.list に記録。
start_backup() {
  local name="$1"
  BACKUP_DIR="$BACKUP_ROOT/$(date +%Y%m%d-%H%M%S)-$name"
  as_root mkdir -p "$BACKUP_DIR"
  log "バックアップ先: $BACKUP_DIR"
}

# 置き換える前に呼ぶ。既存なら退避し、無ければ「新規作成」として記録する。
backup_file() {
  local path="$1"
  if as_root test -e "$path"; then
    as_root mkdir -p "$BACKUP_DIR$(dirname "$path")"
    as_root cp -a "$path" "$BACKUP_DIR$path"
  else
    echo "$path" | as_root tee -a "$BACKUP_DIR/created.list" >/dev/null
  fi
}

# ファイルを配置する（バックアップしてからコピー）。内容が同じなら何もしない。
install_file() {
  local src="$1" dst="$2" mode="${3:-0644}"
  if as_root test -f "$dst" && as_root cmp -s "$src" "$dst"; then
    log "変更なし: $dst"
    return 0
  fi
  backup_file "$dst"
  as_root mkdir -p "$(dirname "$dst")"
  as_root install -m "$mode" -o root -g root "$src" "$dst"
  log "配置: $dst"
}

# 既存ファイルの「directive value;」の行を、値だけ書き換える（構成は変えず数値だけ変える）。
# 使い方: set_directive <file> <directive> <value>  → 行があれば書き換えて 0、無ければ 1 を返す。
set_directive() {
  local file="$1" key="$2" value="$3"
  if as_root grep -qE "^[[:space:]]*$key[[:space:]]" "$file"; then
    as_root sed -i -E "s|^([[:space:]]*)$key[[:space:]].*|\1$key $value;|" "$file"
    log "数値変更: $file の $key → $value"
    return 0
  fi
  return 1
}

# apply 後の疎通確認。HTTP 200 かつ body が 1 byte 以上なら OK。
# この台に HTTP が無いときは案内だけ出して 0 を返す。NG のときは 1 を返す（呼び出し側で止めるか決める）。
health_check() {
  local out code size
  if ! out=$(curl -s -o /dev/null -w '%{http_code} %{size_download}' --max-time 10 "$CHECK_URL" 2>/dev/null); then
    log "疎通: $CHECK_URL に接続できない。この台に HTTP が無ければ、アプリの台で curl して確認する"
    return 0
  fi
  code="${out% *}"; size="${out#* }"
  if [[ $code == 200 && $size -gt 0 ]]; then
    log "疎通 OK: $CHECK_URL → HTTP $code, $size bytes"
    return 0
  fi
  warn "疎通 NG: $CHECK_URL → HTTP $code, $size bytes（200 で 0 bytes は DB 接続エラーの典型）"
  warn "アプリのログ（journalctl -u <アプリ> -n 20）を見る。戻すなら ./rollback.sh -y ${BACKUP_DIR:-<バックアップ名>}"
  return 1
}
