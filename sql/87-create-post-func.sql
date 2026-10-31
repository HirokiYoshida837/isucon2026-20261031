-- POST /api/posts 用。users ロック → 期間と枠の判定 → image_objects・image_keys・posts の INSERT を 1 往復で行う（#170）。
-- code: 0=成功, 404=ユーザーが無い（Go の sql.ErrNoRows 相当）, 409=post_slot_unavailable
-- 失敗のときは Go の User（string 列）に入るよう user の列を空文字で返す
-- plpgsql は文ごとに新しいスナップショットを取るので、ユーザー行のロック待ちの間に commit された投稿も枠の判定で見える（Go で 1 文ずつ流していたときと同じ）。
-- 1 文の CTE にまとめるとこの前提が崩れて二重投稿が起きるので、関数にする。
CREATE OR REPLACE FUNCTION create_post(p_user_id text, p_post_id text, p_image_id text, p_caption text, p_visibility text,
  p_location_kind text, p_latitude numeric, p_longitude numeric, p_locality text, p_data bytea)
RETURNS TABLE(code integer, r_id text, r_username text, r_secret_key_hash text, r_display_name text, r_bio text,
  r_profile_image_id text, r_region text, r_account_type text, r_status text,
  r_window_id text, r_on_time_deadline timestamptz, r_posted_at timestamptz)
LANGUAGE plpgsql AS $$
DECLARE
  u users%ROWTYPE;
  w_id text;
  w_starts timestamptz;
  w_deadline timestamptz;
  w_date date;
  n_active bigint;
  n_deleted bigint;
  v_posted timestamptz;
BEGIN
  SELECT * INTO u FROM users WHERE id = p_user_id AND status = 'active' FOR NO KEY UPDATE;
  IF NOT FOUND THEN
    RETURN QUERY SELECT 404, ''::text, ''::text, ''::text, ''::text, ''::text, NULL::text, ''::text, ''::text, ''::text, NULL::text, NULL::timestamptz, NULL::timestamptz;
    RETURN;
  END IF;
  -- 期間の切り替えと重なると空になるので 1 回だけ取り直す（新しいスナップショットで次の期間が見える）
  FOR i IN 1..2 LOOP
    SELECT w.id, w.local_date, w.starts_at, w.on_time_deadline, counts.active, counts.deleted
      INTO w_id, w_date, w_starts, w_deadline, n_active, n_deleted
      FROM posting_windows w CROSS JOIN LATERAL (
        SELECT count(*) FILTER (WHERE p.deleted_at IS NULL) AS active,
               count(*) FILTER (WHERE p.deleted_at IS NOT NULL) AS deleted
        FROM posts p
        WHERE p.author_id = p_user_id AND p.local_date = w.local_date
      ) counts
     WHERE w.region = u.region AND w.ended_at IS NULL FOR SHARE OF w;
    EXIT WHEN FOUND;
  END LOOP;
  v_posted := clock_timestamp();
  IF w_id IS NULL OR n_active <> 0 OR n_deleted >= 2 OR v_posted < w_starts THEN
    RETURN QUERY SELECT 409, ''::text, ''::text, ''::text, ''::text, ''::text, NULL::text, ''::text, ''::text, ''::text, NULL::text, NULL::timestamptz, NULL::timestamptz;
    RETURN;
  END IF;
  INSERT INTO image_objects (id, owner_user_id, data, media_type, created_at) VALUES (p_image_id, p_user_id, p_data, 'image/jpeg', v_posted);
  INSERT INTO image_keys (id, created_at, owner_user_id) VALUES (p_image_id, v_posted, p_user_id);
  INSERT INTO posts (id, author_id, posting_window_id, image_id, caption, visibility,
                     location_kind, latitude, longitude, locality, posted_at, local_date)
  VALUES (p_post_id, p_user_id, w_id, p_image_id, p_caption, p_visibility,
          p_location_kind, p_latitude, p_longitude, p_locality, v_posted, w_date);
  RETURN QUERY SELECT 0, u.id::text, u.username::text, u.secret_key_hash::text, u.display_name::text, u.bio::text,
    u.profile_image_id::text, u.region::text, u.account_type::text, u.status::text, w_id, w_deadline, v_posted;
END $$;
