---
name: bench
description: ISUReal（ISUCON2026）の競技サーバーへのデプロイ、ポータルでのベンチ実行（ブラウザ操作）、ベンチ中のリソース記録、結果の Discord 投稿までを行う。「デプロイして」「main を反映して」「ベンチ回して」「ベンチ結果貼って」「Discord に流して」などで使う。
---

# デプロイ・ベンチ（ISUReal）

SKILL `bench-setup` が作った。確認日時: 2026-10-31 10:55 JST、すー
下の値は確認済みの事実。構成を変えたら（DB 分離など）この表と `isucon.env` を一緒に直す。

## この問題の構成
| 項目 | 値 |
|---|---|
| ホストと役割 | asia1 nginx+app（asia）/ europe1 nginx+app（europe）/ america1 nginx+app（america）/ asia2 PostgreSQL（asia） |
| デプロイするホスト（`ISU_HOSTS`） | asia1 europe1 america1 asia2（asia2 は Makefile がアプリの再起動を飛ばす。ベンチ前後の記録のために含める） |
| リポジトリの場所（`ISU_REMOTE_DIR`） | `/home/isucon/webapp` |
| デプロイコマンド（`ISU_DEPLOY_CMD`） | `bash -ec "export PATH=\$PATH:/home/isucon/local/go/bin; make deploy"` |
| デプロイの所要時間（1 台） | 3〜6 秒（コード差分なしの実測。全 4 台で 21 秒。差分があるとビルドの分だけ延びる） |
| ポータル | https://portal.isucon.net/contestant |
| ベンチの所要時間 | 135〜146 秒（#1856・#1582 の実測） |

## 既定の動き（`/bench`）
- 引数なしの `/bench` は、最新の main をデプロイする（`deploy.sh main`）
- `/bench` を呼んだ時点で、main のデプロイは人の OK 済みとして進める
- 別ブランチや PR を試すときだけ、`/bench <branch>` / `/bench <PR番号>` のように指定してもらう
- デプロイ後は ✅ を確認し、聞かずにそのまま下の「流れ」の 2 以降（ベンチ実行 → 結果投稿 → 次の改善候補）まで進める（2026-10-31 すーが決定。`/bench` を呼んだ時点でベンチまで OK 済み）
  - 全ホスト ✅ でなければ（❌ や exit 非 0）ベンチに進まず、原因を報告して止まる
- 事前に聞かないこと: ブランチの選択、ベンチまで回すか、Discord に投稿するか（投稿する前提）

## スクリプト（`.claude/skills/bench/`）
| スクリプト | やること |
|---|---|
| `deploy.sh [branch\|pr番号] [hosts...]` | 各サーバーで `REF=<branch\|pr番号> <ISU_DEPLOY_CMD>` を実行。反映コミットと成否を Discord に投稿。失敗ホストはログ添付。既定は `main`。数字を渡すと PR 番号として扱う |
| `bench-start.sh [メモ]` | 各サーバーで `ISU_BEFORE_CMD`（bench-before.sh）を実行し、開始時刻を記録して投稿 |
| `bench-end.sh [スコア] [PASS/FAIL]` | ベンチ完了を確認した直後に実行。完了時刻を記録して投稿 |
| `bench-report.sh <スコア> [メモ]` | 各サーバーで `ISU_AFTER_CMD`（bench-after.sh）を実行し、NewRelic（開始〜完了の期間ぴったり）と合わせて投稿。スコア履歴に追記 |
| `discord-post.sh [-f file] "本文"` | 任意の投稿。2000 文字超は自動で添付に回す |

- 設定: `~/.config/isucon/isucon.env`（ホスト・デプロイ・ベンチ前後コマンド。gh-setup / newrelic-setup と共通）と `discord.env`（Webhook URL）。中身を表示・出力しないこと
- 履歴: リポジトリ直下の `bench-logs/scores.tsv`（.gitignore 済み）
- `DRY_RUN=1` で Discord に送らず内容だけ表示する
- ベンチ中のリソース記録は bench-before.sh が裏で始め、bench-after.sh が止めて集計する
  - 2 秒ごと: CPU・メモリ・スワップ・load・ディスク・NW・TCP。5 秒ごと: プロセス別 CPU・メモリ（pidstat）
  - `db_conn` は `-` で出る（resource-sampler.sh が MySQL 専用で、PostgreSQL の接続数は取らないため）
