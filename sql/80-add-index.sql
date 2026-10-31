-- index の追加（PostgreSQL 16）。init.sh が reset.sql の後に毎回流す
-- 何回流しても同じ結果になるよう、必ず IF NOT EXISTS を付ける
-- 名前だけで有無を見るので、定義を変えるときは名前も変える
-- CONCURRENTLY は書かない（重いテーブルはデプロイ前に idx.sh apply で先に貼る）
--
-- 例:
--   CREATE INDEX IF NOT EXISTS idx_posts_author_id_created_at ON posts (author_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_friendships_user_high_id ON friendships (user_high_id);
CREATE INDEX IF NOT EXISTS idx_block_relations_blocked_id ON block_relations (blocked_id);
CREATE INDEX IF NOT EXISTS idx_posts_posting_window_id_posted_at_id ON posts (posting_window_id, posted_at DESC, id DESC) WHERE deleted_at IS NULL;
-- register の LOCK TABLE users を外すための一意制約（go/user_api.go の ON CONFLICT (username) が使う）
CREATE UNIQUE INDEX IF NOT EXISTS uq_users_username ON users (username);
-- followers / following 一覧（go/relationship_list_api.go）の ORDER BY updated_at DESC, id DESC LIMIT 51 を index 順で返す（#73）
CREATE INDEX IF NOT EXISTS idx_follows_followee_id_updated_at_id ON follows (followee_id, updated_at DESC, id DESC);
CREATE INDEX IF NOT EXISTS idx_follows_follower_id_updated_at_id ON follows (follower_id, updated_at DESC, id DESC);
