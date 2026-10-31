# インフラに当てたもの（再起動試験前に見る）

サーバーに入っている設定の一覧。1 行 1 項目。戻し方は SKILL `infra-tuning` の「戻し方」。
数値チューニングは再起動後も残る。構成に触った項目（✕）は無い。

| 時刻（UTC） | 台 | 項目 | 値 | 戻し方 / バックアップ |
|---|---|---|---|---|
| 10-31 02:33 | asia2 | PostgreSQL `conf.d/99-isucon-tuning.conf`（チームメイトが直接） | shared_buffers 1GB、synchronous_commit off、jit off、pg_stat_statements、max_wal_size 4GB、checkpoint_timeout 15min | `/home/isucon/pg-backup/20261031-023302/rollback.sh` |
| 10-31 03:05 | asia2 | PostgreSQL `conf.d/90-isucon.conf`（PR #51） | max_connections 300、ssl off、work_mem 16MB、wal_compression lz4、log_min_duration_statement 100ms ほか（99- と重なる項目は 99- が勝つ） | `sudo rm /etc/postgresql/16/main/conf.d/90-isucon.conf && sudo systemctl restart postgresql` |
| 10-31 03:06 | asia1 | nginx-main worker_connections / worker_rlimit_nofile | 8192 / 65536 | `rollback.sh -y 20261031-030632-nginx` / `20261031-030633-nginx` |
| 10-31 03:06 | asia1 | nginx-http keepalive_requests（`conf.d/99-isucon-tuning.conf`） | 10000 | `rollback.sh -y 20261031-030634-nginx` |
| 10-31 03:06 | europe1 | nginx-main worker_connections / worker_rlimit_nofile | 8192 / 65536 | `rollback.sh -y 20261031-030636-nginx` / `20261031-030637-nginx` |
| 10-31 03:06 | europe1 | nginx-http keepalive_requests | 10000 | `rollback.sh -y 20261031-030638-nginx` |
| 10-31 03:06 | america1 | nginx-main worker_connections / worker_rlimit_nofile | 8192 / 65536 | `rollback.sh -y 20261031-030639-nginx` / `20261031-030641-nginx` |
| 10-31 03:06 | america1 | nginx-http keepalive_requests | 10000 | `rollback.sh -y 20261031-030642-nginx` |
| 10-31 03:06 | asia1 / europe1 / america1 | journald（PR #50）`/etc/systemd/journald.conf.d/isucon.conf` | ForwardToSyslog=no、RateLimit 0、SystemMaxUse 1G | `sudo rm /etc/systemd/journald.conf.d/isucon.conf && sudo systemctl restart systemd-journald` |
| 10-31 03:07 | 4 台 | `os-quiet.sh`（PR #52）+ `/etc/sysctl.d/91-isucon.conf` | apt/fwupd/motd/update-notifier の timer と unattended-upgrades 等を disable。tcp_max_syn_backlog 8192、netdev_max_backlog 8192、tcp_fin_timeout 10 | `os-quiet.sh --restore` |
| 10-31 03:09 | asia1 / europe1 / america1 | nginx サイト設定 `apply-site.sh`（PR #49 + #55）+ `/etc/nginx/isucon/proxy_keepalive.conf` | upstream app keepalive 128、SSE（`/api/notifications`）の read/send timeout 1h・buffering off、画像 3 秒キャッシュ | `rollback.sh -y 20261031-030939-site`（asia1）/ `20261031-030942-site`（europe1）/ `20261031-030946-site`（america1） |
| 10-31 06:5x | asia2 | PostgreSQL `conf.d/95-bgwriter.conf`（#188） | bgwriter_delay 20ms、bgwriter_lru_maxpages 1000、bgwriter_lru_multiplier 4.0（SIGHUP で反映） | `sudo rm /etc/postgresql/16/main/conf.d/95-bgwriter.conf && sudo systemctl reload postgresql` |
| 10-31 07:12 | asia2 | PostgreSQL `conf.d/90-isucon.conf` の `log_min_duration_statement` を `-1`（スロークエリログ停止、SIGHUP で反映）。9.8GB のログを truncate（空き 18GB → 28GB） | SQL は pg_stat_statements と NewRelic で見る。envcheck PASS | 戻すなら `100ms` に書き換えて `sudo systemctl reload postgresql`。truncate は `sudo -u postgres truncate -s 0 <log>`（root だと fs.protected_regular=2 で Permission denied） |

## 未適用
- asia2 PostgreSQL `conf.d/96-log-params.conf`（`log_parameter_max_length = 256`）: スロークエリログを再び有効にするときだけ必要（07:12 にログ自体を止めたので今は不要）
- asia2 PostgreSQL `conf.d/99-wal.conf`（#234）: `wal_buffers 128MB`（再起動が要る）、`wal_writer_delay 10ms`、`wal_writer_flush_after 8MB`（reload で効く）。配置と戻し方はファイル冒頭
- isureal-go の GC 設定（#92）: `.claude/skills/infra-tuning/infra/systemd/isureal-go.service.d/gc.conf`（GOGC=200、GOMEMLIMIT=2GiB）。asia1 / europe1 / america1 に配置する。戻し方はファイル冒頭
- なし（C2 `tcp_tw_reuse=1` はベンチ後の TIME_WAIT を見て判断）

## 最終提出前（終了 -90 分）
- asia2: スロークエリログは 07:12 UTC に止めた（`log_min_duration_statement = -1`）。再び 100ms にしていたら -1 に戻す
- SKILL `newrelic-setup` の `toggle.sh off`
- 初手スナップショット: 4 台とも `/home/isucon/infra-backup/snapshot/`（03:03 UTC。02:33 の pg 99- と画像キャッシュを含む状態）
