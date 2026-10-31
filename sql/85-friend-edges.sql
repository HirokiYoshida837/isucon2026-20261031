-- friendships を両方向の行で持つ表（#123）。user_low_id / user_high_id の OR 条件をやめ、PK 1 本で引く。
-- 書き込みは friendships の行トリガーで同じトランザクションの中で追従する（書く場所が 5 か所あり、書き漏らしを避けるため）。
-- reset.sql の psql セッションは session_replication_role = replica でトリガーが動かないので、下で作り直す
CREATE TABLE IF NOT EXISTS friend_edges (
  user_id varchar(36) NOT NULL,
  friend_id varchar(36) NOT NULL,
  PRIMARY KEY (user_id, friend_id)
);
-- 友達一覧を friend_edges だけで引くため friendships.id / updated_at も持つ（#172）。friendships は INSERT と DELETE だけなので値はずれない。
-- 列があるときは ALTER しない（IF NOT EXISTS でも ACCESS EXCLUSIVE を取りに行く。#162）。値は下の作り直しで埋まる
DO $$
BEGIN
  IF NOT EXISTS (SELECT 1 FROM information_schema.columns
                 WHERE table_name = 'friend_edges' AND column_name = 'relation_id') THEN
    ALTER TABLE friend_edges ADD COLUMN relation_id varchar(36), ADD COLUMN updated_at timestamptz;
  END IF;
END $$;

CREATE OR REPLACE FUNCTION friend_edges_sync() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
  IF TG_OP = 'INSERT' THEN
    INSERT INTO friend_edges (user_id, friend_id, relation_id, updated_at)
    VALUES (NEW.user_low_id, NEW.user_high_id, NEW.id, NEW.updated_at), (NEW.user_high_id, NEW.user_low_id, NEW.id, NEW.updated_at)
    ON CONFLICT DO NOTHING;
  ELSE
    DELETE FROM friend_edges
    WHERE (user_id, friend_id) IN ((OLD.user_low_id, OLD.user_high_id), (OLD.user_high_id, OLD.user_low_id));
  END IF;
  RETURN NULL;
END $$;

CREATE OR REPLACE TRIGGER friend_edges_sync AFTER INSERT OR DELETE ON friendships
FOR EACH ROW EXECUTE FUNCTION friend_edges_sync();

-- 初期化で friendships を initial_friendships から戻した後に作り直す（約 5 万行）
TRUNCATE friend_edges;
INSERT INTO friend_edges (user_id, friend_id, relation_id, updated_at)
SELECT user_low_id, user_high_id, id, updated_at FROM friendships
UNION ALL
SELECT user_high_id, user_low_id, id, updated_at FROM friendships;
CREATE INDEX IF NOT EXISTS idx_friend_edges_user_updated ON friend_edges (user_id, updated_at DESC, relation_id DESC);
