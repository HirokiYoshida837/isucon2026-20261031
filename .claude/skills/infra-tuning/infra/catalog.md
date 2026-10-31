# チューニングカタログ（インフラ秘伝のタレ）

当日はこのカタログを上から順に見て、`inspect.sh` の結果と照らして「当てる / 当てない」を決める。
値はここに書いた「決め方」で、実機の RAM・CPU・既存値から出す。固定の設定ファイルは置かない。
適用は `apply-item.sh`（1 項目 1 コマンド。差分表示 → バックアップ → 反映 → 疎通確認）。

凡例
- 対象: どのファイル・どの文脈に書くか。`apply-item.sh` の `<target>` に対応する
- 既定: ○ = 当日まず当てる候補、△ = 計測して効きそうなら、✕ = 構成に触るので原則当てない（opt-in）
- 当てない条件: ここに当たるものは提案しない

---

## A. nginx（サーバー1）

### A1. worker_connections ○
- 対象: `nginx-main`（`/etc/nginx/nginx.conf` の events { } の既存行を数値変更）
- 決め方: 8192（1 ワーカーあたり。Ubuntu 既定 768）
- なぜ: ベンチの同時接続 + upstream への接続がワーカーごとにこの数で頭打ちになる
- 当てない条件: 既に 4096 以上
- 確認: `nginx -T | grep worker_connections`

### A2. worker_rlimit_nofile ○
- 対象: `nginx-main`（main 文脈。無ければ worker_processes の次に 1 行追記）
- 決め方: 65535
- なぜ: worker_connections を上げても fd 上限で止まる
- 確認: `cat /proc/$(pgrep -o nginx)/limits | grep "open files"`

### A3. keepalive_requests ○
- 対象: `nginx-http`（既存行があれば数値変更、無ければ conf.d に追記）
- 決め方: 10000（既定 1000）
- なぜ: ベンチャー → nginx の接続が 1000 リクエストごとに切れて再接続する。上げると TIME_WAIT とハンドシェイクが減る
- 確認: ベンチ中の `tcp_timewait`（bench-after の resources）

### A4. keepalive_timeout △
- 対象: `nginx-http`
- 決め方: 65（Ubuntu 既定と同じなら触らない）
- 当てない条件: 既に 60 以上

### A5. open_file_cache ○（静的ファイルを nginx が返す問題）
- 対象: `nginx-http`（4 行: `open_file_cache max=10000 inactive=60s;` `open_file_cache_valid 60s;` `open_file_cache_min_uses 1;` `open_file_cache_errors off;`）
- なぜ: 静的ファイルの open / stat を省く
- 当てない条件: アプリが実行中に書き出すファイル（画像等）を nginx が配る場合は `open_file_cache_valid` を短く（5s）するか当てない。古い内容を返す
- 確認: 静的ファイルの応答時間（alp の `/css/` 等）

### A6. proxy_buffers / proxy_buffer_size △
- 対象: `nginx-http`（`proxy_buffers 64 16k;` `proxy_buffer_size 16k;`）
- なぜ: 大きいレスポンス（HTML 数十 KB）をディスクの一時ファイルに落とさない
- 当てる条件: ベンチ後に `error.log` の `an upstream response is buffered to a temporary file` が 0 件より多い（inspect の判断用カウンタ）。0 なら不要

### A7. gzip_comp_level ✕（不要）
- nginx の既定値が既に 1（最速）。`gzip on` のままなら触るところがない。`gzip off` の問題は、ベンチャーが gzip を要求するか見てから `gzip on` を検討

### A8. upstream keep-alive ○（サイト設定 → SKILL `nginx-site`）
- 対象: `site`。問題のサイト設定に差分を足す作業は SKILL `nginx-site` の手順で（雛形 `nginx/sites-available/isucon.conf.example`、配置 `apply-site.sh <file>`）
- 内容: `upstream app { server 127.0.0.1:8080; keepalive 64; keepalive_requests 10000; }` + location で `include /etc/nginx/isucon/proxy_keepalive.conf;`（`proxy_http_version 1.1` と `Connection ""`）
- なぜ: nginx → アプリの接続を毎リクエスト張り直さなくなる。ポート枯渇と遅延が減る
- 確認: ベンチ中の `tcp_timewait` が減る。アプリ側の accept 数

