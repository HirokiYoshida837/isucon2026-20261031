---
name: db-migrate-setup
description: 競技が始まったら、問題の初期化の仕組みと DB を調べて、その問題専用の SKILL db-migrate（index・スキーマ変更の手順と道具）を競技リポジトリに作る。「マイグレーションの SKILL 作って」「index 貼る準備」「db-migrate 作って」「初期化の仕組み調べて」などで使う。
---

# db-migrate 作成（ISUCON 用）

マイグレーションのやり方は問題ごとに変わる（スキーマの置き場所・初期化の方式・タイムアウト）。
だから当日、実物を読んでから、その問題専用の SKILL `db-migrate` を競技リポジトリに作る。
判断の知識は同じフォルダの `reference.md`（判断フロー・過去回の例・手法）にある。この SKILL はそれを当日の事実で埋める手順。

## ファイル
| パス | 中身 |
|---|---|
| `reference.md` | マイグレーション手法。判断フロー・2 パターンの手順・画像の外出し・DB 分離時の initialize・過去回の例 |
| `inspect.sh <ssh 名> [DB 名] [webapp のパス]` | DB サーバーの事実を集める（読み取りだけ）。バージョン・DB 一覧・テーブルサイズ・既存 index・`sql/` の中身・DROP / TRUNCATE / DELETE の有無 |
| `templates/SKILL.md.template` | 作る `db-migrate/SKILL.md` の雛形。`<<...>>` を埋め、`PATTERN:SQL` か `PATTERN:GO` の片方を残す |
| `templates/idx.sh` | 作る SKILL に入れる道具（show / explain / apply）。MySQL・MariaDB 共通 |
| `templates/80-add-index.sql` | SQL ファイル型のときに作る SKILL へ同梱する |
| `templates/init.sh` / `90-recompute.sql` / `initialize_handler.go` / `export-images.sh` | 初期化・再計算・Go の冪等 DDL・画像の外出しの例。`reference.md` の手法から参照する |
| 作例 | 練習リポジトリ（private-isu）の `.claude/skills/db-migrate/`。Go 型の作例 |

## 前提
- 競技リポジトリの CLAUDE.md の「環境」節と `docs/`（prompts/01 が作る）が埋まっていること
- 足りない値は人に聞く（推測で埋めない）

## 集める情報
| 項目 | どこで確認するか |
|---|---|
| 初期化の入口（URI・メソッド） | マニュアル（`docs/sources/`）、アプリの initialize ハンドラ |
| 初期化の方式 | initialize ハンドラが何を呼ぶか（`init.sh` / SQL ファイル / Go 内の SQL）。`inspect.sh` の DROP / TRUNCATE / DELETE |
| タイムアウト秒数 | マニュアル。見つからなければベンチマーカーのソース（`InitializeTimeout` などで grep）。それでも無ければ人に聞く |
| DB のエンジン・バージョン・場所・DB 名 | `inspect.sh`、アプリの DB 接続の環境変数（`env.sh` など） |
| テーブルサイズ・既存 index | `inspect.sh <ssh 名> <DB 名>` |
| ビルドと vet のコマンド・デプロイ SKILL 名 | 競技リポジトリの CLAUDE.md・Makefile |

## 作る手順
1. `.claude/skills/db-migrate-setup/inspect.sh <ssh 名>` で DB 名を確かめ、`inspect.sh <ssh 名> <DB 名>` で残りを集める
2. initialize ハンドラと、そこから流れる SQL・スクリプトを全部読む
3. 方式を決める（`reference.md`「当日どれを選ぶか」）
   | 初期化の中身 | 方式 | 書き方 |
   |---|---|---|
   | `DROP TABLE` → `CREATE TABLE` を毎回流す | 作り直し | SQL ファイル型（80-add-index.sql。スキーマに直接書いてもよい） |
   | SQL ファイルで TRUNCATE / DELETE だけ | 引き継ぎ | SQL ファイル型（80-add-index.sql） |
   | Go の中で DELETE などを流すだけ（SQL ファイルが無い） | 引き継ぎ | Go 型（`ensureIndex`） |
   - DB が PostgreSQL・SQLite なら、このテンプレートは使えない。止まって人に聞く
   - MariaDB なら `CREATE INDEX IF NOT EXISTS` が使える。SQL ファイル型ならそれで書いてよい（80-add-index.sql の冒頭の注記）
4. 競技リポジトリに `.claude/skills/db-migrate/` を作る
   - `templates/SKILL.md.template` → `SKILL.md`。`<<...>>` を全部埋め、使わない PATTERN ブロックとコメント行を消す
   - `templates/idx.sh` → `idx.sh`。既定の ssh 名と DB 名を埋めて `chmod +x`
   - SQL ファイル型なら `templates/80-add-index.sql` も同梱する
5. 確かめる
   - `grep -n '<<[^<>]*>>' .claude/skills/db-migrate/*` が 0 件（idx.sh の heredoc `<<SQL` は対象外にした式）
   - `idx.sh show` と `idx.sh explain "SELECT 1"` が動く
   - Go 型なら、`ensureIndex` を一時的に初期化関数へ足してビルドと `go vet` が通ることを見て、戻す（変数名・import の差分を SKILL に書く）
   - SQL ファイル型なら、手元か競技サーバーで初期化を 2 回続けて叩き、2 回とも成功する時間を測って SKILL に書く（競技サーバーで叩くのは人の OK を得てから）
6. ブランチ（例 `<担当>/skill-db-migrate`）で commit → push → PR。Claude Code を再起動すると SKILL が読み込まれる

## 注意
- この SKILL は実行時に DB を変えない（`inspect.sh` は読み取りだけ）。index を貼るのは作った `db-migrate` の仕事
- この SKILL は競技リポジトリにも入る（`harness-setup` が写す）。当日に作るのは `db-migrate` だけ
- 初期化の方式を読み違えると壊れる。根拠のファイルと行を SKILL の表に書く
- DB を別サーバーに分けたら、作った SKILL の表と `idx.sh` の既定値を直す（`reference.md` 6 章）
