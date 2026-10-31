-- friendships を両方向の行で持つ表（#123）。user_low_id / user_high_id の OR 条件をやめ、PK 1 本で引く。
-- 書き込みは friendships の行トリガーで同じトランザクションの中で追従する（書く場所が 5 か所あり、書き漏らしを避けるため）。
-- reset.sql の psql セッションは session_replication_role = replica でトリガーが動かないので、下で作り直す
CREATE TABLE IF NOT EXISTS friend_edges (
  user_id varchar(36) NOT NULL,
  friend_id varchar(36) NOT NULL,
  PRIMARY KEY (user_id, friend_id)
);

CREATE OR REPLACE FUNCTION friend_edges_sync() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
  IF TG_OP = 'INSERT' THEN
    INSERT INTO friend_edges (user_id, friend_id)
    VALUES (NEW.user_low_id, NEW.user_high_id), (NEW.user_high_id, NEW.user_low_id)
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
INSERT INTO friend_edges (user_id, friend_id)
SELECT user_low_id, user_high_id FROM friendships
UNION ALL
SELECT user_high_id, user_low_id FROM friendships;
