#!/usr/bin/env bash
# 実機の構成を読んで、カタログ（catalog.md）の判断材料を 1 画面に出す（各サーバーで実行。何も変えない）。
#   ./inspect.sh              # 人が読む形式
#   ./inspect.sh > inspect-<host>.txt   # Claude に渡す
set -uo pipefail
# shellcheck source=lib.sh
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"

h() { printf '\n== %s\n' "$*"; }
kv() { printf '  %-34s %s\n' "$1" "$2"; }

h "ホスト"
kv hostname "$(hostname)"; kv kernel "$(uname -r)"; kv arch "$(uname -m)"
kv os "$(. /etc/os-release; echo "$PRETTY_NAME")"
kv nproc "$(nproc)"; kv mem_total_mb "$(free -m | awk '/^Mem:/{print $2}')"; kv mem_avail_mb "$(free -m | awk '/^Mem:/{print $7}')"
kv swap_mb "$(free -m | awk '/^Swap:/{print $2}')"; kv disk_root "$(df -h / | awk 'NR==2{print $4" free / "$2}')"
kv private_ip "$(ip -4 -o addr show scope global 2>/dev/null | awk '{print $4}' | tr '\n' ' ')"
kv snapshot "$(as_root test -d "$SNAPSHOT_DIR" && echo "あり（$(as_root sed -n 's/^taken_at=//p' "$SNAPSHOT_DIR/manifest.txt")）" || echo "なし（snapshot.sh を先に）")"

h "サービス（running）"
systemctl list-units --type=service --state=running --no-legend 2>/dev/null | awk '{print "  "$1}' | grep -iE 'isu|nginx|mysql|mariadb|redis|memcache|pgsql|postgres|newrelic|app|web' || echo "  (該当なし)"
h "待ち受けポート"
as_root ss -ltnp 2>/dev/null | awk 'NR>1{n=split($4,a,":"); p=a[n]; proc=""; if (match($0, /users:\(\("[^"]+"/)) { proc=substr($0, RSTART+9, RLENGTH-10) } if (!(p in seen)) { seen[p]=1; printf "  %-8s %s\n", p, proc } }' | sort -n -k1

h "アプリの systemd ユニット候補と env"
for u in $(systemctl list-unit-files --type=service --no-legend 2>/dev/null | awk '{print $1}' | grep -iE 'isu|app|web|api|go|ruby|python|node|php|perl|rust' | grep -vE '@|apparmor|apport|snapd|systemd|dbus|cloud|google|whoopsie|networkd|rsyslog|getty|ssh|polkit|udisks|upower|user'); do
  printf '  %-28s active=%s enabled=%s\n' "$u" "$(systemctl is-active "$u" 2>/dev/null)" "$(systemctl is-enabled "$u" 2>/dev/null)"
  systemctl show "$u" -p EnvironmentFiles -p LimitNOFILE -p ExecStart --no-pager 2>/dev/null | sed 's/^/      /' | cut -c1-160
done
for f in /home/isucon/env.sh /home/isucon/*.env /home/isucon/webapp/env.sh; do
  [[ -f $f ]] && { echo "  $f:"; grep -vE '^\s*(#|$)' "$f" | sed -E 's/(PASS[A-Z_]*|SECRET[A-Z_]*|KEY)=.*/\1=***/' | sed 's/^/      /'; }
done

