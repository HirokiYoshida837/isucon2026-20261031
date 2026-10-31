-- 他の index に包含されていて要らない index を消す（#228）。init.sh が毎回流すので IF EXISTS を付ける。
-- posts_author_id（001-schema.sql）: 先頭が author_id の非部分 index（idx_posts_author_id_posting_window_id・idx_posts_author_id_local_date）が
--   検索と FK posts_author_id_fkey のチェックを引き受ける。posts の INSERT ごとの index 更新を 1 本減らす。
-- idx_follows_followee_id_follower_id（80-add-index.sql から外した）: followee_id 単独は idx_follows_followee_id_updated_at_id、
--   (follower_id, followee_id) の等値は UNIQUE follows_follower_id_followee_id_key で引ける。
DROP INDEX IF EXISTS posts_author_id;
DROP INDEX IF EXISTS idx_follows_followee_id_follower_id;
