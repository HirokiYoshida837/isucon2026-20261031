#!/usr/bin/env bash
# 'user'@'localhost' だけのユーザーに、同じパスワード・同じ権限の 'user'@'127.0.0.1' を作る（DB の台で実行）。
#   ./mysql-add-tcp-users.sh           # 対象を表示して作る
#   ./mysql-add-tcp-users.sh --dry-run # 対象の表示だけ
# skip_name_resolve = ON を入れる前に実行する（入れると localhost の権限が TCP 接続に効かなくなり Error 1130 になる）。
# 作ったユーザーは created-users.list に記録し、restore-all.sh が削除する。
set -euo pipefail
# shellcheck source=lib.sh
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"

dry=0; [[ ${1:-} == --dry-run ]] && dry=1
[[ -n $(detect_db_unit) ]] || die "MySQL / MariaDB がありません"
require_sudo
n=0
while IFS=$'\t' read -r u plugin auth; do
  [[ -z $u ]] && continue
  if mysql_root -N -e "SELECT 1 FROM mysql.user WHERE user='$u' AND host='127.0.0.1'" | grep -q 1; then
    log "既にある: '$u'@'127.0.0.1'"; continue
  fi
  if [[ $dry -eq 1 ]]; then log "作成対象: '$u'@'127.0.0.1'（plugin=${plugin}）"; continue; fi
  ensure_snapshot
  mysql_root -e "CREATE USER '$u'@'127.0.0.1' IDENTIFIED WITH $plugin AS '$auth'"
  mysql_root -N -e "SHOW GRANTS FOR '$u'@'localhost'" | sed "s/\`$u\`@\`localhost\`/\`$u\`@\`127.0.0.1\`/g; s/$/;/" | mysql_root
  echo "$u@127.0.0.1" | as_root tee -a "$CREATED_USERS_LIST" >/dev/null
  log "作成: '$u'@'127.0.0.1'（restore-all.sh で削除される）"; n=$((n+1))
done < <(mysql_root -N -e "SELECT user, plugin, authentication_string FROM mysql.user WHERE host='localhost' AND user NOT IN ('root','debian-sys-maint','mysql.sys','mysql.session','mysql.infoschema','mariadb.sys')")
[[ $dry -eq 1 ]] || { mysql_root -e "FLUSH PRIVILEGES"; log "完了（$n ユーザー）。確認: mysql -h 127.0.0.1 -u <user> -p<pass> -e 'SELECT 1'"; }
