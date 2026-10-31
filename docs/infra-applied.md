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
| 10-31 07:45 | asia1 / europe1 / america1 | nginx `access_log off`（`apply-item.sh nginx-http access_log off`。nginx.conf の http 文脈の行を書き換え、reload） | access.log への書き込み停止（asia1 は 239MB まで育っていた） | `rollback.sh -y 20261031-074515-nginx`（asia1 / europe1）/ `20261031-074516-nginx`（america1） |
| 10-31 07:45 | asia1 / europe1 / america1 | `isureal-go.service.d/loglevel.conf`（`Environment=ISUREAL_LOG_LEVEL=error`。daemon-reload 済み、**次の restart / deploy から効く**） | アクションログを 5xx だけに（asia1 で 5 分 1.3 万行 → ほぼ 0。IP BAN はアプリ内判定なので journald は読まれていない） | `sudo rm /etc/systemd/system/isureal-go.service.d/loglevel.conf && sudo systemctl daemon-reload && sudo systemctl restart isureal-go` |
| 10-31 07:45 | asia2 | PostgreSQL `conf.d/97-log-off.conf`（reload 済み） | `log_checkpoints = off` | `sudo rm /etc/postgresql/16/main/conf.d/97-log-off.conf && sudo systemctl reload postgresql` |
| 10-31 07:45 | asia2 | PostgreSQL `conf.d/99-wal.conf`（#234 / PR #254。reload 済み） | `wal_writer_delay 10ms`・`wal_writer_flush_after 8MB` は反映済み。`wal_buffers 128MB` は **pending_restart**（再起動試験の reboot で効く） | `sudo rm /etc/postgresql/16/main/conf.d/99-wal.conf && sudo systemctl restart postgresql` |
| 10-31 07:52 | asia1 | `env.sh` に `ISUREAL_IMAGE_CACHE_MB=512`（#260 の意図。RSS が 2.7GB まで伸びていた）+ isureal-go restart（loglevel.conf もこの restart で有効化） | 起動直後 RSS 44MB、`GET /` 200、journald 10 秒で 0 行 | `env.sh.bak-20261031-imgcache` に戻して restart（env.sh の中身は表示しない） |
| 10-31 08:10 | asia2 | PostgreSQL `conf.d/99-zz-pss-off.conf`（reload 済み。`99-isucon-tuning.conf` の `track = all` より後に読ませるため名前を 99-zz に） | `pg_stat_statements.track = none`（最終提出前。全クエリ統計の CPU / lock のオーバーヘッドを外す） | `sudo rm /etc/postgresql/16/main/conf.d/99-zz-pss-off.conf && sudo systemctl reload postgresql` |

## 未適用
- asia2 PostgreSQL `conf.d/96-log-params.conf`（`log_parameter_max_length = 256`）: スロークエリログを再び有効にするときだけ必要（07:12 にログ自体を止めたので今は不要）
- asia2 PostgreSQL `conf.d/99-wal.conf`（#234）: 07:45 に配置・reload 済み。`wal_buffers 128MB` だけ PostgreSQL の restart 待ち（上の表）
- isureal-go の GC 設定（#92 / PR #97 はマージ済みだが未配置）: `gc.conf`（GOGC=200、GOMEMLIMIT=2GiB）。**asia1 には今のままでは当てない**（07:40 時点で isureal の RSS 2.4GB・HWM 2.5GB。GOMEMLIMIT=2GiB だと GC が回り続けて CPU を食う。GOGC=200 はヒープを増やす方向で OOM に近づく）。europe1 / america1 は RSS 600MB で当てられるが CPU は暇なので効果は小さい
- asia2 `max_wal_size`（99- で 4GB）・`checkpoint_timeout`（15min）: `checkpoints_req 42 > checkpoints_timed 32` で WAL 量起因のチェックポイントが多い。pg_wal は 4.1GB。07:46 のベンチで WAL は 1.8GB、WAL 起因のチェックポイントは増えなかった（req 42 のまま）ので当てていない
- rsyslog（4 台）: journald の転送は止めてあるので syslog への書き込みは systemd のセッション行だけ（毎分数行）。止める利益が小さいので触っていない
- なし（C2 `tcp_tw_reuse=1` はベンチ後の TIME_WAIT を見て判断）

## 最終提出前（終了 -90 分）
- asia2: スロークエリログは 07:12 UTC に止めた（`log_min_duration_statement = -1`）。再び 100ms にしていたら -1 に戻す
- nginx access_log・アプリのアクションログ・log_checkpoints は 07:45 UTC に止めた（上の表）。戻さなくてよい
- SKILL `newrelic-setup` の `toggle.sh off`: **08:04 UTC に実施済み**（4 台。Go agent は ENABLED=false で restart、infra agent は disable --now。asia2 では toggle.sh が isureal-go を起動してしまうので stop し直した。envcheck 4 台 passed）
- 初手スナップショット: 4 台とも `/home/isucon/infra-backup/snapshot/`（03:03 UTC。02:33 の pg 99- と画像キャッシュを含む状態）
