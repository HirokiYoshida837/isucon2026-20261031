-- POST /api/friend-requests 用。ペアロック・存在確認・状態判定・INSERT/accept を 1 往復で行う。
-- 戻り値の code: 0=成功, 404=relationship_not_found, 409=relationship_conflict
-- plpgsql は文ごとに新しいスナップショットを取るので、ロック待ちの間に commit された block / friendship も見える。
CREATE OR REPLACE FUNCTION create_friend_request(p_actor text, p_target text, p_id text, p_friendship_id text)
RETURNS TABLE(code integer, rid text, rstatus text, rupdated timestamptz)
LANGUAGE plpgsql AS $$
DECLARE
  v_pending_id text;
  v_pending_requester text;
BEGIN
  PERFORM pg_advisory_xact_lock(hashtextextended(least(p_actor, p_target) || '|' || greatest(p_actor, p_target), 0));
  IF (SELECT count(*) FROM users WHERE id IN (p_actor, p_target) AND status = 'active') <> 2 THEN
    RETURN QUERY SELECT 404, NULL::text, NULL::text, NULL::timestamptz;
    RETURN;
  END IF;
  IF EXISTS(SELECT 1 FROM block_relations WHERE (blocker_id = p_actor AND blocked_id = p_target) OR (blocker_id = p_target AND blocked_id = p_actor))
     OR EXISTS(SELECT 1 FROM friendships WHERE user_low_id = least(p_actor, p_target) AND user_high_id = greatest(p_actor, p_target)) THEN
    RETURN QUERY SELECT 409, NULL::text, NULL::text, NULL::timestamptz;
    RETURN;
  END IF;
  SELECT fr.id, fr.requester_id INTO v_pending_id, v_pending_requester FROM friend_requests fr
   WHERE fr.status = 'pending' AND ((fr.requester_id = p_actor AND fr.recipient_id = p_target) OR (fr.requester_id = p_target AND fr.recipient_id = p_actor))
   LIMIT 1;
  IF v_pending_id IS NULL THEN
    RETURN QUERY INSERT INTO friend_requests (id, requester_id, recipient_id, status, created_at, updated_at)
      VALUES (p_id, p_actor, p_target, 'pending', clock_timestamp(), clock_timestamp())
      RETURNING 0, friend_requests.id::text, friend_requests.status, friend_requests.updated_at;
  ELSIF v_pending_requester = p_actor THEN
    RETURN QUERY SELECT 409, NULL::text, NULL::text, NULL::timestamptz;
  ELSE
    INSERT INTO friendships (id, user_low_id, user_high_id, source_request_id)
      VALUES (p_friendship_id, least(p_actor, p_target), greatest(p_actor, p_target), v_pending_id);
    RETURN QUERY UPDATE friend_requests SET status = 'accepted', updated_at = clock_timestamp() WHERE id = v_pending_id
      RETURNING 0, friend_requests.id::text, friend_requests.status, friend_requests.updated_at;
  END IF;
END $$;

-- POST /api/friend-requests/:requestId/:action 用。権限判定・ペアロック・存在確認・状態判定・UPDATE/accept を 1 往復で行う。
-- p_status は accepted / rejected / cancelled（action の検証は呼び出し側）。
-- 戻り値の code: 0=成功, 403=relationship_action_forbidden, 404=relationship_not_found, 409=relationship_conflict
CREATE OR REPLACE FUNCTION transition_friend_request(p_actor text, p_request_id text, p_status text, p_friendship_id text)
RETURNS TABLE(code integer, rid text, rstatus text, rupdated timestamptz)
LANGUAGE plpgsql AS $$
DECLARE
  v_req text;
  v_rec text;
  v_cur text;
BEGIN
  SELECT fr.requester_id, fr.recipient_id INTO v_req, v_rec FROM friend_requests fr WHERE fr.id = p_request_id;
  IF NOT FOUND THEN
    RETURN QUERY SELECT 404, NULL::text, NULL::text, NULL::timestamptz;
    RETURN;
  END IF;
  IF (p_status = 'cancelled' AND p_actor <> v_req) OR (p_status <> 'cancelled' AND p_actor <> v_rec) THEN
    RETURN QUERY SELECT 403, NULL::text, NULL::text, NULL::timestamptz;
    RETURN;
  END IF;
  PERFORM pg_advisory_xact_lock(hashtextextended(least(v_req, v_rec) || '|' || greatest(v_req, v_rec), 0));
  IF (SELECT count(*) FROM users WHERE id IN (v_req, v_rec) AND status = 'active') <> 2 THEN
    RETURN QUERY SELECT 404, NULL::text, NULL::text, NULL::timestamptz;
    RETURN;
  END IF;
  SELECT fr.status INTO v_cur FROM friend_requests fr WHERE fr.id = p_request_id;
  IF v_cur <> 'pending' THEN
    RETURN QUERY SELECT 403, NULL::text, NULL::text, NULL::timestamptz;
    RETURN;
  END IF;
  IF p_status = 'accepted' THEN
    IF EXISTS(SELECT 1 FROM block_relations WHERE (blocker_id = v_req AND blocked_id = v_rec) OR (blocker_id = v_rec AND blocked_id = v_req)) THEN
      RETURN QUERY SELECT 409, NULL::text, NULL::text, NULL::timestamptz;
      RETURN;
    END IF;
    INSERT INTO friendships (id, user_low_id, user_high_id, source_request_id)
      VALUES (p_friendship_id, least(v_req, v_rec), greatest(v_req, v_rec), p_request_id);
  END IF;
  RETURN QUERY UPDATE friend_requests SET status = p_status, updated_at = clock_timestamp() WHERE id = p_request_id
    RETURNING 0, friend_requests.id::text, friend_requests.status, friend_requests.updated_at;
END $$;
