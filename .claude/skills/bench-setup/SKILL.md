---
name: bench-setup
description: 競技が始まったら、デプロイの仕組みとポータルを確かめて、その問題専用の SKILL bench（デプロイ・ベンチ・Discord 投稿の手順と道具）を競技リポジトリに作る。「bench 作って」「デプロイとベンチの準備」「bench-setup」「ベンチの SKILL 作って」などで使う。
---

# bench 作成（ISUCON 用）

デプロイとベンチの手順は問題ごとに変わる（サーバーの役割・デプロイコマンド・ポータルの画面）。
だから当日、実物で 1 回通してから、その問題専用の SKILL `bench` を競技リポジトリに作る。
スクリプトは練習で直した完成品をそのまま写す。当日に作るのは SKILL.md（確認済みの事実）だけ。

## ファイル
| パス | 中身 |
|---|---|
| `check.sh [ホスト...]` | 事前チェック（読み取りだけ）。手元の設定（isucon.env・discord.env・jq）と、各ホストの git・gh 認証・Makefile の deploy・go・秘伝のタレ・pidstat を OK / WARN / NG で出す |
| `templates/SKILL.md.template` | 作る `bench/SKILL.md` の雛形。`<<...>>` を当日の事実で埋める |
| `templates/*.sh` | 作る SKILL に入れるスクリプト一式（deploy / bench-start / bench-end / bench-report / discord-post / lib）。中身は変えずに写す |

## 前提
- SKILL `makefile-setup` で競技リポジトリに Makefile（`REF` を受け取る `deploy`）ができていること
- 全台で SKILL `gh-setup` が済み、本人が `gh auth login` 済みであること
- 秘伝のタレを全台に配ってあること（SKILL `infra-tuning`。bench-before.sh / bench-after.sh を使う）
- `~/.config/isucon/isucon.env` と `discord.env` があること。足りない値は人に聞く（推測で埋めない）

## 集める情報
| 項目 | どこで確認するか |
|---|---|
| ホストと役割 | 競技リポジトリの CLAUDE.md「環境」、`~/.ssh/config` |
| `ISU_HOSTS` / `ISU_REMOTE_DIR` / `ISU_DEPLOY_CMD` | `isucon.env`。`check.sh` の INFO 行 |
| go の場所（非対話 ssh の PATH） | `check.sh` の go の行。WARN なら各サーバーで `which go` を見て `ISU_DEPLOY_CMD` に PATH を足す |
| デプロイの所要時間 | 手順 3 の `deploy.sh main` の実測 |
| ポータルの URL・ベンチ実行の手順・状態の文言・結果の場所 | 手順 4 で、人と一緒に画面を見て確かめる |
| ベンチの所要時間 | 手順 4 の実測（bench-end.sh の「所要」） |
| この問題固有の注意 | 競技リポジトリの CLAUDE.md・`docs/`（初期化の時間など） |

## 作る手順
1. `.claude/skills/bench-setup/check.sh` を実行する。NG を全部直す
   - `isucon.env` を編集する前は `.bak` を取る
   - `ISU_DEPLOY_CMD` を複数コマンドにするときは `bash -ec "..."` で包む（`REF=x cmd1 && cmd2` だと REF が cmd1 にしか渡らず、途中で落ちても exit 0 の偽成功になるため）
   - go を PATH に足す例: `ISU_DEPLOY_CMD='bash -ec "export PATH=\$PATH:/home/isucon/local/golang/bin; make deploy"'`（場所は当日の `which go` で確かめる）
2. 競技リポジトリに `.claude/skills/bench/` を作る
   - `templates/*.sh` をそのままコピーして `chmod +x`
   - `templates/SKILL.md.template` → `SKILL.md`。この時点で分かる `<<...>>` を埋める
3. デプロイを通す（人の OK を得てから）
   - 競技リポジトリのルートで `.claude/skills/bench/deploy.sh main`。全ホスト ✅ と Discord への投稿を確認し、所要時間を測る
   - `DRY_RUN=1` は Discord に送らないだけで、デプロイ自体は実行される
   - PR がひとつあれば `deploy.sh <PR番号>` も試す（`gh pr checkout` の挙動の確認）。無ければ SKILL.md の注意に「PR 指定は未確認」と書く
4. ベンチを 1 回通す（人と一緒に）
   - `bench-start.sh "bench-setup の確認"` → ポータルでベンチ → `bench-end.sh` → `bench-report.sh`
   - ポータルの画面で、実行の手順・実行中 / 待機中 / 完了の文言・スコアとエラーの場所を確かめ、SKILL.md に書く
   - Discord に結果とリソース表、添付（alp・pt-query-digest・NewRelic）が届くことを確認
5. 確かめる
   - `grep -n '<<[^<>]*>>' .claude/skills/bench/SKILL.md` が 0 件
   - SKILL.md の「確認日時」に日時と確認した人を書く
6. ブランチ（例 `<担当>/skill-bench`）で commit → push → PR。Claude Code を再起動すると SKILL が読み込まれる

## 注意
- この SKILL は競技リポジトリにも入る（`harness-setup` が写す）。当日に作るのは `bench` だけ
- スクリプトを直すときは prep の `templates/` を直し、競技リポジトリの `bench/` にもコピーする（片方だけ直さない）
- 構成を変えたら（DB 分離・app を 1 台増やす等）、`isucon.env` と作った `bench/SKILL.md` の「この問題の構成」を一緒に直し、`check.sh` をもう一度実行する
- ポータル操作は、ベンチの実行と結果の閲覧だけ。想定外のダイアログが出たら押さずに人に聞く
- Webhook URL・キー・ポータルのトークンを SKILL.md やログに書かない
