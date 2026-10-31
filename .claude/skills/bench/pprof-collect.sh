#!/usr/bin/env bash
# ISUCON2026: pprof-start.sh が各サーバーに残した CPU プロファイルを回収して集計する。bench-report.sh から呼ばれる
# 使い方: pprof-collect.sh <保存先ディレクトリ>
#   <保存先>/pprof-<host>.txt に flat 上位と cum 上位（全部と、アプリの関数だけ）を書き、標準出力に Discord 用の要約を出す
set -uo pipefail
. "$(dirname "$0")/lib.sh"
OUT="${1:?保存先ディレクトリを指定して}"
PKG="example.com/isureal-company/isureal."
command -v go >/dev/null || { echo "(go が無いので pprof を集計できない)"; exit 0; }
for h in $ISU_PPROF_HOSTS; do
  # 取得が終わるのを少し待つ（ベンチ完了直後に呼ばれると profile がまだ走っていることがある）
  for _ in $(seq 1 30); do ssh "${SSH_OPTS[@]}" "$h" "test -f /tmp/isu-pprof/done" 2>/dev/null && break; sleep 2; done
  ssh "${SSH_OPTS[@]}" "$h" "cat /tmp/isu-pprof/log 2>/dev/null" > "$OUT/pprof-$h.txt" 2>/dev/null
  if ! scp -q "${SSH_OPTS[@]}" "$h:/tmp/isu-pprof/cpu.prof" "$OUT/cpu-$h.prof" 2>/dev/null || [ ! -s "$OUT/cpu-$h.prof" ]; then
    echo "$h: プロファイルなし（$(tail -1 "$OUT/pprof-$h.txt" 2>/dev/null)）"; rm -f "$OUT/cpu-$h.prof"; continue
  fi
  scp -q "${SSH_OPTS[@]}" "$h:/tmp/isu-pprof/heap.prof" "$OUT/heap-$h.prof" 2>/dev/null
  FLAT=$(go tool pprof -top -nodecount=40 "$OUT/cpu-$h.prof" 2>/dev/null)
  CUM=$(go tool pprof -top -cum -nodecount=400 "$OUT/cpu-$h.prof" 2>/dev/null)
  {
    echo "== $h: flat 上位（CPU を直接使った関数）"; echo "$FLAT"
    echo; echo "== $h: cum 上位（呼び出し先を含む）"; echo "$CUM" | head -60
    echo; echo "== $h: cum 上位（アプリの関数だけ）"; echo "$CUM" | grep "$PKG" | grep -v Middleware | head -40
  } >> "$OUT/pprof-$h.txt"
  # 要約: 計測時間に対する CPU 使用量、アプリ関数の cum 上位、flat 上位
  TOTAL=$(echo "$FLAT" | sed -n 's/^Duration: \([0-9.]*\)s, Total samples = \([0-9.]*\)s (\([0-9.]*\)%).*/\2s \/ \1s（\3%、2 コアなら 200% が上限）/p')
  APP=$(echo "$CUM" | grep "$PKG" | grep -v "Middleware\|func[0-9]*\.1$" | head -6 | awk -v p="$PKG" '{n=$6; sub(p,"",n); sub(/^NewServer\./,"",n); sub(/\.func[0-9]+$/,"",n); printf "%s%s %s", (NR>1?" | ":""), n, $5}')
  LEAF=$(echo "$FLAT" | awk '$1 ~ /^[0-9.]+(s|ms)$/ && $2 ~ /%$/ && c < 3 {n=$6; sub(/.*\//,"",n); printf "%s%s %s", (c>0?" | ":""), n, $2; c++}')
  echo "$h: CPU ${TOTAL:-サンプルなし（負荷が無かったか取得が早すぎた）}"
  echo "  app(cum): ${APP:-なし}"
  echo "  leaf(flat): ${LEAF:-なし}"
done
