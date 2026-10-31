-- 閲覧者 user_id と相手 other_id の共通の友達数 n（#176）。友達の友達フィードがリクエストごとに数え直していた集計を書き込み時に持つ。
-- n = 0 の行は消さずに残す（other_id 側の index が無いので DELETE は全表を読む）。読む側が n > 0 で絞る
CREATE TABLE IF NOT EXISTS mutual_friends (
  user_id varchar(36) NOT NULL,
  other_id varchar(36) NOT NULL,
  n integer NOT NULL,
  PRIMARY KEY (user_id, other_id)
);

-- friendships の辺 a-b が増える/減ると、a の友達 f(≠b) と b、b の友達 g(≠a) と a の共通の友達数が両方向 1 ずつ変わる。
-- 4 本は互いに同じ (u, o) を作らない（f ≠ a、g ≠ b、a ≠ b）ので 1 文の ON CONFLICT で足せる。
-- 同時に別の辺を足すトランザクションと行ロックの順が交差しないよう、キー順に書く
-- a-b と a-c の書き込みが同時に走ると、互いの未 commit の辺が friend_edges に見えず n がずれる（#190）。
-- 先頭で a・b それぞれのユーザー単位の advisory lock をキー順（a < b）に取り、同じ人を含む辺の書き込みを直列にする。
-- plpgsql は文ごとに新しいスナップショットを取るので、待った後の friend_edges 読みには相手の commit 済みの辺が見える。
-- キーの seed は 1（ペアロックの seed 0 と空間を分ける）
CREATE OR REPLACE FUNCTION mutual_friends_sync() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE a text; b text; d integer;
BEGIN
  IF TG_OP = 'INSERT' THEN a := NEW.user_low_id; b := NEW.user_high_id; d := 1;
  ELSE a := OLD.user_low_id; b := OLD.user_high_id; d := -1; END IF;
  PERFORM pg_advisory_xact_lock(hashtextextended(a, 1));
  PERFORM pg_advisory_xact_lock(hashtextextended(b, 1));
  INSERT INTO mutual_friends (user_id, other_id, n)
  SELECT u, o, d FROM (
    SELECT f.friend_id AS u, b AS o FROM friend_edges f WHERE f.user_id = a AND f.friend_id <> b
    UNION ALL SELECT b, f.friend_id FROM friend_edges f WHERE f.user_id = a AND f.friend_id <> b
    UNION ALL SELECT g.friend_id, a FROM friend_edges g WHERE g.user_id = b AND g.friend_id <> a
    UNION ALL SELECT a, g.friend_id FROM friend_edges g WHERE g.user_id = b AND g.friend_id <> a
  ) pairs
  ORDER BY u, o
  ON CONFLICT (user_id, other_id) DO UPDATE SET n = mutual_friends.n + EXCLUDED.n;
  RETURN NULL;
END $$;

-- 名前の順で friend_edges_sync の後に動く（friend_edges は更新済み）。a-b 自身は f <> b / g <> a で除くので順序に依存しない
CREATE OR REPLACE TRIGGER mutual_friends_sync AFTER INSERT OR DELETE ON friendships
FOR EACH ROW EXECUTE FUNCTION mutual_friends_sync();

-- 初期化で friend_edges を作り直した後に作り直す（約 105 万行）
TRUNCATE mutual_friends;
INSERT INTO mutual_friends (user_id, other_id, n)
SELECT a.user_id, b.friend_id, count(*)
FROM friend_edges a JOIN friend_edges b ON b.user_id = a.friend_id
WHERE b.friend_id <> a.user_id
GROUP BY 1, 2;
