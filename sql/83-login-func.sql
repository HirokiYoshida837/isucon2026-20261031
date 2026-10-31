-- POST /api/auth/login 用。users ロック・status/hash 検証・旧セッション無効化・新セッション INSERT を 1 往復で行う。
-- code: 0=成功, 401=invalid_credentials
-- plpgsql は文ごとに新しいスナップショットを取るので、ロック待ちの間に commit された別ログインのセッションも UPDATE で見え、active は常に 1 本。
CREATE OR REPLACE FUNCTION login_user(p_username text, p_hash text, p_session_id text, p_token_hash text)
RETURNS TABLE(code integer, uid text, uname text, dname text, ubio text, uregion text, utype text, ustatus text, screated timestamptz)
LANGUAGE plpgsql AS $$
DECLARE
  u users%ROWTYPE;
  v_created timestamptz;
BEGIN
  SELECT * INTO u FROM users WHERE username = p_username FOR NO KEY UPDATE;
  IF NOT FOUND OR u.status <> 'active' OR u.secret_key_hash <> p_hash THEN
    RETURN QUERY SELECT 401, NULL::text, NULL::text, NULL::text, NULL::text, NULL::text, NULL::text, NULL::text, NULL::timestamptz;
    RETURN;
  END IF;
  UPDATE login_sessions SET active = false, invalidated_at = clock_timestamp(), reason = 'replaced'
   WHERE user_id = u.id AND active = true;
  INSERT INTO login_sessions (id, user_id, token_hash) VALUES (p_session_id, u.id, p_token_hash)
   RETURNING created_at INTO v_created;
  RETURN QUERY SELECT 0, u.id::text, u.username::text, u.display_name::text, u.bio::text, u.region::text, u.account_type::text, u.status::text, v_created;
END $$;