### A9. パス振り分け（3 台構成）○（→ SKILL `nginx-site`）
- 対象: `site`
- 内容: `upstream app_light { least_conn; サーバー1; サーバー2; }` と `upstream app_heavy { サーバー2; }`。重いエンドポイントの location だけ `app_heavy` へ
- 決め方: alp / NewRelic の endpoints で合計時間が大きく CPU を使うものを `app_heavy` に
- なぜ: 重い処理を 1 台に寄せると、その台だけオンメモリキャッシュを持てる。軽い処理は 2 台で捌く

### A10. 静的ファイルを nginx から返す ○（→ SKILL `nginx-site`）
- 対象: `site`
- 内容: `location ~ ^/(css|js|img|assets)/ { expires 1d; add_header Cache-Control "public"; access_log off; try_files $uri =404; }`
- 当てない条件: 拡張子付き URL をアプリが返す問題では拡張子で絞らない（ディレクトリで絞る）
- 確認: alp で静的ファイルのリクエストがアプリに行っていない

### A11. alp 用 JSON ログ ○（計測。性能には効かない）
- 対象: `nginx-measure`（`apply-item.sh nginx-measure on`。conf.d に `log_format json` と `include /etc/nginx/isucon/access_log.conf` を追記。問題側の access_log は触らない）
- なぜ: bench-after.sh の alp がこのログを読む
- 最終提出前: `measure.sh off`

### A12. http2 △
- 対象: `site`（`listen 443 ssl http2;`）
- 当てない条件: ベンチャーが HTTP/1.1 のみ（当日マニュアルで確認）。害は無いが効果も無い

---

## B. MySQL（サーバー3）

### B1. innodb_buffer_pool_size ○
- 対象: `mysql`（`/etc/mysql/conf.d/99-isucon.cnf` に追記。mysqld restart）
- 決め方: 同居 = 1G（RAM 4GB の 25%）。DB 専用 = 2G（RAM 4GB の 50%。OS と infra agent 50MB を残す）。RAM 8GB なら倍
- 増やすかの判断: 初手ベンチの後に `inspect.sh` の「判断用カウンタ」で `buffer_pool_pages_data`（実際に載ったデータ量）を見る。pool を使い切っていなければ増やさない（private-isu は 1.2GB のデータに対し 45MB しか載らなかった。大半が画像 BLOB でベンチが触らない）
- なぜ: データとインデックスをメモリに載せる。DB 律速の問題で一番効く
- 当てない条件: 既に RAM の 50% 以上
- 丸め: 1G 以上を指定すると `innodb_buffer_pool_chunk_size（128M）× instances（8）= 1G` 単位に切り上がる（1536M → 2G、2560M → 3G）。1G / 2G のように 1G 刻みで指定する
- 確認: `SHOW VARIABLES LIKE 'innodb_buffer_pool_size'`。ベンチ中の `mysqld` RSS と `cpu_iowait`

### B2. innodb_flush_log_at_trx_commit = 2 ○
- 対象: `mysql`
- なぜ: コミットごとの fsync をやめて 1 秒ごとにまとめる。書き込みが多い問題で大きく効く
- 副作用: OS クラッシュ時に最後の 1 秒分を失う可能性（ISUCON では許容。再起動試験は正常停止なので失わない）
- 確認: ベンチ中の `disk_w_kBs` と `cpu_iowait` が減る

### B3. max_connections ○
- 対象: `mysql` + `limits-unit mysql`（LimitNOFILE の drop-in。max_connections を上げるため）
- 決め方: 1000（既定 151）。アプリのコネクションプール上限 × アプリ台数 + 余裕
- なぜ: `Too many connections` を出さない
- 確認: ベンチ中の `db_conn` max が上限に張り付いていない

### B4. innodb_redo_log_capacity ○（MySQL 8.0.30+）/ innodb_log_file_size（MariaDB）
- 対象: `mysql`
- 決め方: 1G
- なぜ: チェックポイントの頻度が減り、書き込みの山をならせる
- 当てない条件: MySQL 8.0.29 以前は項目名が違う（`innodb_log_file_size`）。`--validate-config` で弾かれる

### B5. innodb_log_buffer_size △
- 対象: `mysql`。決め方: 64M。なぜ: 大きなトランザクションでディスクに書く回数が減る
- 当てる条件: ベンチ後に `Innodb_log_waits` が 0 より大きい（inspect の判断用カウンタ）。0 なら不要

