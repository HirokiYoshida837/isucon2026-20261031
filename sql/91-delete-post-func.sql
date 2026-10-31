-- DELETE /api/posts/:postId 用。users ロック → 投稿の存在・作者・その日の削除数の判定 → UPDATE を 1 往復で行う（#220）。
-- code: 0=成功, -1=ユーザーが無い（Go の sql.ErrNoRows 相当）, 404=post_not_found, 403=post_not_editable, 409=post_slot_unavailable
-- plpgsql は文ごとに新しいスナップショットを取るので、ユーザー行のロック待ちの間に commit された削除も削除数に入る（Go で 1 文ずつ流していたときと同じ）。
-- 1 文の CTE にまとめるとこの前提が崩れて削除上限（2 回）を超えるので、関数にする。
CREATE OR REPLACE FUNCTION delete_post(p_user_id text, p_post_id text) RETURNS integer
LANGUAGE plpgsql AS $$
DECLARE
  v_author text;
  v_date date;
BEGIN
  PERFORM 1 FROM users WHERE id = p_user_id FOR NO KEY UPDATE;
  IF NOT FOUND THEN
    RETURN -1;
  END IF;
  SELECT p.author_id, p.local_date INTO v_author, v_date
    FROM posts p JOIN posting_windows w ON w.id = p.posting_window_id
   WHERE p.id = p_post_id AND p.deleted_at IS NULL;
  IF NOT FOUND THEN
    RETURN 404;
  END IF;
  IF v_author <> p_user_id THEN
    RETURN 403;
  END IF;
  IF (SELECT count(*) FROM posts dp WHERE dp.author_id = p_user_id AND dp.deleted_at IS NOT NULL AND dp.local_date = v_date) >= 2 THEN
    RETURN 409;
  END IF;
  UPDATE posts SET deleted_at = clock_timestamp() WHERE id = p_post_id;
  RETURN 0;
END $$;
