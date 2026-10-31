#!/usr/bin/env bash
# ベンチ中のリソースを一定間隔で TSV に記録する。bench-before.sh が裏で起動し、bench-after.sh が止めて集計する。
#   ./resource-sampler.sh <出力 TSV> [間隔秒（既定 2）]
#
# 列: time cpu_busy cpu_user cpu_sys cpu_iowait cpu_steal mem_used_mb mem_avail_mb swap_used_mb load1
#     disk_r_kBs disk_w_kBs net_rx_kBs net_tx_kBs tcp_estab tcp_timewait db_conn db_running
# - /proc から直接読むので、追加のパッケージは要らない
# - db_conn / db_running は MySQL の Threads_connected / Threads_running。DB が無いサーバーでは -
set -uo pipefail
# shellcheck source=lib.sh
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"

out="${1:?出力ファイルを指定して}"
iv="${2:-2}"

# このサーバーで MySQL に root で入れるか（入れなければ DB 列は - にする）
has_db=0
if sudo -n true 2>/dev/null && sudo -n mysql -uroot -N -e 'SELECT 1' >/dev/null 2>&1; then has_db=1; fi

# CPU: user nice system idle iowait irq softirq steal の合計値
read_cpu() { awk '/^cpu /{print $2, $3, $4, $5, $6, $7, $8, $9; exit}' /proc/stat; }
# ディスク: 物理ディスク全体（パーティション・loop を除く）の読み書きセクタ数
read_disk() {
  awk '$3 ~ /^(nvme[0-9]+n[0-9]+|xvd[a-z]+|sd[a-z]+|vd[a-z]+)$/ {r += $6; w += $10} END {print r + 0, w + 0}' /proc/diskstats
}
# ネットワーク: lo 以外の受信・送信バイト数
read_net() {
  awk -F'[: ]+' 'NR > 2 && $2 != "lo" {rx += $3; tx += $11} END {print rx + 0, tx + 0}' /proc/net/dev
}
# TCP: 状態 01=ESTABLISHED、06=TIME_WAIT を数える（IPv4 と IPv6）
read_tcp() {
  cat /proc/net/tcp /proc/net/tcp6 2>/dev/null | awk '$4 == "01" {e++} $4 == "06" {t++} END {print e + 0, t + 0}'
}
read_mem() {
  awk '/^MemTotal:/{t=$2} /^MemAvailable:/{a=$2} /^SwapTotal:/{st=$2} /^SwapFree:/{sf=$2}
       END {printf "%d %d %d", (t - a) / 1024, a / 1024, (st - sf) / 1024}' /proc/meminfo
}
read_db() {
  if [[ $has_db -eq 1 ]]; then
    sudo -n mysql -uroot -N -e "SHOW GLOBAL STATUS WHERE Variable_name IN ('Threads_connected','Threads_running')" 2>/dev/null |
      awk '{v[$1] = $2} END {print (v["Threads_connected"] == "" ? "-" : v["Threads_connected"]), (v["Threads_running"] == "" ? "-" : v["Threads_running"])}'
  else
    echo "- -"
  fi
}

printf 'time\tcpu_busy\tcpu_user\tcpu_sys\tcpu_iowait\tcpu_steal\tmem_used_mb\tmem_avail_mb\tswap_used_mb\tload1\tdisk_r_kBs\tdisk_w_kBs\tnet_rx_kBs\tnet_tx_kBs\ttcp_estab\ttcp_timewait\tdb_conn\tdb_running\n' > "$out"

read -r pu pn ps pi pw pq psq pst <<<"$(read_cpu)"
read -r pdr pdw <<<"$(read_disk)"
read -r prx ptx <<<"$(read_net)"
pt=$(date +%s.%N)

while sleep "$iv"; do
  read -r u n s i w q sq st <<<"$(read_cpu)"
  read -r dr dw <<<"$(read_disk)"
  read -r rx tx <<<"$(read_net)"
  now=$(date +%s.%N)
  read -r estab tw <<<"$(read_tcp)"
  read -r used avail swap <<<"$(read_mem)"
  read -r dbc dbr <<<"$(read_db)"
  load1=$(cut -d' ' -f1 /proc/loadavg)
  awk -v u=$((u - pu)) -v n=$((n - pn)) -v s=$((s - ps)) -v i=$((i - pi)) -v w=$((w - pw)) \
      -v q=$((q - pq)) -v sq=$((sq - psq)) -v st=$((st - pst)) \
      -v dr=$((dr - pdr)) -v dw=$((dw - pdw)) -v rx=$((rx - prx)) -v tx=$((tx - ptx)) \
      -v dt="$(awk -v a="$now" -v b="$pt" 'BEGIN {print a - b}')" \
      -v ts="$(date +%H:%M:%S)" -v used="$used" -v avail="$avail" -v swap="$swap" -v load1="$load1" \
      -v estab="$estab" -v tw="$tw" -v dbc="$dbc" -v dbr="$dbr" 'BEGIN {
        tot = u + n + s + i + w + q + sq + st; if (tot <= 0) tot = 1; if (dt <= 0) dt = 1
        printf "%s\t%.1f\t%.1f\t%.1f\t%.1f\t%.1f\t%d\t%d\t%d\t%s\t%.0f\t%.0f\t%.0f\t%.0f\t%d\t%d\t%s\t%s\n",
          ts, 100 * (tot - i - w) / tot, 100 * (u + n) / tot, 100 * (s + q + sq) / tot, 100 * w / tot, 100 * st / tot,
          used, avail, swap, load1, dr * 512 / 1024 / dt, dw * 512 / 1024 / dt, rx / 1024 / dt, tx / 1024 / dt,
          estab, tw, dbc, dbr
      }' >> "$out"
  pu=$u; pn=$n; ps=$s; pi=$i; pw=$w; pq=$q; psq=$sq; pst=$st
  pdr=$dr; pdw=$dw; prx=$rx; ptx=$tx; pt=$now
done
