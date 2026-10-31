-- POST /api/friend-requests/:id/:action 用。取得・権限判定・ペアロック・状態判定・UPDATE/accept を 1 往復で行う。
-- p_status: accepted / rejected / cancelled（action の検証は呼び出し側）
-- 戻り値の code: 0=成功, 403=relationship_action_forbidden, 404=relationship_not_found, 409=relationship_conflict
-- plpgsql は文ごとに新しいスナップショットを取るので、ロック待ちの間に commit された変更も見える。
CREATE OR REPLACE FUNCTION transition_friend_request(p_actor text, p_request_id text, p_status text, p_friendship_id text)
RETURNS TABLE(code integer, rid text, rstatus text, rupdated timestamptz)
LANGUAGE plpgsql AS $$
DECLARE
  v_requester text;
  v_recipient text;
  v_status text;
BEGIN
  SELECT fr.requester_id, fr.recipient_id INTO v_requester, v_recipient FROM friend_requests fr WHERE fr.id = p_request_id;
  IF v_requester IS NULL THEN
    RETURN QUERY SELECT 404, NULL::text, NULL::text, NULL::timestamptz;
    RETURN;
  END IF;
  IF (p_status = 'cancelled' AND p_actor <> v_requester) OR (p_status <> 'cancelled' AND p_actor <> v_recipient) THEN
    RETURN QUERY SELECT 403, NULL::text, NULL::text, NULL::timestamptz;
    RETURN;
  END IF;
  PERFORM pg_advisory_xact_lock(hashtextextended(least(v_requester, v_recipient) || '|' || greatest(v_requester, v_recipient), 0));
  IF (SELECT count(*) FROM users WHERE id IN (v_requester, v_recipient) AND status = 'active') <> 2 THEN
    RETURN QUERY SELECT 404, NULL::text, NULL::text, NULL::timestamptz;
    RETURN;
  END IF;
  SELECT fr.status INTO v_status FROM friend_requests fr WHERE fr.id = p_request_id;
  IF v_status <> 'pending' THEN
    RETURN QUERY SELECT 403, NULL::text, NULL::text, NULL::timestamptz;
    RETURN;
  END IF;
  IF p_status = 'accepted' THEN
    IF EXISTS(SELECT 1 FROM block_relations WHERE (blocker_id = v_requester AND blocked_id = v_recipient) OR (blocker_id = v_recipient AND blocked_id = v_requester)) THEN
      RETURN QUERY SELECT 409, NULL::text, NULL::text, NULL::timestamptz;
      RETURN;
    END IF;
    INSERT INTO friendships (id, user_low_id, user_high_id, source_request_id)
      VALUES (p_friendship_id, least(v_requester, v_recipient), greatest(v_requester, v_recipient), p_request_id);
  END IF;
  RETURN QUERY UPDATE friend_requests SET status = p_status, updated_at = clock_timestamp() WHERE id = p_request_id
    RETURNING 0, friend_requests.id::text, friend_requests.status, friend_requests.updated_at;
END $$;
