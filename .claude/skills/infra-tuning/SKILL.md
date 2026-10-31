---
name: infra-tuning
description: ISUCON2026 の競技サーバーに「インフラ秘伝のタレ」を当てる。実機の構成を読み（inspect.sh）、カタログ（この SKILL の infra/catalog.md）と照らして項目ごとに提案し、人の OK を得てから 1 項目ずつ適用する（apply-item.sh）。初手スナップショットと疎通確認つきで、いつでも全部戻せる。「タレ入れて」「インフラチューニング」「nginx / MySQL / sysctl の設定見て」「DB 分離」「元に戻して」などで使う。
---

# インフラ秘伝のタレ（ISUCON 用）

本番のバージョンも構成も当日まで分からない。だから固定の設定ファイルは置かない。
**実機を読む → カタログと照らして提案 → 人が OK → 1 項目ずつ当てる → 疎通確認**、の順で進める。
ファイルはこの SKILL の `infra/`（`.claude/skills/infra-tuning/infra/`。カタログ・スクリプト・設定の断片）。サーバー側は `/home/isucon/infra/`（競技リポジトリに入れて `git pull` 後に `cp -r .claude/skills/infra-tuning/infra ~/` か、手元から `rsync -a --delete .claude/skills/infra-tuning/infra/ isucon@<IP>:/home/isucon/infra/`）。

## 守ること
- **何かを変える前に `snapshot.sh`**（各台 1 回。apply-item.sh は無ければ自動で取る）
- **構成は変えない**。既存ファイルは数値の書き換えだけ、新しい設定は追記ファイルだけ。カタログで ✕ の項目は人が明示したときだけ
- **1 項目ずつ**。各 apply の最後に出る `疎通 OK / NG` を必ず読む。NG ならその場で `rollback.sh -y <名前>`
- mysqld の restart はベンチ中にやらない。再起動のあとはアプリも再起動する（apply-item.sh が `ISU_APP_UNIT` で自動化）
- 値や項目を推測で決めない。`inspect.sh` の数値（RAM、既存値、ユーザーの host、待ち受けポート）を根拠にする
- 削除・停止・DROP など不可逆な操作は実行前に人に確認する

## 手順（各サーバーで。所要 15 分）

### 1. 読む
```bash
cd /home/isucon/infra/scripts
./snapshot.sh            # 初手スナップショット（既にあれば何もしない）
./inspect.sh             # 構成・既存値・ユーザー・ポート・疎通を 1 画面に
```
- `isucon.env` の `ISU_APP_UNIT` にアプリの systemd ユニット名を書く（inspect の「アプリの systemd ユニット候補」から）
- トップページが認証必須なら `ISU_CHECK_URL` を未ログインで 200 を返す URL にする

### 2. 提案する（Claude がやる）
`.claude/skills/infra-tuning/infra/catalog.md` を上から見て、台の役割（nginx-app / app / db / single）に関係する項目だけ、次の表で出す。

| # | 項目 | 現在値 | 提案値 | 根拠（inspect の値） | 判定 |
|---|---|---|---|---|---|
| B1 | innodb_buffer_pool_size | 128M | 1G | RAM 7.9GB の同居、データ 300MB | ○ 当てる |
| B9 | skip_name_resolve | OFF | - | isuconp@localhost のみ、アプリは TCP | ✕ 当てない（Error 1130 になる） |

- 「当てない」も理由つきで載せる（人が判断できるように）
- 既定 ○ の項目でも、カタログの「当てない条件」に当たれば提案しない
- 表を出して **人の OK を待つ**。OK の出た項目だけ 3. へ

