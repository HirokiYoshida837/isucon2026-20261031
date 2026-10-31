-- POST /api/admin/posting-window 用。advisory lock・conflict 判定・前の期間の ended_at・posting_windows の INSERT・pg_notify を 1 往復で行う。
-- 戻り値: o_code 0=作成（o_starts_at / o_on_time_deadline を返す）, 409=posting_window_conflict
-- starts_at は元の handler と同じく clock_timestamp()。pg_notify は COMMIT 時に届く（関数の中でも同じ）。
CREATE OR REPLACE FUNCTION open_posting_window(p_region text, p_local_date date, p_id text, p_seconds bigint)
RETURNS TABLE(o_code integer, o_starts_at timestamptz, o_on_time_deadline timestamptz)
LANGUAGE plpgsql AS $$
DECLARE
  v_starts timestamptz;
  v_deadline timestamptz;
BEGIN
  PERFORM pg_advisory_xact_lock(hashtext('posting_window_open:' || p_region));
  IF EXISTS (SELECT 1 FROM posting_windows w WHERE w.region = p_region AND w.local_date >= p_local_date) THEN
    RETURN QUERY SELECT 409, NULL::timestamptz, NULL::timestamptz;
    RETURN;
  END IF;
  v_starts := clock_timestamp();
  v_deadline := v_starts + p_seconds * interval '1 second';
  UPDATE posting_windows w SET ended_at = v_starts WHERE w.region = p_region AND w.ended_at IS NULL;
  INSERT INTO posting_windows (id, region, local_date, starts_at, on_time_deadline)
  VALUES (p_id, p_region, p_local_date, v_starts, v_deadline);
  -- 通知の行は書かない。hub が payload（region|id|starts_at の UNIX マイクロ秒）から作る
  PERFORM pg_notify('posting_window_open', p_region || '|' || p_id || '|' || (extract(epoch FROM v_starts) * 1000000)::bigint);
  RETURN QUERY SELECT 0, v_starts, v_deadline;
END $$;
