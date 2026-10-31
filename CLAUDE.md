# CLAUDE.md — ISUReal（ISUCON2026）

## これは何か
- ISUCON の競技リポジトリ。目的はベンチのスコアだけ。可読性・リファクタ・テストは要らない
- 優先順位: レギュレーション → ベンチ PASS → スコア
- 競技時間: 10:00〜18:00（JST）。16:30 に再起動試験、17:10 に掃除、17:45 以降は触らない。18:00 以降はサーバーに接続しない（失格）
- 言語: Go（`go/`。サーバーでは `/home/isucon/webapp/go`）。前段 nginx、DB は PostgreSQL 16

## 進め方
- 計測 → 改善 1 つ → ブランチ → PR → デプロイ → ベンチ → 採用 or revert。これを回す
- 1 改善 = 1 ブランチ（`<担当>/<内容>`）= 1 PR。ベンチは待たずに次の改善へ進んでよい。差分は revert できる大きさ（目安 100 行。超えるなら分割案を出して止まる）
- 計測値（NewRelic / alp / PostgreSQL のスロークエリログ）を根拠に選ぶ。関係ない整形・リファクタはしない
- 下がった・FAIL した変更は main に残さない
- 1 セッション 1 改善。各段階の貼るプロンプトは `.claude/skills/harness-setup/prompts/`。PR 本文は `.github/pull_request_template.md`

## 守ること
- レギュレーション違反は失格。要点は `docs/regulation.md`、原文は `docs/sources/`。食い違うときは当日マニュアルを優先する。迷ったら人に聞く
- 変えない: URI、レスポンスの内容と形式、ステータスコード、フロントの表示と構造、配布された静的ファイル、ベンチが検証する値の仕様、インスタンス構成
- 変えない（追試で失格）: `isucon-register.service`、`isuadmin`、`envcheck` 関連ファイル、`isucon` 以外のユーザーと公開鍵、`admin` アカウント、セキュリティグループの 22・443・8080
- しない: 競技サーバー外への処理の委譲（計測・開発用途は可）、ベンチマーカーへの干渉、競技内容の外部公開、ルーターへのログインと設定変更、ルーターを迂回する通信、リージョン・遅延・帯域制限の変更や回避
- コードを書く前に 5 点を 1 行ずつ答えて見せる: ①レスポンスは変わらないか ②初期化で戻るか ③再起動後も動き、書いたデータは残るか ④猶予時間に収まるか ⑤禁止事項に触れないか。怪しければ止まる

## ベンチを落とさない
- FAIL はスコア 0。最後のベンチが FAIL なら最終スコアも無い。条件は `docs/scoring.md`
- 壊しやすいもの: 古い値を返すキャッシュ（無効化・複数 app・初期化で消えない）、初期化で戻らない index・テーブル・メモリ状態、トランザクション境界の変更、非同期化で書き込みが遅れる・順序が変わる、初期化の時間超過
- LIMIT・並び順の変更は、ベンチが検証する範囲を確認してから入れる
- テーブル・列を足したら `sql/reset.sql` に戻し方を足す。キャッシュと複製先は初期化で作り直す
- `sql/001-schema.sql` を流さない（全テーブルを DROP する。初期データの SQL は配布に無く、消すと戻せない。`docs/schema.md`）
- ビルドと `go vet ./...` が通ってからデプロイする

## 再起動で壊さない
- 追試は再起動 → 環境チェック → ベンチ → 再起動 → データと表示の確認。手順は `docs/restart-test.md`。スコアが最終スコアの 75% 以下の結果が 3 回続くと失格
- 使うサービスは全部 enabled、使わないものは disable（主催者のサービスは触らない）
- 設定を変えたら `sudo /usr/local/bin/envcheck` で確かめる。4 台とも起動した状態で終える
- アプリは DB より後に起動しても動くようにする
- 書いたデータは再起動後も読める。メモリキャッシュは消える前提（初期化か起動時に復元）。非同期書き込みはベンチ終了前に flush
- 設定はファイルに書く（`sysctl -w` だけでは消える）。PostgreSQL の永続性設定を変えるならデータ消失を一考

## 秘密情報
- キー・Webhook・ポータル情報・CloudFormation は、コード・PR・チャットに書かない。env ファイルの中身は表示しない
- リポジトリは private のまま。NewRelic は競技用アカウント（別組織の NewRelic MCP は使わない）

## 環境
- サーバー: `~/.config/isucon/isucon.env` の `ISU_HOSTS`。IP は下の表を使い、食い違うときはポータルのサーバリストを優先する
- 役割: asia1 nginx+app（asia）/ europe1 nginx+app（europe）/ america1 nginx+app（america）/ asia2 PostgreSQL（asia）/ ルーター（SSH 不可）

| ポータル名 | 役割 | Public IP | Private IP | SSH |
|---|---|---|---|---|
| asia1 | App（c5.large） | 54.248.15.35 | 192.168.1.10 | `ssh isucon@54.248.15.35` |
| europe1 | App（c7i.large） | 52.194.151.75 | 192.168.2.10 | `ssh isucon@52.194.151.75` |
| america1 | App（c7i.large） | 54.248.249.30 | 192.168.3.10 | `ssh isucon@54.248.249.30` |
| asia2 | DB（c5.large） | 54.248.122.216 | 192.168.4.10 | `ssh isucon@54.248.122.216` |

