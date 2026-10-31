---
name: db-migrate
description: ISUReal の DB（PostgreSQL 16）に index やスキーマ変更を入れる（マイグレーション）。初期化（POST /api/initialize → sql/init.sh）で冪等に再現できる形で入れ、重いテーブルは事前に流し、EXPLAIN で効いたか確かめる。「index 貼って」「インデックス追加」「マイグレーション」「スキーマ変更」「カラム追加」「EXPLAIN 見て」などで使う。
---

# DB マイグレーション（ISUReal 専用）

- この SKILL は prep の SKILL `db-migrate-setup` が 2026-10-31 に作った。初期化の仕組みが変わったら作り直す
- テンプレートは MySQL 向けなので、PostgreSQL に読み替えて作った（CLAUDE.md「SKILL の読み替え」）
- 手法の考え方と過去回の例: `.claude/skills/db-migrate-setup/reference.md`（MySQL の構文はそのまま使わない）

## 方針
- 変更は初期化の処理に入れる（ベンチは毎回初期化を叩き、追試は再起動後にやり直すため）
- 冪等に書く（初期化は何度も走るため。2 回目以降は即スキップさせて時間を守る）
- 重いテーブルは、デプロイ前に同じ定義で先に 1 回流す（初期化の制限時間に収めるため）
- 効いたかは EXPLAIN と計測値で確かめる

## この問題の実態（2026-10-31 調査）
| 項目 | 内容 |
|---|---|
| DB | PostgreSQL 16.15。`CREATE INDEX IF NOT EXISTS` が使える |
| DB の場所 | ssh `asia2`、DB 名 `isureal`。3 台のアプリが共有する |
| 初期化の入口 | `POST /api/initialize` → `sql/init.sh` → `sql/reset.sql`（`go/initialize.go:20`）。手で流すなら `~/webapp/sql/init.sh` |
| 初期化の実行場所 | リクエストを受けたアプリのサーバー。そこの `~/webapp/sql` を使い、`psql` で asia2 へつなぐ |
| 初期化の方式 | 引き継ぎ。TRUNCATE と DELETE だけで、テーブルと index を作り直さない。根拠: `sql/reset.sql:5-11`、`:24` |
| 初期化の制限時間 | 不明。「最終チェックで確定する」とだけある（`docs/sources/manual.md:448`）。`docs/regulation.md` の質問 3 |
| 初期化の所要時間 | 未計測。計測は競技サーバーで叩くので人の OK を待つ |
| 書き方 | SQL ファイル型（下の「書き方」） |

- `sql/001-schema.sql` を流さない（全テーブルを DROP する。初期データの SQL は配布に無く、消すと戻せない）
- `sql/003-initial-snapshot.sql` も流し直さない（初期データの基準が変わる）

### テーブル（大きい順）
| テーブル | 推定行数 | テーブル | index | 備考 |
|---|---|---|---|---|
| `image_objects` | 183 万 | 14 GB | 153 MB | 月別パーティション（子 36 個） |
| `notifications` | 182 万 | 272 MB | 387 MB | |
| `realmojis` | 120 万 | 229 MB | 277 MB | |
| `posts` | 63 万 | 152 MB | 58 MB | |
| `comments` | 55 万 | 90 MB | 70 MB | |
| `friendships` | 2.5 万 | 5 MB | 5 MB | |
| `friend_requests` | 2.9 万 | 5 MB | 2 MB | |
| `users` | 5,000 | 2 MB | 472 kB | |
| `follows` | 1 万 | 2 MB | 2 MB | |
| 他（`posting_windows`・`login_sessions`・`block_relations`・`ip_bans`・`administrator*`） | 各 1,100 以下 | 各 168 kB 以下 | | |

- 重いテーブル（目安 100MB 超、または 100 万行超）: `image_objects`、`notifications`、`realmojis`、`posts`
- `initial_*` は初期化の戻し元。index は要らない

### 既存 index
主キーは全テーブルにある。主キー以外はこれだけ。

| テーブル | index | 定義 |
|---|---|---|
| `block_relations` | `block_relations_blocker_id_blocked_id_key` | UNIQUE `(blocker_id, blocked_id)` |
| `comments` | `comments_post_id` | `(post_id)` |
| `follows` | `follows_follower_id_followee_id_key` | UNIQUE `(follower_id, followee_id)` |
| `friendships` | `friendships_user_low_id_user_high_id_key` | UNIQUE `(user_low_id, user_high_id)` |
| `notifications` | `notifications_user_id_posting_window_id_key` | UNIQUE `(user_id, posting_window_id)` |
| `posting_windows` | `posting_windows_one_active_region` | UNIQUE `(region) WHERE ended_at IS NULL` |
| `posting_windows` | `posting_windows_region_local_date_key` | UNIQUE `(region, local_date)` |
| `posts` | `posts_author_id` | `(author_id)` |
| `posts` | `posts_posting_window_id` | `(posting_window_id)` |
| `realmojis` | `realmojis_one_active` | UNIQUE `(post_id, reactor_id) WHERE replaced_at IS NULL AND deleted_at IS NULL` |
| `realmojis` | `realmojis_post_id` | `(post_id)` |

