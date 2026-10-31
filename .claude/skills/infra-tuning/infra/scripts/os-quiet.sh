#!/usr/bin/env bash
# OS の不要なタイマー・サービスを止め、sysctl（sysctl/91-isucon.conf）を当てる。4 台すべてで実行する（issue #23）。
#   sudo bash os-quiet.sh --dry-run   # 何をするか表示だけ
#   sudo bash os-quiet.sh             # 適用 → sysctl --system → envcheck
#   sudo bash os-quiet.sh --restore   # 戻す（サービスを enable、sysctl ファイルを削除）
#
# 触らないもの（主催者・競技に必要）: amazon-ssm-agent、isucon-register、envcheck*、chrony、ssh、nginx、postgresql、isureal-*、newrelic-infra
set -euo pipefail

here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
src_sysctl="$here/../sysctl/91-isucon.conf"
dst_sysctl="/etc/sysctl.d/91-isucon.conf"

timers=(apt-daily.timer apt-daily-upgrade.timer fwupd-refresh.timer motd-news.timer update-notifier-download.timer update-notifier-motd.timer)
services=(unattended-upgrades.service ModemManager.service multipathd.service multipathd.socket udisks2.service)

mode=apply
for a in "$@"; do
  case "$a" in
    --dry-run) mode=dry ;;
    --restore) mode=restore ;;
    *) echo "unknown option: $a" >&2; exit 2 ;;
  esac
done

sudo_cmd=()
if [[ "$(id -u)" -ne 0 ]]; then sudo_cmd=(sudo); fi

run() {
  if [[ "$mode" == dry ]]; then
    echo "[dry-run] $*"
  else
    echo "+ $*"
    "${sudo_cmd[@]}" "$@"
  fi
}

# unit ファイルが存在するか
unit_exists() {
  systemctl list-unit-files --no-legend "$1" 2>/dev/null | grep -q .
}

unit_state() {
  printf '%s/%s' "$(systemctl is-enabled "$1" 2>/dev/null || true)" "$(systemctl is-active "$1" 2>/dev/null || true)"
}

echo "== host: $(hostname)  mode: $mode"

if [[ "$mode" == restore ]]; then
  for u in "${timers[@]}"; do
    unit_exists "$u" || { echo "skip (not found): $u"; continue; }
    run systemctl enable --now "$u"
  done
  for u in "${services[@]}"; do
    unit_exists "$u" || { echo "skip (not found): $u"; continue; }
    run systemctl enable "$u"
  done
  if [[ -e "$dst_sysctl" ]]; then
    run rm -f "$dst_sysctl"
  else
    echo "skip (not found): $dst_sysctl"
  fi
  run sysctl --system >/dev/null
  echo "== restored. 確認: systemctl list-timers / sysctl net.ipv4.tcp_max_syn_backlog"
  exit 0
fi

echo "== 1. タイマー・サービスを止める（disable --now）"
for u in "${timers[@]}" "${services[@]}"; do
  if ! unit_exists "$u"; then
    echo "skip (not found): $u"
    continue
  fi
  echo "  $u: $(unit_state "$u")"
  run systemctl disable --now "$u"
done

echo "== 2. sysctl: $src_sysctl -> $dst_sysctl"
[[ -r "$src_sysctl" ]] || { echo "not found: $src_sysctl" >&2; exit 1; }
if [[ "$mode" == dry ]]; then
  if [[ -e "$dst_sysctl" ]]; then
    diff -u "$dst_sysctl" "$src_sysctl" || true
  else
    echo "[dry-run] (new file)"
    cat "$src_sysctl"
  fi
fi
run install -m 0644 -o root -g root "$src_sysctl" "$dst_sysctl"
run sysctl --system >/dev/null
if [[ "$mode" != dry ]]; then
  sysctl net.ipv4.tcp_max_syn_backlog net.core.netdev_max_backlog net.ipv4.tcp_fin_timeout
fi

echo "== 3. envcheck"
if [[ "$mode" == dry ]]; then
  echo "[dry-run] /usr/local/bin/envcheck"
  exit 0
fi
"${sudo_cmd[@]}" /usr/local/bin/envcheck
echo "== done. 戻すには: $0 --restore"