### B6. innodb_io_capacity / innodb_io_capacity_max △
- 対象: `mysql`。決め方: 2000 / 3000（SSD 前提）
- なぜ: バックグラウンドのフラッシュを遅らせない
- 当てない条件: ディスクが gp2 等で IOPS が低い（当日の EBS 種別を確認）。ベンチ後に `Innodb_buffer_pool_wait_free = 0` かつ iowait 1% 未満なら不要

### B7. tmp_table_size / max_heap_table_size △
- 対象: `mysql`。決め方: 64M（両方同じ値）
- なぜ: GROUP BY / ORDER BY の一時テーブルをメモリで処理する
- 当てる条件: ベンチ後に `Created_tmp_disk_tables` が増えている（inspect の判断用カウンタ）。0 なら不要
- 確認: `SHOW GLOBAL STATUS LIKE 'Created_tmp_disk_tables'` が増えない

### B8. スロークエリログ △（opt-in。NewRelic で SQL は見えるので既定は OFF）
- 対象: `mysql-measure`（`apply-item.sh mysql-measure on`。`99-isucon-slowlog.cnf` + `SET GLOBAL`）
- 使うとき: NewRelic に無い `Rows_examined`（index が効いているか）を pt-query-digest で見たいとき。「index を貼ったのに遅い」の調査用
- 注意: `long_query_time=0` は全クエリを書くので書き込み負荷とディスクを食う。見終わったら `mysql-measure off`。`long_query_time` は新しい接続から効く（アプリ再起動）

### B9. skip_name_resolve ✕（opt-in）
- 対象: `mysql`
- なぜ: 接続ごとの逆引きを止める
- **事前チェック**: `SELECT user, host FROM mysql.user`。アプリ用ユーザーが `@localhost` だけで、アプリが TCP（127.0.0.1）で繋ぐなら `Error 1130` で繋がらなくなる（2026-10-28 練習で発生。アプリは 200 で空 body を返す）
- 当てるなら: 先に `mysql-add-tcp-users.sh` で `@127.0.0.1` を同じ権限で作る
- 効果は小さい（ローカル接続の逆引きは速い）。DB 分離して他サーバーから繋ぐときだけ検討

### B10. disable_log_bin ○（MySQL 8 のみ。2026-10-28 すー判断で既定に）
- 対象: `mysql`（値なし。`apply-item.sh mysql disable_log_bin`）。MariaDB は既定で無効なので不要
- なぜ: MySQL 8 は既定でバイナリログ ON で、コミットごとに binlog 書き込み + fsync（`sync_binlog=1`）が入る。B2 で redo の fsync を消しても binlog の fsync が残る。止めると書き込みが重い問題で 1〜3 割効くことがある
- 副作用: レプリケーション・PITR が使えない（ISUCON では使わない）。mysqld の再起動が要る
- 事前チェック: `log_bin` が ON のときだけ意味がある（inspect の値）
- 代案: 止めたくないなら `sync_binlog = 0`（数値変更だけで fsync が消える。binlog 自体は残る）

### B11. innodb_flush_method = O_DIRECT ✕（opt-in）
- なぜ: OS キャッシュとの二重バッファを避ける。buffer pool を大きくしたときに効く
- 副作用: 小さい buffer pool だと OS キャッシュが使えず遅くなることがある

### B12. performance_schema = OFF ✕（opt-in）
- なぜ: 数百 MB のメモリが空く
- 副作用: NewRelic の DB 詳細や `sys` スキーマが使えない

### B13. bind-address = 0.0.0.0 ✕（DB 分離時に必要）
- 対象: `mysql-main`（標準の `mysqld.cnf` / `50-server.cnf` の既存行を値変更。conf.d に二重に書かない）
- セット: `split-db-grant.sql`（VPC 帯からのユーザー）、`check-links.sh <DB IP>:3306`、アプリ側 `set-db-host.sh`

---

## C. カーネル・limits（全台）

### C1. somaxconn / tcp_max_syn_backlog ○
- 対象: `sysctl`（`/etc/sysctl.d/99-isucon.conf` に追記。即時反映）
- 決め方: 65535 / 65535
- なぜ: listen のバックログ。nginx / アプリの backlog がこの値で頭打ち
- 当てない条件: 既に 4096 以上で、ベンチ中に `SYNs to LISTEN sockets dropped`（`nstat -az TcpExtListenDrops`）が増えていない

