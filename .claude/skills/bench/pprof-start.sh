#!/usr/bin/env bash
# ISUCON2026: ベンチ中の CPU プロファイル（pprof）を各アプリサーバーで裏取りする。bench-start.sh から呼ばれる
# 使い方: pprof-start.sh
#   各サーバーで待機スクリプトを nohup で起動し、負荷の開始（アクションログが 3 秒に ISU_PPROF_TRIGGER 行以上）を検知したら
#   ISU_PPROF_SECONDS 秒の CPU プロファイルとヒープを /tmp/isu-pprof/ に保存する。回収は pprof-collect.sh
set -uo pipefail
. "$(dirname "$0")/lib.sh"
WAIT_SCRIPT=$(cat <<REMOTE
#!/usr/bin/env bash
# pprof-start.sh が置いた待機スクリプト。負荷開始を待って CPU プロファイルを取る
DIR=/tmp/isu-pprof; PORT=$ISU_PPROF_PORT; UNIT=$ISU_APP_UNIT
rm -f "\$DIR/cpu.prof" "\$DIR/heap.prof" "\$DIR/done"
if ! curl -sf -o /dev/null "http://127.0.0.1:\$PORT/debug/pprof/"; then echo "pprof に届かない（:\$PORT）"; echo skip > "\$DIR/done"; exit 0; fi
for i in \$(seq 1 $ISU_PPROF_MAX_WAIT); do
  n=\$(journalctl -u "\$UNIT" -o cat --since "-3s" 2>/dev/null | grep -c '"action"')
  if [ "\$n" -ge $ISU_PPROF_TRIGGER ]; then echo "load detected after \${i}s (\$n lines/3s) \$(date -u +%T)"; break; fi
  sleep 1
done
echo "profile start \$(date -u +%T)"
curl -s "http://127.0.0.1:\$PORT/debug/pprof/profile?seconds=$ISU_PPROF_SECONDS" -o "\$DIR/cpu.prof"
echo "profile end \$(date -u +%T)"
curl -s "http://127.0.0.1:\$PORT/debug/pprof/heap" -o "\$DIR/heap.prof"
echo ok > "\$DIR/done"
REMOTE
)
for h in $ISU_PPROF_HOSTS; do
  # 前回の待機が残っていたら pid ファイルで止める（pkill -f は ssh のシェル自身に当たるので使わない）
  # `&` は直前のコマンドだけに掛かるように `;` で区切る（`&&` でつなぐと列全体が裏に回り ssh の終了で死ぬ）
  if ssh "${SSH_OPTS[@]}" "$h" "mkdir -p /tmp/isu-pprof; cat > /tmp/isu-pprof/wait.sh || exit 1; [ -f /tmp/isu-pprof/pid ] && kill \$(cat /tmp/isu-pprof/pid) 2>/dev/null; setsid nohup bash /tmp/isu-pprof/wait.sh > /tmp/isu-pprof/log 2>&1 < /dev/null & echo \$! > /tmp/isu-pprof/pid" <<<"$WAIT_SCRIPT"; then
    echo "pprof 待機開始: $h"
  else
    echo "⚠️ $h で pprof の待機を開始できなかった" >&2
  fi
done