- `deploy.sh` は偽の成功を返さない作り
  - ssh できないホストが 1 台でもあれば、Discord に流さず手元で止まる（exit 2。`~/.ssh/config` と `ISU_HOSTS` を直す）
  - ブランチ指定のときは、サーバーの HEAD がリモートのブランチ先頭と一致するかも確かめる。不一致は ❌（exit 3）

## 流れ
1. `deploy.sh main`（別ブランチは `deploy.sh <branch>`、PR は `deploy.sh <PR番号>`）。全ホスト ✅ を確認する。❌ があればベンチに進まず原因を見る
2. `bench-start.sh "<変更内容>"`
3. ポータルでベンチを実行する（Claude in Chrome）
   - ポータルにログイン済みのブラウザを使う（すー: 「すー」プロファイル）。`list_connected_browsers` で確認し、違えば `select_browser`
   - 実行の手順: https://portal.isucon.net/contestant/benchmark_jobs の `Enqueue` を押す。確認ダイアログは出ず、ジョブの詳細ページ（`/contestant/benchmark_jobs/<ID>`）へ移る
   - 他のベンチが実行中・待機中でないことを確かめてから押す。見分け方: Job List の Status。`RUNNING` が実行中、`FINISHED` が完了（ポータルの絞り込みには他に `PENDING`・`ERRORED`・`CANCELLED` がある。この 3 つは未確認。マニュアルは待機中を `WAITING` と書いている）
   - 30 秒ごとに `get_page_text` で状態を確認し、終わるまで待つ（ベンチ中はデプロイ・再起動をしない）
   - 完了を確認したら、すぐ `bench-end.sh <スコア> <PASS|FAIL>` で完了時刻を記録する
   - 結果の読み取り: ジョブの詳細ページの `Result`（`FinishedPassed`、`Score`、`Score Breakdown`）と `Stdout` 末尾の JSON（`pass`・`score`・`messages`）。FAIL の表示は未確認で、`pass` と `messages` を見る
4. `bench-report.sh <スコア> "<変更内容>"`。FAIL のときはポータルのエラー文を `discord-post.sh` で続けて投稿する
5. 添付の集計結果と NewRelic（SKILL `newrelic-nrql`）から、次の改善候補を 3 つまで挙げる
6. スコアが下がったら、その変更の revert を提案する

## この問題の注意
- デプロイの前に、ポータルでベンチが `WAITING`・`RUNNING` でないことと、他の人の `make deploy` が走っていないことを確かめる（デプロイはアプリを再起動する。チームで同じ 4 台を触っている）
- asia2 の `isureal-go.service` は disabled のまま保つ。`make deploy` は asia2 で取得とビルドだけ行い、再起動を飛ばす
- `pt-query-digest` は添付されない（bench-after.sh が MySQL のスロークエリログ前提のため）。スロークエリは PostgreSQL のログか NewRelic で見る
- alp の集計は今は出ない（2026-10-31 の確認時点で nginx のアクセスログが空。endpoints は NewRelic で見る）
- PR 指定（`deploy.sh <PR番号>`）は未確認
- 初期化の制限時間と負荷テストの長さはマニュアルに記載が無い（`docs/scoring.md`）。走行全体の実測は上の表、スコアは `docs/bench-log.md` に残す

## ポータル操作の範囲
- やってよい: ベンチの実行、結果・ログの閲覧（2026-10-26 すーが決定）
- やらない: チーム情報の変更、サーバー選択以外の設定変更、ベンチの取り消し、その他の送信操作。必要なら人に頼む
- 画面に想定外の確認ダイアログや表示が出たら、押さずに人に聞く
- Webhook URL やキー、ポータルのトークンを投稿本文やログに出さない
