#!/usr/bin/env bash
# 初手スナップショットに丸ごと戻す（秘伝のタレを全部外して、触る前の状態にする）。
#   ./restore-all.sh [アプリのユニット名]     # 確認してから戻す
#   ./restore-all.sh -y [アプリのユニット名]  # 確認なし
#
# 戻すもの: snapshot.sh が退避したディレクトリ・ファイル（manifest.txt に書いてあるもの）
# 戻した後にやること: sysctl 反映、daemon-reload、nginx reload、mysqld restart、opt-in で作った MySQL ユーザーの削除、アプリ再起動、疎通確認
# 【危険】mysqld を restart する。ベンチ中に実行しないこと。
# 【注意】sysctl はスナップショット時点の値を書き戻す。limits（nofile）は新しいログイン・再起動から効く。
set -euo pipefail
# shellcheck source=lib.sh
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"

yes=0
if [[ ${1:-} == "-y" ]]; then yes=1; shift; fi
app_unit="${1:-${ISU_APP_UNIT:-}}"
require_sudo
as_root test -f "$SNAPSHOT_DIR/manifest.txt" || die "スナップショットがありません: ${SNAPSHOT_DIR}（snapshot.sh を先に実行していない台）"

log "戻す内容: $SNAPSHOT_DIR"
as_root cat "$SNAPSHOT_DIR/manifest.txt" | sed 's/^/  /'
if as_root test -s "$CREATED_USERS_LIST"; then
  log "削除する MySQL ユーザー（opt-in で作ったもの）"; as_root cat "$CREATED_USERS_LIST" | sed 's/^/  /'
fi
if [[ $yes -ne 1 ]]; then
  read -r -p "この内容で全部戻します。よいですか？ [y/N] " ans
  [[ $ans == y || $ans == Y ]] || { log "中止"; exit 0; }
fi

# 戻す前に、タレが変えた sysctl のキーを控える（戻した後に値を書き戻すため）
mapfile -t sysctl_keys < <(as_root grep -vE '^\s*(#|$)' /etc/sysctl.d/99-isucon.conf 2>/dev/null | cut -d= -f1 | tr -d ' ' || true)

touched_nginx=0; touched_mysql=0; touched_sysctl=0; touched_systemd=0
while IFS='=' read -r kind path; do
  case "$kind" in
    dir)
      # スナップショット時点のツリーに完全一致させる（後から足したファイルは消える）
      if has_cmd rsync; then
        as_root rsync -a --delete "$SNAPSHOT_DIR$path/" "$path/"
      else
        as_root rm -rf "$path"; as_root cp -a "$SNAPSHOT_DIR$path" "$path"
      fi
      log "戻した: $path"
      ;;
    file)
      as_root cp -a "$SNAPSHOT_DIR$path" "$path"; log "戻した: $path" ;;
    *) continue ;;
  esac
  [[ $path == /etc/nginx* ]] && touched_nginx=1
  [[ $path == /etc/mysql* ]] && touched_mysql=1
  [[ $path == /etc/sysctl.d* || $path == /etc/security* ]] && touched_sysctl=1
  [[ $path == /etc/systemd* ]] && touched_systemd=1
done < <(as_root cat "$SNAPSHOT_DIR/manifest.txt")

if [[ $touched_sysctl -eq 1 ]]; then
  as_root sysctl --system >/dev/null 2>&1 || true
  # ファイルを戻しても、値は書いていた項目が残る。スナップショット時点の値を書き戻す
  if as_root test -f "$SNAPSHOT_DIR/sysctl-a.txt"; then
    for k in ${sysctl_keys[@]+"${sysctl_keys[@]}"}; do
      v=$(as_root grep -E "^$k = " "$SNAPSHOT_DIR/sysctl-a.txt" | head -1 | cut -d= -f2- | sed 's/^ //')
      [[ -n $v ]] && as_root sysctl -w "$k=$v" >/dev/null 2>&1 || true
    done
    as_root sysctl -w net.ipv4.ip_local_reserved_ports= >/dev/null 2>&1 || true
    log "sysctl の値をスナップショット時点に戻した"
  fi
fi

if [[ $touched_systemd -eq 1 || $touched_mysql -eq 1 ]]; then
  as_root systemctl daemon-reload
fi

if [[ $touched_nginx -eq 1 ]] && has_unit nginx; then
  if as_root nginx -t >/dev/null 2>&1; then as_root systemctl reload nginx; log "nginx reload"; else warn "nginx -t に失敗。設定を確認（sudo nginx -t）"; fi
fi

db_unit="$(detect_db_unit)"
if [[ $touched_mysql -eq 1 && -n $db_unit ]]; then
  log "systemctl restart $db_unit"
  as_root systemctl restart "$db_unit" || warn "$db_unit の再起動に失敗。journalctl -u $db_unit -n 50"
  for _ in $(seq 1 30); do as_root mysqladmin -uroot ping >/dev/null 2>&1 && break; sleep 1; done
  if as_root test -s "$CREATED_USERS_LIST"; then
    while IFS= read -r uh; do
      [[ -z $uh ]] && continue
      u="${uh%@*}"; h="${uh#*@}"
      mysql_root -e "DROP USER IF EXISTS '$u'@'$h'" && log "MySQL ユーザー削除: $uh" || warn "削除失敗: $uh"
    done < <(as_root cat "$CREATED_USERS_LIST")
    as_root rm -f "$CREATED_USERS_LIST"
  fi
fi

if [[ -n $app_unit ]] && has_unit "${app_unit%.service}"; then
  as_root systemctl restart "${app_unit%.service}"; sleep 2
  log "アプリ再起動: ${app_unit}（active=$(systemctl is-active "${app_unit%.service}")）"
else
  warn "アプリは再起動していない（ユニット名を引数か ISU_APP_UNIT で渡すと再起動する）。DB を戻した後はアプリの再起動が要る"
fi

health_check || true
log "復元完了。apply ごとのバックアップ（$BACKUP_ROOT/<日時>-*）はそのまま残している"
