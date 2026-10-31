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
CREATE INDEX IF NOT EXISTS idx_realmojis_image_id_reactor_id ON realmojis (image_id, reactor_id) WHERE replaced_at IS NULL AND deleted_at IS NULL;
CREATE INDEX IF NOT EXISTS idx_posts_image_id_author_id ON posts (image_id, author_id) WHERE deleted_at IS NULL;
CREATE INDEX IF NOT EXISTS idx_posts_author_id_posting_window_id ON posts (author_id, posting_window_id);
CREATE INDEX IF NOT EXISTS idx_users_username_id ON users (username, id);
CREATE INDEX IF NOT EXISTS idx_login_sessions_token_hash ON login_sessions (token_hash) WHERE active = true;
CREATE INDEX IF NOT EXISTS idx_login_sessions_user_id ON login_sessions (user_id) WHERE active = true;
CREATE INDEX IF NOT EXISTS idx_friend_requests_requester_id_recipient_id ON friend_requests (requester_id, recipient_id) WHERE status = 'pending';
CREATE INDEX IF NOT EXISTS idx_friend_requests_recipient_id_requester_id ON friend_requests (recipient_id, requester_id) WHERE status = 'pending';
CREATE INDEX IF NOT EXISTS idx_follows_followee_id_follower_id ON follows (followee_id, follower_id);
CREATE INDEX IF NOT EXISTS idx_posting_windows_local_date ON posting_windows (local_date);
