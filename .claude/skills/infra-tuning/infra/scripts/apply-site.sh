#!/usr/bin/env bash
# nginx のサイト設定（人が問題の設定に差分を足したもの）を配置する（nginx の台で実行）。
#   ./apply-site.sh <file>            # sites-available/ に同名で置き、sites-enabled/ にリンク → nginx -t → reload → 疎通確認
#   ./apply-site.sh --dry-run <file>  # 現在の同名ファイルとの差分だけ見る
# upstream keep-alive の断片（/etc/nginx/isucon/proxy_keepalive.conf）も一緒に置く。
# 戻し方: ./rollback.sh -y <バックアップ名>
set -euo pipefail
# shellcheck source=lib.sh
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"

dry=0; [[ ${1:-} == --dry-run ]] && { dry=1; shift; }
site="${1:?サイト設定ファイルを指定}"
[[ -f $site ]] || die "ファイルがありません: $site"
name="$(basename "$site")"; dst="/etc/nginx/sites-available/$name"
require_sudo
log "差分（- が現在、+ が配置後）: $dst"
if as_root test -f "$dst"; then as_root diff -u "$dst" "$site" || true; else sed 's/^/+ /' "$site"; fi
[[ $dry -eq 1 ]] && exit 0
ensure_snapshot
start_backup site
install_file "$INFRA_DIR/nginx/isucon/proxy_keepalive.conf" /etc/nginx/isucon/proxy_keepalive.conf
install_file "$site" "$dst"
if ! as_root test -L "/etc/nginx/sites-enabled/$name"; then
  backup_file "/etc/nginx/sites-enabled/$name"
  as_root ln -sfn "$dst" "/etc/nginx/sites-enabled/$name"
  log "有効化: /etc/nginx/sites-enabled/$name"
fi
log "nginx -t"
if ! as_root nginx -t; then
  warn "nginx -t に失敗。元に戻します"; "$INFRA_DIR/scripts/rollback.sh" -y "$BACKUP_DIR"; exit 1
fi
as_root systemctl reload nginx; sleep 1
health_check || warn "戻すなら ./rollback.sh -y $BACKUP_DIR"
log "完了。バックアップ: $BACKUP_DIR"