### 3. 当てる
```bash
./apply-item.sh --dry-run mysql innodb_buffer_pool_size 1G    # 差分だけ見る
./apply-item.sh mysql innodb_buffer_pool_size 1G --no-restart # mysql は --no-restart で足していき
./apply-item.sh mysql innodb_flush_log_at_trx_commit 2 --no-restart
./apply-item.sh mysql max_connections 1000                   # 最後の 1 つで restart（validate → restart → アプリ再起動 → 疎通）
./apply-item.sh limits-unit mysql 65535
./apply-item.sh nginx-main worker_connections 8192
./apply-item.sh nginx-main worker_rlimit_nofile 65535
./apply-item.sh nginx-http keepalive_requests 10000
./apply-item.sh sysctl net.core.somaxconn 65535
./apply-item.sh nginx-measure on && ./apply-item.sh mysql-measure on   # 計測（alp / pt-query-digest 用）
```
- target の一覧と使い方は `apply-item.sh` のヘッダ（引数なしで実行すると出る）
- サイト設定（upstream keep-alive、パス振り分け、静的配信 = カタログ A8〜A10）は人が問題の設定に差分を足して `apply-site.sh <file>`。雛形は `nginx/sites-available/isucon.conf.example`
- 当てたら `docs/infra-applied.md`（競技リポジトリ）に「台 / 項目 / 値 / バックアップ名」を 1 行ずつ追記する。再起動試験前に何が入っているか一目で分かる

### 4. 確かめる
```bash
./inspect.sh                 # 値が変わったか
./bench-before.sh <memo>     # → ベンチ → ./bench-after.sh → cat ~/bench-logs/<run>/summary.txt
```
- 効果はベンチのスコアと、bench-after の resources（cpu / tcp_timewait / db_conn）、alp、pt-query-digest で見る
- NewRelic 側は SKILL `newrelic-nrql`

## 戻し方
```bash
./rollback.sh                       # apply 1 回ごとのバックアップ一覧
./rollback.sh -y <名前>             # その 1 回分だけ戻す（nginx は reload、MySQL は restart）
./restore-all.sh -y <アプリ unit>   # 初手スナップショットに丸ごと戻す（タレ全部 + 作ったユーザーも消す）
```

## 3 台構成（app / app / db + nginx のパス振り分け）
| 台 | 読む | 当てる候補（カタログ） | 構成の作業 |
|---|---|---|---|
| サーバー1 nginx + app | inspect | A1〜A7、A11、C1〜C8、`limits-unit <app>` | A8〜A10 を `apply-site.sh`（`app_light` least_conn ×2、`app_heavy` = サーバー2） |
| サーバー2 app | inspect | C1〜C8、`limits-unit <app>` | `set-db-host.sh` で DB をサーバー3 に |
| サーバー3 db | inspect | B1〜B8、`limits-unit mysql`、C1〜C8 | B13 `apply-item.sh mysql-main bind-address 0.0.0.0` → `split-db-grant.sql` → `check-links.sh` |

- 重い処理を `app_heavy` に寄せる判断は alp / NewRelic の endpoints（合計時間と CPU）で。サーバー2 だけオンメモリキャッシュを持たせる手もある

## 最終提出前（終了 -90 分）
- `./measure.sh off`（JSON ログ・スロークエリログ・NewRelic infra を止める）+ SKILL `newrelic-setup` の `toggle.sh off`
- 数値チューニングは残してよい（再起動後も残る）。`docs/infra-applied.md` を見て、構成に触った項目（✕）が無いか確認する

## ハマりどころ（2026-10-28 練習で確認）
- `skip_name_resolve` を入れると `user@localhost` の権限が TCP 接続に効かず `Error 1130`。アプリは 200 で空 body を返すので、疎通確認は「200 かつ body > 0」で見る（health_check がそう判定する）
- `rollback.sh <名前>` は表示だけ。戻すのは `-y`
- ssh 越しの `pkill -f <名前>` は自分のセッションを巻き込む。`pkill -f "[r]esource-sampler"` と書く
- `matching-groups.txt` が全行コメントだと旧 bench-after が無言で落ちた（修正済み。取れた分で summary.txt を出す）
- ip_local_port_range の下限は下げない（11211 / 33060 と重なる）
