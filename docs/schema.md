# スキーマ

- 出典は `sql/001-schema.sql`、`sql/003-initial-snapshot.sql`、`sql/reset.sql`
- DB は PostgreSQL 16。asia2 の 1 台を 3 台のアプリが共有する（当日マニュアル「データベース(PostgreSQL)」）

## テーブルと index
| テーブル | 主キー | その他の index・制約 | 初期化での扱い |
|---|---|---|---|
| `users` | `id` | `uq_users_username`（UNIQUE、register の重複判定に使う） | 新規を削除、初期分を `initial_users` へ戻す |
| `image_objects` | `(id, created_at)` | `created_at` の月別パーティション、`owner_user_id` の FK | 新規を削除。境界より後のパーティションは TRUNCATE |
| `administrators` | `id` | 無し | 触らない |
| `login_sessions` | `id` | 無し | TRUNCATE |
| `administrator_sessions` | `id` | 無し | TRUNCATE |
| `friendships` | `id` | UNIQUE `(user_low_id, user_high_id)` | TRUNCATE 後に `initial_friendships` から入れ直す |
| `follows` | `id` | UNIQUE `(follower_id, followee_id)` | TRUNCATE 後に `initial_follows` から入れ直す |
| `block_relations` | `id` | UNIQUE `(blocker_id, blocked_id)` | TRUNCATE 後に `initial_block_relations` から入れ直す |
| `posting_windows` | `id` | UNIQUE `(region, local_date)`、UNIQUE `(region) WHERE ended_at IS NULL` | 新規を削除 |
| `posts` | `id` | `posts_author_id`、`posts_posting_window_id` | 新規を削除 |
| `friend_requests` | `id` | 無し | TRUNCATE 後に `initial_friend_requests` から入れ直す |
| `realmojis` | `id` | UNIQUE `(post_id, reactor_id) WHERE replaced_at IS NULL AND deleted_at IS NULL`、`realmojis_post_id` | 新規を削除 |
| `comments` | `id` | `comments_post_id` | 新規を削除 |
| `notifications` | `id` | UNIQUE `(user_id, posting_window_id)` | 新規を削除 |
| `ip_bans` | `ip` | 無し | TRUNCATE |

- `001-schema.sql` の DROP 一覧にある `app_settings`、`worker_jobs`、`idempotency_records` は CREATE されない
- 「新規」は `created_at` が `initial_data_boundary` より後の行

## 初期化の仕組み
- `POST /api/initialize` → `sql/init.sh` → `sql/reset.sql`（`go/initialize.go`、`sql/init.sh`）
- `reset.sql` はテーブルと index を作り直さない。貼った index は初期化の後も残る
- `posts`・`comments`・`realmojis` の初期分への更新は `reset.sql` では戻らない
- `003-initial-snapshot.sql` は `initial_*` テーブルを作る。初期データの基準になるので流し直さない
- 新しいテーブルや列を足したら `reset.sql` にも戻し方を足す

## 触るときの注意
- `001-schema.sql` を流さない
  - 先頭で全テーブルを DROP する
  - 初期データの SQL（002 番）は `sql/` にも asia1 の `~/webapp/sql` にも無い。消すと戻せない
- 手元の変更は `sql/` に残し、DB への適用手順は SKILL `db-migrate-setup` で作る `db-migrate` にまとめる
- `001-schema.sql` の `image_objects` の前に「ファイル配置は障害になった。決して変更しない」というコメントがある
  - 当日マニュアルは保存方式の変更を認めている（「競技環境の変更」）
  - 画像を外に出すなら、全リージョンから取得できること・初期データの画像も出せることを確かめる
