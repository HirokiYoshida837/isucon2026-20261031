#!/usr/bin/env bash
# apply-*.sh / measure.sh で配置した設定を、実行前の状態に戻す。
#   ./rollback.sh                    # バックアップ一覧を表示するだけ
#   ./rollback.sh <バックアップ名>    # 確認してから戻す（名前は一覧の名前 or フルパス）
#   ./rollback.sh -y <バックアップ名> # 確認なしで戻す（apply-*.sh の自動復旧から呼ばれる）
#
# 戻し方
# - 置き換えたファイルはバックアップから書き戻す。
# - 新規作成したファイル（created.list）は削除する。
# - 触ったパスに応じて nginx reload / mysqld restart / sysctl --system を行う。
#
# 【危険】/etc/mysql 以下を戻した場合は mysqld を restart する（数十秒アプリが DB に繋がらない）。
# 【注意】sysctl は「ファイル」を戻すだけ。値は既に反映済みなので、完全に戻るのは OS 再起動後。
#          今すぐ戻したいときは バックアップ内の sysctl-before.txt を見て sysctl -w で戻す。
set -euo pipefail
# shellcheck source=lib.sh
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"

yes=0
if [[ ${1:-} == "-y" ]]; then yes=1; shift; fi
target="${1:-}"

if [[ -z $target ]]; then
  log "バックアップ一覧（新しい順）: $BACKUP_ROOT"
  as_root ls -1t "$BACKUP_ROOT" 2>/dev/null || echo "  (なし)"
  exit 0
fi

if [[ $target == /* ]]; then dir="$target"; else dir="$BACKUP_ROOT/$target"; fi
as_root test -d "$dir" || die "バックアップがありません: $dir"
require_sudo

# 戻すファイルの一覧（バックアップ内のパス = 元のパス）。
mapfile -t restore < <(as_root find "$dir" -mindepth 2 ! -type d | sed "s#^$dir##" | sort)
created=()
if as_root test -f "$dir/created.list"; then
  mapfile -t created < <(as_root cat "$dir/created.list")
fi

log "戻す内容: $dir"
for p in "${restore[@]}"; do echo "  書き戻し: $p"; done
for p in "${created[@]}"; do echo "  削除    : $p"; done
if [[ ${#restore[@]} -eq 0 && ${#created[@]} -eq 0 ]]; then
  log "戻すものがありません"
  exit 0
fi

if [[ $yes -ne 1 ]]; then
  read -r -p "実行しますか？ [y/N] " ans
  [[ $ans == y || $ans == Y ]] || die "中止しました"
fi

for p in "${restore[@]}"; do
  as_root mkdir -p "$(dirname "$p")"
  as_root cp -a "$dir$p" "$p"
done
for p in "${created[@]}"; do
  [[ -n $p ]] && as_root rm -f "$p"
done

# 触ったパスから、反映が必要なサービスを判断する。
all_paths="$(printf '%s\n' "${restore[@]}" "${created[@]}")"

if grep -q '^/etc/systemd/' <<< "$all_paths"; then
  as_root systemctl daemon-reload
fi

if grep -q '^/etc/nginx/' <<< "$all_paths"; then
  log "nginx -t && reload"
  as_root nginx -t && as_root systemctl reload nginx
fi

if grep -qE '^/etc/mysql/|^/etc/systemd/system/(mysql|mariadb)\.service\.d/' <<< "$all_paths"; then
  unit="$(detect_db_unit)"
  if [[ -n $unit ]]; then
    log "systemctl restart $unit"
    as_root systemctl restart "$unit"
  fi
fi

if grep -q '^/etc/sysctl.d/' <<< "$all_paths"; then
  as_root sysctl --system >/dev/null
  warn "sysctl はファイルだけ戻した。値を即時に戻すなら $dir/sysctl-before.txt を参照"
fi

log "ロールバック完了"
