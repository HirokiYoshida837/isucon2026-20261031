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
-- 画像の生存確認（go/image_api.go）・認証（go/session.go）・友達申請・ユーザー検索・自分の投稿の index（#72 #74）
-- DB にはすでに貼ってある（PR #11 が yochi/feed-indexes 側に入って main に届いていなかった分と、手で貼った posts(author_id, posted_at)）
CREATE INDEX IF NOT EXISTS idx_realmojis_image_id_reactor_id ON realmojis (image_id, reactor_id) WHERE replaced_at IS NULL AND deleted_at IS NULL;
CREATE INDEX IF NOT EXISTS idx_posts_image_id_author_id ON posts (image_id, author_id) WHERE deleted_at IS NULL;
CREATE INDEX IF NOT EXISTS idx_posts_author_id_posting_window_id ON posts (author_id, posting_window_id);
CREATE INDEX IF NOT EXISTS idx_posts_author_id_posted_at_id ON posts (author_id, posted_at DESC, id DESC) WHERE deleted_at IS NULL;
CREATE INDEX IF NOT EXISTS idx_users_username_id ON users (username, id);
CREATE INDEX IF NOT EXISTS idx_login_sessions_token_hash ON login_sessions (token_hash) WHERE active = true;
CREATE INDEX IF NOT EXISTS idx_login_sessions_user_id ON login_sessions (user_id) WHERE active = true;
CREATE INDEX IF NOT EXISTS idx_friend_requests_requester_id_recipient_id ON friend_requests (requester_id, recipient_id) WHERE status = 'pending';
CREATE INDEX IF NOT EXISTS idx_friend_requests_recipient_id_requester_id ON friend_requests (recipient_id, requester_id) WHERE status = 'pending';
CREATE INDEX IF NOT EXISTS idx_follows_followee_id_follower_id ON follows (followee_id, follower_id);
CREATE INDEX IF NOT EXISTS idx_posting_windows_local_date ON posting_windows (local_date);
-- memories / 投稿詳細の realmojis・comments 取得を Index Only Scan にして heap のランダム読み（1 投稿 1 ページ、ディスク 0.5〜0.7ms）を消す（#181）
-- SELECT 句の列を全部 INCLUDE に持つ（replaced_at / deleted_at は WHERE で NULL 固定だが SELECT 句にあるので入れる）
-- DB には先に CREATE INDEX CONCURRENTLY で貼る（realmojis 120 万行・comments 55 万行）
CREATE INDEX IF NOT EXISTS idx_realmojis_post_id_active_cover ON realmojis (post_id, created_at DESC, id DESC) INCLUDE (reactor_id, image_id, reaction_type, replaced_at, deleted_at) WHERE replaced_at IS NULL AND deleted_at IS NULL;
CREATE INDEX IF NOT EXISTS idx_comments_post_id_active_cover ON comments (post_id, created_at, id) INCLUDE (author_id, body, deleted_at) WHERE deleted_at IS NULL;