### C2. tcp_tw_reuse = 1 ○
- 決め方: 1（既定 2 = ループバックのみ）
- なぜ: nginx → アプリ、アプリ → DB の短命接続で TIME_WAIT を再利用
- 当てない条件: upstream keep-alive（A8）が入っていて TIME_WAIT が少ない

### C3. tcp_fin_timeout / tcp_max_tw_buckets △
- 決め方: 10 / 2000000。なぜ: TIME_WAIT を早く解放し、上限で落とさない
- 当てる条件: ベンチ中の `tcp_timewait` max（bench-after）が `tcp_max_tw_buckets` の半分を超える、または `TcpExtTCPTimeWaitOverflow > 0`

### C4. netdev_max_backlog △
- 決め方: 16384。なぜ: NIC から受けたパケットの待ち行列
- 当てる条件: `/proc/net/softnet_stat` の 2 列目（dropped）が 0 より大きい（inspect の判断用カウンタ）。0 なら不要

### C5. tcp_slow_start_after_idle = 0 △
- なぜ: アイドル後に輻輳ウィンドウを縮めない（keep-alive 再開が速い）

### C6. fs.file-max / fs.nr_open △
- 決め方: 2097152。なぜ: 接続 1 本 = fd 1 本
- 当てない条件: 既定（Ubuntu 24.04 は 9223372036854775807 / 1048576）で十分なことが多い。nofile を 65535 にするなら nr_open はそれ以上あればよい

### C7. vm.swappiness = 1 △
- なぜ: DB のバッファをスワップに出さない
- 当てない条件: swap が無い（`free -m` の Swap が 0）

### C8. nofile 65535 ○
- 対象: `limits`（`/etc/security/limits.d/99-isucon-limits.conf`）と `limits-unit <app>`（systemd drop-in `LimitNOFILE=65535`）
- なぜ: アプリ・DB の fd 上限。新しいログインと restart から効く
- 確認: `systemctl show <unit> -p LimitNOFILE`

### C9. ip_local_port_range ✕（当てない）
- 下限を下げると 11211（memcached）/ 33060（mysqlx）等の待ち受けポートと重なる。既定 32768〜60999 と C2 で足りる

---

## D. 構成（スクリプト化しているもの）
- D1. DB 分離: `apply-item.sh mysql-main bind-address 0.0.0.0` → `split-db-grant.sql` → `check-links.sh` → `set-db-host.sh`
- D2. アプリ 2 台 + パス振り分け: A9 のサイト設定
- D3. 計測の ON/OFF: `measure.sh on|off`（A11 + B8 + NewRelic infra をまとめて）
- D4. 全部戻す: `restore-all.sh`。1 回分戻す: `rollback.sh -y <名前>`

---

## C. OS（ISUReal 当日追記）
- 道具: `scripts/os-quiet.sh`（`--dry-run` / `--restore`）。4 台すべてで実行。設定は `sysctl/91-isucon.conf`（`/etc/sysctl.d/91-isucon.conf` に install、主催者の `90-isucon.conf` は触らない）
- 止めるもの: `apt-daily(-upgrade).timer`、`fwupd-refresh.timer`、`motd-news.timer`、`update-notifier-*.timer`、`unattended-upgrades`、`ModemManager`、`multipathd`、`udisks2`
- なぜ: `apt-daily-upgrade.timer` が 15:42 JST（競技中）に発火予定。常駐の RSS 約 200MB。ベンチ中の CPU と再起動試験の邪魔を減らす
- sysctl: `tcp_max_syn_backlog 256 → 8192`、`netdev_max_backlog 1000 → 8192`、`tcp_fin_timeout 60 → 10`
- 触らない: `amazon-ssm-agent`、`isucon-register`、`envcheck*`、`chrony`、`ssh`、`nginx`、`postgresql`、`isureal-*`、`newrelic-infra`
- 確認: 実行末尾の `sudo /usr/local/bin/envcheck`、`systemctl list-timers`
- 戻し方: `os-quiet.sh --restore`（サービスを enable、timer は `--now`、sysctl ファイルを削除して `sysctl --system`）