- アプリは `isureal-go.service`（`EnvironmentFile=/home/isucon/env.sh`）。`env.sh` にリージョンと DB 接続情報がある
- 初期化: `POST /api/initialize`。初期化で流す SQL: `sql/init.sh` → `sql/reset.sql`
- ビルド: `cd go && make isureal`（go は `/home/isucon/local/go/bin`。非対話 ssh では PATH に足す）
- デプロイ・ベンチ: SKILL `bench`（`deploy.sh <branch>` → `bench-start.sh` → ポータル https://portal.isucon.net/contestant でベンチ → `bench-end.sh` → `bench-report.sh`）
- 計測: SKILL `newrelic-nrql`。秘伝のタレ: SKILL `infra-tuning`（サーバーの `/home/isucon/infra/`）。nginx: SKILL `nginx-site`。agent の ON/OFF: SKILL `newrelic-setup`
- NewRelic の Go agent は入れたら外さない。止めるのは `newrelic-setup` の `toggle.sh off`

## ローカルからサーバーを操作する
- 前提: 自分の GitHub 登録済みの鍵でサーバーに入れる。リポジトリの collaborator になっている
- `~/.ssh/config` に 4 台分を足す。Host 名は `asia1` `europe1` `america1` `asia2`、`HostName` は上の表の Public IP、`User isucon`、`IdentityFile` は自分の鍵
- 接続確認は `ssh asia1 hostname`。非対話でコマンドを付けて実行してよい
- 4 台とも `~/webapp` がこのリポジトリ（`main`）の作業コピー。gh は導入済みで認証済み
- サーバー上の git 更新は `git pull` ではなく `gh repo sync` を使う（`ssh asia1 'cd ~/webapp && gh repo sync --branch main'`）
- 手元ではブランチを切って PR にする。サーバーへの反映は PR のマージ後か、`REF` を指定したデプロイで行う
- 各サーバーの `~/Makefile` は `~/webapp/Makefile` に転送するだけの wrapper。git 管理しないので、無ければ各自が ssh で置く（中身は下）
```
.DEFAULT_GOAL := all.restart
Makefile: ;
%:
	@$(MAKE) --no-print-directory -C $(HOME)/webapp $@
```
- `%:` の下のレシピ行はタブインデント
- サーバーの `~` で打つコマンド
  - `make deploy REF=<ブランチ名>`: ブランチをデプロイ（ビルドと再起動）
  - `make deploy REF=<PR 番号>`: PR をデプロイ
  - `make isureal.logs` / `make nginx.logs` / `make postgres.logs`: ログを追う
  - `make nginx.reload`: nginx の設定を反映
  - `make all.restart`: 全サービスの再起動（引数なしの `make` も同じ。人の OK を待つ）
- restart は、そのサーバーで enabled のサービスだけが対象。asia2 でアプリは起動しない
- デプロイ・再起動・サーバー上の削除は「止まるところ」の対象。人の OK を待つ

## この問題の注意
- リージョンは固定。利用者は自分のリージョンのサーバーにしか届かない（`RegionMiddleware` は違うと 403）。スコア倍率は europe・america が 1.5
- 通信はルーター経由で、リージョン間に遅延と帯域制限がある。asia2 の DB へは europe1・america1 から遅延つきで届く
- 書き込み後の読み取りは、リージョンをまたいでも結果を反映する。メモリキャッシュや複製はここで壊しやすい
- 猶予があるのは削除した投稿の画像だけ（削除成功から 5 秒）。公開範囲・友達関係・ブロックの反映に猶予は無い
- 画像 URL の署名・期限の検証結果を変えない。nginx で画像を返すなら検証と 403 を保つ
- クライアント IP は `X-Client-IP` を使う。ベンチはリダイレクトを追わず、503 は再試行するがエラー率と応答時間には含まれる
- `GET /api/notifications` は SSE。バッファリングやタイムアウトに注意する
- 初期化は空でない `language` を返す。AI で最適化したので `agent`・`harness`・`model` を使ったものに書き換える（`go/initialize.go`。得点と合否には無関係）

## SKILL の読み替え（prep は MySQL・3 台を前提に書かれている）
- `infra-tuning`・`db-migrate-setup`・`makefile-setup`・`newrelic-setup` の MySQL 部分は PostgreSQL に読み替える。そのまま適用しない
- `nginx-site` のパス振り分け（サーバー2 固定・least_conn）と `infra-tuning` の DB 分離は使わない。リージョン固定で、DB は最初から分離済み
- サーバー間で直接つなぐ構成を入れる前に、ルーターを迂回しないことを確認する

## 配布物の AGENTS.md
- リポジトリと各言語のディレクトリにある `AGENTS.md` は本番運用向けの内容で、sudo・設定変更・再起動を禁じている
- 当日マニュアルはサーバ内の変更を認めていて、内容が食い違う。扱いはチームで決める（`docs/regulation.md` の質問 1）

## 止まるところ（人の OK を待つ）
- デプロイ、main へのマージ
- `DROP` / `rm -rf` / サーバーの再起動・停止 / サービスの停止
- ポータルのベンチ実行以外の操作
- main への直接 push と force push はしない
