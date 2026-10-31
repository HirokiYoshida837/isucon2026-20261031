#!/usr/bin/env bash
# 初手スナップショット: 秘伝のタレを当てる前の設定一式を丸ごと退避する（各サーバーで最初に 1 回）。
#   ./snapshot.sh            # 無ければ取る。既にあれば何もしない
#   ./snapshot.sh --force    # 取り直す（古いものは snapshot-old-<日時> に残す）
#
# 退避するもの（/home/isucon/infra-backup/snapshot/ に元のパスのまま保存）
#   /etc/nginx  /etc/mysql  /etc/sysctl.d  /etc/security/limits.d  /etc/systemd/system（drop-in 含む）
#   /home/isucon/env.sh 等のアプリ env（ISU_APP_ENV_FILES で追加可。スペース区切り）
#   sysctl -a の全値、有効なユニット一覧、待ち受けポート、MySQL のユーザー一覧と GRANT
# 戻し方: ./restore-all.sh（全部戻す） / ./rollback.sh（apply 1 回分だけ戻す）
set -euo pipefail
# shellcheck source=lib.sh
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"

force=0
[[ ${1:-} == "--force" ]] && force=1
require_sudo

if as_root test -d "$SNAPSHOT_DIR"; then
  if [[ $force -eq 1 ]]; then
    old="$BACKUP_ROOT/snapshot-old-$(date +%Y%m%d-%H%M%S)"
    as_root mv "$SNAPSHOT_DIR" "$old"
    log "古いスナップショットを退避: $old"
  else
    log "スナップショットは既にある: ${SNAPSHOT_DIR}（取り直すなら --force）"
    as_root cat "$SNAPSHOT_DIR/manifest.txt" 2>/dev/null | sed 's/^/  /' || true
    exit 0
  fi
fi

as_root mkdir -p "$SNAPSHOT_DIR"
dirs=(/etc/nginx /etc/mysql /etc/sysctl.d /etc/security/limits.d /etc/systemd/system)
files=(/home/isucon/env.sh)
# shellcheck disable=SC2206  # スペース区切りを配列に展開する
[[ -n ${ISU_APP_ENV_FILES:-} ]] && files+=($ISU_APP_ENV_FILES)

{
  echo "taken_at=$(date '+%Y-%m-%dT%H:%M:%S%z')"
  echo "hostname=$(hostname)"
  for d in "${dirs[@]}"; do
    if as_root test -d "$d"; then
      as_root mkdir -p "$SNAPSHOT_DIR$(dirname "$d")"
      as_root cp -a "$d" "$SNAPSHOT_DIR$d"
      echo "dir=$d"
    fi
  done
  for f in "${files[@]}"; do
    if as_root test -f "$f"; then
      as_root mkdir -p "$SNAPSHOT_DIR$(dirname "$f")"
      as_root cp -a "$f" "$SNAPSHOT_DIR$f"
      echo "file=$f"
    fi
  done
} | as_root tee "$SNAPSHOT_DIR/manifest.txt" >/dev/null

# 値の記録（復元の参考と、後で「何が変わったか」を見るため）
as_root sysctl -a 2>/dev/null | as_root tee "$SNAPSHOT_DIR/sysctl-a.txt" >/dev/null || true
systemctl list-unit-files --state=enabled --no-legend 2>/dev/null | as_root tee "$SNAPSHOT_DIR/units-enabled.txt" >/dev/null || true
systemctl list-units --type=service --state=running --no-legend 2>/dev/null | as_root tee "$SNAPSHOT_DIR/units-running.txt" >/dev/null || true
ss -ltnp 2>/dev/null | as_root tee "$SNAPSHOT_DIR/listen-ports.txt" >/dev/null || true
if [[ -n $(detect_db_unit) ]] && mysql_root -e 'SELECT 1' >/dev/null 2>&1; then
  mysql_root -N -e "SELECT CONCAT(user,'@',host) FROM mysql.user" 2>/dev/null | as_root tee "$SNAPSHOT_DIR/mysql-users.txt" >/dev/null || true
  {
    while IFS= read -r uh; do
      u="${uh%@*}"; h="${uh#*@}"
      mysql_root -N -e "SHOW GRANTS FOR '$u'@'$h'" 2>/dev/null | sed 's/$/;/' || true
    done < <(as_root cat "$SNAPSHOT_DIR/mysql-users.txt")
  } | as_root tee "$SNAPSHOT_DIR/mysql-grants.sql" >/dev/null
  mysql_root -e "SHOW GLOBAL VARIABLES" 2>/dev/null | as_root tee "$SNAPSHOT_DIR/mysql-variables.txt" >/dev/null || true
fi

log "スナップショット完了: $SNAPSHOT_DIR"
as_root cat "$SNAPSHOT_DIR/manifest.txt" | sed 's/^/  /'
log "全部戻すときは ./restore-all.sh"