## スクリプト（`.claude/skills/db-migrate/idx.sh`）
| コマンド | やること |
|---|---|
| `idx.sh show [table]` | テーブルの行数・サイズと index 一覧（`valid` が `f` なら壊れている） |
| `idx.sh explain "<SELECT ...>"` | EXPLAIN（実行はしない）。`$1` は実際の値に置き換えて渡す |
| `idx.sh apply <table> <name> "<cols>" ["<where>"]` | 無ければ CREATE INDEX、所要秒を表示。あればスキップ（**人の OK を得てから使う**） |

- `apply` は書き込みを止めない `CONCURRENTLY` で貼る
- パーティションの親（`image_objects`）だけ `CONCURRENTLY` を使えない。貼り終わるまで書き込みが止まる
- `CONCURRENTLY` は実行中のトランザクションの終了を待つ。失敗すると INVALID な index が残る
- INVALID が残ると `IF NOT EXISTS` が素通りするので、`apply` は止まって知らせる
- 既定の接続先は上の表の DB。変えるなら `ISU_DB_SSH` を使い、この SKILL の表も直す

## 流れ（1 index = 1 ブランチ = 1 PR）
1. 根拠を出す: SKILL newrelic-nrql で重い SQL を特定し、`idx.sh show` で今の index を見る
   - PostgreSQL のスロークエリログは無効（`log_min_duration_statement = -1`）
2. `idx.sh explain "<その SQL>"` で `Seq Scan` や `Sort` を確かめる
3. 貼る index を決める
   - WHERE の等価条件のカラム → ORDER BY のカラム、の順の複合 index にする
   - ORDER BY の向きは index にそろえる（`created_at DESC` など）
   - `deleted_at IS NULL` で毎回絞るなら部分 index も考える（既存の `realmojis_one_active` が例）
   - index 名は `idx_<table>_<カラム>` にそろえる（index 名は DB 全体で一意のため）
4. コードの 5 点チェックを 1 行ずつ答える（CLAUDE.md）
5. 下の「書き方」で初期化に足す
6. `cd go && make isureal` と `go vet ./...` を通す（Go を変えたとき。go は `/home/isucon/local/go/bin`）
7. 重いテーブルは、デプロイ前に `idx.sh apply` で先に貼る（人の OK を待つ）
   - `80-add-index.sql` と同じ名前・同じ定義にする（有無を名前だけで見るため）
   - ベンチの開始と重ねない（初期化の `TRUNCATE` などが待たされ、制限時間を超えると FAIL になるため）
8. SKILL bench でデプロイ → `ssh asia1 'time ~/webapp/sql/init.sh'` を 2 回続けて流し、2 回とも成功することを確かめる（人の OK を待つ。もう片方の人のベンチと重ねない）
   - 制限時間が確定するまでは、変更前より大きく伸びていないかで判断する
9. `idx.sh show <table>` で付いて `valid` が `t` なこと、`idx.sh explain` のプランに index 名が出ることを確かめる
10. ベンチ → スコアと NewRelic の SQL 時間を見て採用 or revert

## 書き方
- 初回だけ: `.claude/skills/db-migrate/80-add-index.sql` を `sql/` にコピーする
- 初回だけ: `sql/init.sh` の `--file=reset.sql` の後ろに `--file=80-add-index.sql` を足す（行末の `\` も足す）
- index ごとに、`sql/80-add-index.sql` の末尾へ 1 行足す
  ```sql
  CREATE INDEX IF NOT EXISTS idx_<table>_<cols> ON <table> (<col1>, <col2>);
  ```
- `CONCURRENTLY` は書かない（初期化の中では待つ必要がなく、失敗時に INVALID が残るため）
- `sql/001-schema.sql` は書き換えない（初期化で流れないので、本番に入らないため）

## 戻すとき
- 引き継ぎ型なので、revert しても index は DB に残る
- 消すなら `DROP INDEX CONCURRENTLY <name>`（`image_objects` は `DROP INDEX <name>`）。**DROP は人の OK を待つ**
- 残っていても害が少ない（書き込みが少し重くなる程度）なら、残したまま次へ進んでよい

## カラム追加・型変更など index 以外
- 考え方は同じ。`ALTER TABLE <table> ADD COLUMN IF NOT EXISTS ...` で冪等に書く
- 手法は `db-migrate-setup/reference.md` の 3〜4 章（`ALGORITHM` や `ON DUPLICATE KEY` は MySQL の構文なので読み替える）
- 埋め戻し（UPDATE）も初期化に入れ、`reset.sql` の後ろで流して時間を測る
- トリガーで埋めない（`reset.sql` の `session_replication_role = replica` が同じ `psql` のセッションに残り、トリガーと外部キーが動かないため）
- ベンチが検証する値・レスポンスの形を変えないこと
- テーブル・列を足したら `sql/reset.sql` に戻し方を足す（CLAUDE.md）

## やらないこと
- サーバーで手で貼っただけで終わらせる（追試で再現しないため）
- `ALTER SYSTEM` や `postgresql.conf` など設定の変更（SKILL infra-tuning の担当）
- 初期化に `DROP TABLE` を足す（データが戻らなくなる）
