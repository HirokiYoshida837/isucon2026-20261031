-- PUT /api/blocks 用。ペアロック・存在確認・conflict 判定・block の INSERT・friendships / follows の削除・friend_requests の cancel を 1 往復で行う。
-- 戻り値: 0=作成, 404=relationship_not_found, 409=relationship_conflict
-- 元の blockHandler（BEGIN → ペアロック + active 確認 → CTE → COMMIT の 4 往復）と同じ順序・同じ条件。
CREATE OR REPLACE FUNCTION block_user(p_actor text, p_target text, p_id text) RETURNS integer
LANGUAGE plpgsql AS $$
BEGIN
  PERFORM pg_advisory_xact_lock(hashtextextended(least(p_actor, p_target) || '|' || greatest(p_actor, p_target), 0));
  IF (SELECT count(*) FROM users WHERE id IN (p_actor, p_target) AND status = 'active') <> 2 THEN
    RETURN 404;
  END IF;
  INSERT INTO block_relations (id, blocker_id, blocked_id) VALUES (p_id, p_actor, p_target)
  ON CONFLICT (blocker_id, blocked_id) DO NOTHING;
  IF NOT FOUND THEN
    RETURN 409;
  END IF;
  DELETE FROM friendships WHERE user_low_id = least(p_actor, p_target) AND user_high_id = greatest(p_actor, p_target);
  DELETE FROM follows WHERE (follower_id = p_actor AND followee_id = p_target) OR (follower_id = p_target AND followee_id = p_actor);
  UPDATE friend_requests SET status = 'cancelled', updated_at = clock_timestamp()
   WHERE status = 'pending'
     AND ((requester_id = p_actor AND recipient_id = p_target) OR (requester_id = p_target AND recipient_id = p_actor));
  RETURN 0;
END $$;
