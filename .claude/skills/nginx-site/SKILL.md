---
name: nginx-site
description: ISUCON2026 の nginx サイト設定（sites-available）に、upstream keep-alive・静的ファイルの nginx 配信・3 台構成のパス振り分け（軽い処理は least_conn で 2 台、重い処理はサーバー2 固定）を足す。問題の設定を読んで差分を作り、人の OK 後に apply-site.sh で配置して効果を確認する。「nginx のサイト設定」「静的ファイルを nginx で返す」「keep-alive 入れて」「重い処理を 2 台目に振る」「upstream 分けて」などで使う。
---

# nginx サイト設定（ISUCON 用）

サイト設定は問題ごとに違う（証明書・server_name・ポート・root・URL）。だから雛形をそのまま置かず、**問題の設定を読んで差分だけ足す**。
数値チューニング（worker_connections 等）は SKILL `infra-tuning`。ここは「構成に触る」側なので、必ず差分を見せて OK をもらってから配置する。

## 入れるもの（効く順）
| # | 内容 | なぜ効くか | 確認 |
|---|---|---|---|
| 1 | upstream keep-alive（`upstream app { keepalive 64; }` + location で `include /etc/nginx/isucon/proxy_keepalive.conf;`） | nginx → アプリの TCP 接続を毎リクエスト張り直さない。TIME_WAIT とハンドシェイクが消える | ベンチ中の `tcp_timewait`（bench-after）が減る。`ss -tn state established '( dport = :8080 )'` の本数が安定 |
| 2 | 静的ファイルを nginx から返す（`location ~ ^/(css|js|img|assets)/ { try_files $uri =404; expires 1d; access_log off; }`） | アプリ（Go）の CPU とコネクションを使わない | alp に `/css/` 等が出なくなる。`curl -sI` で `Cache-Control` が付く |
| 3 | パス振り分け（`upstream app_light { least_conn; サーバー1; サーバー2; }` `upstream app_heavy { サーバー2; }`。重い location だけ `app_heavy`） | 重い処理を 1 台に寄せ、その台だけオンメモリキャッシュを持てる。軽い処理は 2 台で捌く | alp / NewRelic でサーバー別の CPU（`hosts`）が均される |
| 4 | `client_max_body_size`、`http2`（`listen 443 ssl http2;`）は問題の値を尊重。足りないときだけ | - | - |

## 手順

### 1. 読む（nginx の台）
```bash
cd /home/isucon/infra/scripts && ./inspect.sh | sed -n '/== nginx/,/== MySQL/p'
sudo cat /etc/nginx/sites-enabled/*          # 元の設定（全文）
ls <問題の public ディレクトリ>               # 静的ファイルのディレクトリ名（css / js / img / assets ...）
```
- アプリが返す「拡張子付き URL」が無いか（例: private-isu の `/image/{id}.jpg`、isucon14 の `/api/.../icon`）。あれば静的 location は**ディレクトリ名で**絞る（拡張子で絞ると吸ってしまう）
- アプリの待ち受け（`-bind 127.0.0.1:8080` 等）と、2 台目のプライベート IP（`inspect` の `private_ip`）
- 重いエンドポイントの候補: SKILL `newrelic-nrql` の `endpoints`、または bench-after の alp（SUM が大きく、アプリ側 CPU を使うもの）

### 2. 差分を作る
- 元ファイルをリポジトリにコピー: `cp /etc/nginx/sites-enabled/<name>.conf <repo>/.claude/skills/infra-tuning/infra/nginx/sites-available/<name>.conf`
- 雛形 `.claude/skills/infra-tuning/infra/nginx/sites-available/isucon.conf.example` を見て、**元ファイルに**次を足す（置き換えない）
  1. `upstream app` または `app_light` / `app_heavy`（server 行に実 IP:ポート、`keepalive 64; keepalive_requests 10000; keepalive_timeout 60s;`）
  2. 既存の `proxy_pass http://localhost:8080;` → `include /etc/nginx/isucon/proxy_keepalive.conf; proxy_pass http://app;`（既存の `proxy_set_header Host` は include に入っているので消してよい）
  3. 静的ファイルの location（ディレクトリ名で絞る。`access_log off` で alp のノイズを消す）
  4. 3 台のときだけ: 重い location を `app_heavy` へ。具体的なパスを先、`/` や `/api/` を後に書く
- `./apply-site.sh --dry-run <file>` で差分を出し、表にして人に見せる。OK を待つ

### 3. 配置して確かめる
```bash
./apply-site.sh <repo>/.claude/skills/infra-tuning/infra/nginx/sites-available/<name>.conf    # nginx -t → reload → 疎通確認
curl -sI http://localhost/css/style.css | grep -iE 'HTTP|cache-control|content-type'   # 静的が nginx から返る
curl -s -o /dev/null -w '%{http_code} %{size_download}\n' http://localhost/            # アプリ経由が 200 かつ body あり
curl -s -o /dev/null -w '%{http_code}\n' http://localhost/image/1.jpg                   # アプリが返す拡張子付き URL が 404 になっていない
```
- ベンチを回して bench-after の `tcp_timewait` と alp を前回と比べる
- NG なら `./rollback.sh -y <バックアップ名>`

## 3 台構成の雛形（isucon.conf.example の要点）
```nginx
upstream app_light { least_conn; server 127.0.0.1:8080; server <サーバー2 IP>:8080; keepalive 64; keepalive_requests 10000; keepalive_timeout 60s; }
upstream app_heavy { server <サーバー2 IP>:8080; keepalive 64; keepalive_requests 10000; keepalive_timeout 60s; }

location = /api/<heavy>   { include /etc/nginx/isucon/proxy_keepalive.conf; proxy_pass http://app_heavy; }
location /api/            { include /etc/nginx/isucon/proxy_keepalive.conf; proxy_pass http://app_light; }
```
- サーバー2 の 8080 に外から届くこと（`./check-links.sh <サーバー2 IP>:8080`。アプリが `127.0.0.1` にしか bind していなければ `0.0.0.0` に変える）
- 重い処理をサーバー2 に寄せたら、そのエンドポイントのオンメモリキャッシュはサーバー2 だけで完結する（他の台と整合を取らなくてよい）

## 落とし穴
- 静的 location を拡張子で書くと、アプリが返す `/image/123.jpg` のような URL を吸って 404 になる。ディレクトリで絞る
- `try_files $uri /index.html;` は SPA のフロント用。API だけの問題で `/` に置くと全部 index.html になる
- `proxy_keepalive.conf` は `proxy_http_version 1.1` と `Connection ""` を入れる。upstream 側の `keepalive` と**両方**無いと効かない
- `open_file_cache`（infra-tuning A5）を入れていて、アプリが実行中に画像を書き出す問題では、古い内容を返すことがある。`open_file_cache_valid` を短くする
- 配置したファイルはリポジトリにコミットする（当日の履歴と revert のため）
