-- PUT /api/follows 用。ペアロック・存在確認・conflict 判定・INSERT を 1 往復で行う。
-- 戻り値: 0=作成, 404=relationship_not_found, 409=relationship_conflict
-- plpgsql は文ごとに新しいスナップショットを取るので、ロック待ちの間に commit された block も見える。
CREATE OR REPLACE FUNCTION follow_user(p_actor text, p_target text, p_id text) RETURNS integer
LANGUAGE plpgsql AS $$
DECLARE
  v_type text;
BEGIN
  PERFORM pg_advisory_xact_lock(hashtextextended(least(p_actor, p_target) || '|' || greatest(p_actor, p_target), 0));
  IF (SELECT count(*) FROM users WHERE id IN (p_actor, p_target) AND status = 'active') <> 2 THEN
    RETURN 404;
  END IF;
  SELECT account_type INTO v_type FROM users WHERE id = p_target;
  IF v_type <> 'certified_public'
     OR EXISTS(SELECT 1 FROM block_relations WHERE (blocker_id = p_actor AND blocked_id = p_target) OR (blocker_id = p_target AND blocked_id = p_actor))
     OR EXISTS(SELECT 1 FROM follows WHERE follower_id = p_actor AND followee_id = p_target) THEN
    RETURN 409;
  END IF;
  INSERT INTO follows (id, follower_id, followee_id) VALUES (p_id, p_actor, p_target);
  RETURN 0;
END $$;