h "nginx"
if has_cmd nginx || [[ -x /usr/sbin/nginx ]]; then
  kv version "$(nginx -v 2>&1 | sed 's/nginx version: //')"
  main=/etc/nginx/nginx.conf
  kv "conf.d include" "$(as_root grep -qE '^\s*include\s+/etc/nginx/conf\.d/\*\.conf;' $main && echo あり || echo 'なし（追記が読まれない）')"
  for k in worker_processes worker_rlimit_nofile worker_connections multi_accept keepalive_timeout keepalive_requests sendfile tcp_nopush gzip gzip_comp_level open_file_cache proxy_buffers client_max_body_size access_log log_format; do
    v=$(as_root grep -hE "^\s*$k\s" $main /etc/nginx/conf.d/*.conf 2>/dev/null | head -1 | sed -E 's/^\s+//; s/;.*$//' | cut -c1-80)
    kv "$k" "${v:-（未設定）}"
  done
  kv "conf.d" "$(ls /etc/nginx/conf.d/ 2>/dev/null | tr '\n' ' ')"
  kv "sites-enabled" "$(ls /etc/nginx/sites-enabled/ 2>/dev/null | tr '\n' ' ')"
  echo "  --- sites-enabled の中身（upstream / listen / root / location / proxy_pass）"
  as_root grep -hE '^\s*(upstream|server\s|listen|server_name|root|location|proxy_pass|try_files|keepalive|include)' /etc/nginx/sites-enabled/* 2>/dev/null | sed 's/^/      /' | head -40
  kv "nginx worker の open files" "$(cat /proc/"$(pgrep -n -x nginx 2>/dev/null || echo 1)"/limits 2>/dev/null | awk '/open files/{print $4"/"$5}')（master: $(cat /proc/"$(pgrep -o -x nginx 2>/dev/null || echo 1)"/limits 2>/dev/null | awk '/open files/{print $4}')）"
else
  echo "  (nginx なし)"
fi

h "MySQL / MariaDB"
flavor="$(detect_db_flavor)"
if [[ $flavor != none ]] && mysql_root -e 'SELECT 1' >/dev/null 2>&1; then
  kv flavor "${flavor}（unit: $(detect_db_unit)）"
  mysql_root -N -e "SELECT CONCAT('  ', LPAD(variable_name, 34, ' '), ' ', variable_value) FROM performance_schema.global_variables WHERE variable_name IN ('version','innodb_buffer_pool_size','innodb_flush_log_at_trx_commit','innodb_redo_log_capacity','innodb_log_file_size','innodb_log_buffer_size','innodb_flush_method','innodb_io_capacity','max_connections','tmp_table_size','max_heap_table_size','log_bin','skip_name_resolve','performance_schema','slow_query_log','long_query_time','bind_address') ORDER BY variable_name" 2>/dev/null \
  || mysql_root -N -e "SHOW VARIABLES WHERE Variable_name IN ('version','innodb_buffer_pool_size','innodb_flush_log_at_trx_commit','innodb_log_file_size','innodb_log_buffer_size','innodb_flush_method','innodb_io_capacity','max_connections','tmp_table_size','max_heap_table_size','log_bin','skip_name_resolve','performance_schema','slow_query_log','long_query_time','bind_address')" | awk '{printf "  %-34s %s\n", $1, $2}'
  kv "LimitNOFILE（$(detect_db_unit)）" "$(systemctl show "$(detect_db_unit)" -p LimitNOFILE --value)"
  kv "データサイズ MB（全 DB）" "$(mysql_root -N -e "SELECT ROUND(SUM(data_length+index_length)/1024/1024) FROM information_schema.tables WHERE table_schema NOT IN ('mysql','sys','information_schema','performance_schema')" 2>/dev/null)"
  echo "  --- ユーザー（host が localhost だけなら、skip_name_resolve で TCP 接続が壊れる）"
  mysql_root -N -e "SELECT CONCAT('      ', user, '@', host, '  ', plugin) FROM mysql.user WHERE user NOT IN ('mysql.sys','mysql.session','mysql.infoschema','mariadb.sys')" 2>/dev/null
  kv "conf.d" "$(ls "$MYSQL_CONF_DIR" 2>/dev/null | tr '\n' ' ')"
  kv "スロークエリログ" "$(as_root ls -lh "$MYSQL_SLOW_LOG" 2>/dev/null | awk '{print $5" "$9}' || echo なし)"
else
  echo "  (MySQL / MariaDB なし、または root で接続できない)"
fi

h "カーネル（現在値）"
for k in net.core.somaxconn net.ipv4.tcp_max_syn_backlog net.core.netdev_max_backlog net.ipv4.ip_local_port_range net.ipv4.tcp_tw_reuse net.ipv4.tcp_fin_timeout net.ipv4.tcp_max_tw_buckets net.ipv4.tcp_slow_start_after_idle fs.file-max fs.nr_open vm.swappiness; do
  kv "$k" "$(sysctl -n "$k" 2>/dev/null | tr '\t' '-')"
done
kv "sysctl.d" "$(ls /etc/sysctl.d/ 2>/dev/null | tr '\n' ' ')"
kv "limits.d" "$(ls /etc/security/limits.d/ 2>/dev/null | tr '\n' ' ')"
kv "ListenDrops（累計）" "$(nstat -az TcpExtListenDrops 2>/dev/null | awk 'NR==2{print $2}')"

h "判断用カウンタ（△ 項目を当てるか決める。ベンチ 1 回のあとに見る）"
if [[ $flavor != none ]] && mysql_root -e 'SELECT 1' >/dev/null 2>&1; then
  mysql_root -N -e "SHOW GLOBAL STATUS WHERE Variable_name IN ('Uptime','Innodb_log_waits','Created_tmp_tables','Created_tmp_disk_tables','Innodb_buffer_pool_wait_free','Innodb_buffer_pool_reads','Innodb_buffer_pool_read_requests','Max_used_connections','Sort_merge_passes','Select_scan','Handler_read_rnd_next')" 2>/dev/null | awk '{printf "  %-34s %s\n",$1,$2}'
  kv "buffer_pool_pages_data（載った量）" "$(mysql_root -N -e "SELECT CONCAT(ROUND(variable_value*16/1024),' MB') FROM performance_schema.global_status WHERE variable_name='Innodb_buffer_pool_pages_data'" 2>/dev/null)"
  echo "  読み方: Innodb_log_waits>0 → B5、Created_tmp_disk_tables>0 → B7、wait_free>0 → B6、pages_data が pool に近い → B1 を増やす、Sort_merge_passes / Handler_read_rnd_next が大きい → index 不足（アプリ側）"
fi
kv "nginx: upstream 一時ファイル行き" "$({ as_root grep -c 'buffered to a temporary file' /var/log/nginx/error.log 2>/dev/null || true; } | tail -1) 件（>0 → A6）"
kv "softnet dropped" "$(awk '{d+=strtonum("0x"$2)} END{print d}' /proc/net/softnet_stat 2>/dev/null)（>0 → C4）"
kv "TIME_WAIT 現在 / 上限" "$(ss -tan state time-wait 2>/dev/null | awk 'NR>1' | wc -l | tr -d ' ') / $(sysctl -n net.ipv4.tcp_max_tw_buckets)（半分超え → C3）"
nstat -az TcpExtListenDrops TcpExtTCPTimeWaitOverflow 2>/dev/null | awk 'NR>1{printf "  %-34s %s\n",$1,$2}'

h "計測ツール"
for c in alp pt-query-digest pidstat vmstat jq; do kv "$c" "$(command -v $c 2>/dev/null || echo なし)"; done
kv "newrelic-infra" "$(systemctl is-active newrelic-infra 2>/dev/null || echo なし)"

h "疎通（${CHECK_URL}）"
kv result "$(curl -s -o /dev/null -w 'HTTP %{http_code}, %{size_download} bytes, %{time_total}s' --max-time 10 "$CHECK_URL" 2>/dev/null || echo 接続できない)"
echo
