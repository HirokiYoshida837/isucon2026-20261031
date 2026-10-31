#!/usr/bin/env bash
# resource-sampler.sh と pidstat の記録を集計する。bench-after.sh から呼ばれる。
#   ./resource-summary.sh <結果ディレクトリ>
# 入力: resources.tsv（resource-sampler.sh）、pidstat.log（pidstat -h -u -r）
# 出力: resources.txt（項目ごとの avg / p50 / p95 / max）、procs.txt（プロセス別 CPU・メモリ上位）
# サンプルが 0 件の入力は出力ファイルを作らない（bench-after.sh が -s で有無を判定する）。
set -euo pipefail
run_dir="${1:?結果ディレクトリを指定して}"

warn() { printf '\033[33m[WARN]\033[0m %s\n' "$*" >&2; }

# 一時ファイルに書き、中身があるときだけ本来の名前にする。
finish_output() {
  local tmp="$1" dst="$2" what="$3"
  if [[ -s $tmp ]]; then
    mv -f "$tmp" "$dst"
  else
    rm -f "$tmp" "$dst"
    warn "$what: サンプルが無いので出力しない"
  fi
}

if [[ -s $run_dir/resources.tsv ]]; then
  awk -F'\t' '
    NR == 1 { for (c = 2; c <= NF; c++) name[c] = $c; nc = NF; next }
    { n++; for (c = 2; c <= nc; c++) if ($c != "-") { v[c, ++cnt[c]] = $c + 0; sum[c] += $c } }
    END {
      if (n == 0) exit
      printf "%-14s %9s %9s %9s %9s\n", "metric", "avg", "p50", "p95", "max"
      for (c = 2; c <= nc; c++) {
        m = cnt[c]; if (m == 0) { printf "%-14s %9s %9s %9s %9s\n", name[c], "-", "-", "-", "-"; continue }
        # 挿入ソート（サンプル数は数百程度）
        for (i = 1; i <= m; i++) a[i] = v[c, i]
        for (i = 2; i <= m; i++) { x = a[i]; j = i - 1; while (j >= 1 && a[j] > x) { a[j + 1] = a[j]; j-- } a[j + 1] = x }
        p50 = a[int((m - 1) * 0.50) + 1]; p95 = a[int((m - 1) * 0.95) + 1]
        printf "%-14s %9.1f %9.1f %9.1f %9.1f\n", name[c], sum[c] / m, p50, p95, a[m]
      }
      printf "(samples=%d)\n", n
    }' "$run_dir/resources.tsv" > "$run_dir/.resources.tmp" || warn "resources.tsv の集計に失敗"
  finish_output "$run_dir/.resources.tmp" "$run_dir/resources.txt" "resources.txt"
else
  warn "resources.tsv が無いか空（resource-sampler.sh が動いていない）"
fi

if [[ -s $run_dir/pidstat.log ]]; then
  # 見出し行から列位置を求める（sysstat のバージョン差を吸収）。データ行は見出しより "#" の分 1 列少ない
  awk '
    /^# *Time/ && !hdr { for (i = 2; i <= NF; i++) { if ($i == "%CPU") cpu = i - 1; if ($i == "RSS") rss = i - 1; if ($i == "Command") cmd = i - 1; if ($i == "Time") tcol = i - 1 } hdr = 1; next }
    /^#/ || NF == 0 || !hdr { next }
    {
      t = $tcol; if (!(t in seen)) { seen[t] = 1; nt++ }
      k = $cmd; c[k] += $cpu; if ($rss + 0 > r[k]) r[k] = $rss + 0
    }
    END {
      if (nt == 0) exit
      printf "%-20s %9s %10s\n", "command", "cpu_avg%", "rss_max_mb"
      for (k in c) printf "%-20s %9.1f %10.1f\n", k, c[k] / nt, r[k] / 1024 | "sort -k2 -nr | head -10"
      close("sort -k2 -nr | head -10")
      printf "(intervals=%d, cpu%% は 1 コア = 100%%)\n", nt
    }' "$run_dir/pidstat.log" > "$run_dir/.procs.tmp" || warn "pidstat.log の集計に失敗"
  finish_output "$run_dir/.procs.tmp" "$run_dir/procs.txt" "procs.txt"
else
  warn "pidstat.log が無いか空（pidstat が動いていない）"
fi
