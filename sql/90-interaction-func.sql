-- POST /api/posts/:postId/realmojis と POST /api/posts/:postId/comments 用（#167）。
-- ペアロック → 存在・状態判定 → 閲覧可否 → INSERT → 反応者の users 列を返すところまでを 1 往復で行う（europe / america は 1 往復 100ms。6 往復 / 5 往復 → 1）。
-- plpgsql は文ごとに新しいスナップショットを取るので、ロック待ちの間に commit されたブロック・友達解除・投稿削除も判定で見える（sql/81・82・87 と同じ理屈）。
-- 失敗のときは Go の struct（string 列）に入るよう '' / 'epoch' を返し、Go は code だけ見る（87-create-post-func.sql と同じ流儀）。

-- go/post.go の getPostForViewer + postForViewer.canView と同じ判定。投稿が無い（削除済み・投稿者が退会）なら ok = false
CREATE OR REPLACE FUNCTION post_access(p_viewer text, p_post_id text,
  OUT ok boolean, OUT can_view boolean, OUT is_author boolean, OUT has_posted_today boolean,
  OUT window_ended boolean, OUT visibility text, OUT friends boolean)
LANGUAGE plpgsql AS $$
DECLARE r record;
BEGIN
  SELECT p.visibility::text AS visibility, w.ended_at IS NOT NULL AS window_ended,
    v.id IS NOT NULL AS viewer_active,
    p.author_id = p_viewer AS is_author,
    COALESCE(v.has_posted_today, false) AS has_posted_today,
    EXISTS(
      SELECT 1 FROM block_relations b
      WHERE (b.blocker_id = p_viewer AND b.blocked_id = p.author_id) OR (b.blocker_id = p.author_id AND b.blocked_id = p_viewer)
    ) AS blocked,
    EXISTS(
      SELECT 1 FROM friend_edges f WHERE f.user_id = p_viewer AND f.friend_id = p.author_id
    ) AS friends,
    EXISTS(
      SELECT 1 FROM friend_edges a
      JOIN friend_edges b ON b.user_id = a.friend_id AND b.friend_id = p.author_id
      WHERE a.user_id = p_viewer
    ) AS friend_of_friend
  INTO r
  FROM posts p
  JOIN posting_windows w ON w.id = p.posting_window_id
  JOIN users u ON u.id = p.author_id AND u.status = 'active'
  LEFT JOIN LATERAL (
    SELECT v.id, EXISTS(
      SELECT 1 FROM posts vp
      JOIN posting_windows current ON current.region = v.region AND current.ended_at IS NULL
      WHERE vp.author_id = v.id AND vp.local_date = current.local_date AND vp.deleted_at IS NULL
    ) AS has_posted_today
    FROM users v WHERE v.id = p_viewer AND v.status = 'active'
  ) v ON true
  WHERE p.id = p_post_id AND p.deleted_at IS NULL;
  ok := FOUND;
  IF NOT ok THEN
    can_view := false;
    RETURN;
  END IF;
  is_author := r.is_author;
  has_posted_today := r.has_posted_today;
  window_ended := r.window_ended;
  visibility := r.visibility;
  friends := r.friends;
  IF NOT r.viewer_active THEN can_view := false;
  ELSIF r.is_author THEN can_view := true;
  ELSIF r.window_ended OR NOT r.has_posted_today OR r.blocked THEN can_view := false;
  ELSIF r.visibility = 'public' THEN can_view := true;
  ELSIF r.visibility = 'friends' THEN can_view := r.friends;
  ELSIF r.visibility = 'friends_of_friends' THEN can_view := r.friends OR r.friend_of_friend;
  ELSE can_view := false;
  END IF;
END $$;

-- code: 0=成功, 403=interaction_forbidden, 500=Go の sql.ErrNoRows 相当（今の go/comment_api.go の分岐と 1 対 1）
CREATE OR REPLACE FUNCTION create_comment(p_actor text, p_post_id text, p_comment_id text, p_body text)
RETURNS TABLE(code integer, c_id text, c_post_id text, c_author_id text, c_body text, c_created_at timestamptz, c_deleted_at timestamptz,
  u_id text, u_username text, u_secret_key_hash text, u_display_name text, u_bio text, u_profile_image_id text,
  u_region text, u_account_type text, u_status text)
LANGUAGE plpgsql AS $$
DECLARE v_author text; v_user_count bigint; a record; v_code integer := 0;
BEGIN
  SELECT p.author_id::text INTO v_author FROM posts p WHERE p.id = p_post_id AND p.deleted_at IS NULL;
  IF NOT FOUND THEN
    v_code := 403;
  ELSE
    PERFORM pg_advisory_xact_lock(hashtextextended(least(p_actor, v_author) || '|' || greatest(p_actor, v_author), 0));
    SELECT count(*) INTO v_user_count FROM users WHERE id IN (p_actor, v_author);
    IF v_user_count = 0 OR (p_actor <> v_author AND v_user_count <> 2) THEN
      v_code := 500;
    ELSE
      SELECT * INTO a FROM post_access(p_actor, p_post_id);
      -- go/comment.go の canComment と同じ
      IF NOT a.ok OR NOT a.can_view OR a.window_ended THEN v_code := 403;
      ELSIF a.is_author THEN IF NOT a.has_posted_today THEN v_code := 403; END IF;
      ELSIF a.visibility <> 'public' AND NOT a.friends THEN v_code := 403;
      END IF;
    END IF;
  END IF;
  IF v_code <> 0 THEN
    RETURN QUERY SELECT v_code, ''::text, ''::text, ''::text, ''::text, 'epoch'::timestamptz, NULL::timestamptz,
      ''::text, ''::text, ''::text, ''::text, ''::text, NULL::text, ''::text, ''::text, ''::text;
    RETURN;
  END IF;
  RETURN QUERY
    WITH c AS (
      INSERT INTO comments(id,post_id,author_id,body,created_at) VALUES(p_comment_id, p_post_id, p_actor, p_body, clock_timestamp())
      RETURNING id,post_id,author_id,body,created_at,deleted_at
    )
    SELECT 0, c.id::text, c.post_id::text, c.author_id::text, c.body::text, c.created_at, c.deleted_at,
      u.id::text, u.username::text, u.secret_key_hash::text, u.display_name::text, u.bio::text, u.profile_image_id::text,
      u.region::text, u.account_type::text, u.status::text
    FROM c JOIN users u ON u.id = c.author_id AND u.status = 'active';
  IF NOT FOUND THEN
    RAISE EXCEPTION 'create_comment: author % is not active', p_actor;
  END IF;
END $$;

-- code: 0=成功, 403=interaction_forbidden, 404=relationship_not_found（今の go/realmoji_api.go の分岐と 1 対 1）
CREATE OR REPLACE FUNCTION create_realmoji(p_actor text, p_post_id text, p_realmoji_id text, p_image_id text, p_reaction_type text, p_data bytea)
RETURNS TABLE(code integer, r_id text, r_post_id text, r_reactor_id text, r_image_id text, r_reaction_type text,
  r_created_at timestamptz, r_replaced_at timestamptz, r_deleted_at timestamptz,
  u_id text, u_username text, u_secret_key_hash text, u_display_name text, u_bio text, u_profile_image_id text,
  u_region text, u_account_type text, u_status text)
LANGUAGE plpgsql AS $$
DECLARE v_author text; v_active bigint; a record; v_code integer := 0;
BEGIN
  SELECT p.author_id::text INTO v_author FROM posts p WHERE p.id = p_post_id AND p.deleted_at IS NULL;
  IF NOT FOUND THEN
    v_code := 403;
  ELSIF p_actor = v_author THEN
    v_code := 403;
  ELSE
    PERFORM pg_advisory_xact_lock(hashtextextended(least(p_actor, v_author) || '|' || greatest(p_actor, v_author), 0));
    SELECT count(*) INTO v_active FROM users WHERE id IN (p_actor, v_author) AND status = 'active';
    IF v_active <> 2 THEN
      v_code := 404;
    ELSE
      SELECT * INTO a FROM post_access(p_actor, p_post_id);
      IF NOT a.ok OR NOT a.can_view THEN v_code := 403; END IF;
    END IF;
  END IF;
  IF v_code <> 0 THEN
    RETURN QUERY SELECT v_code, ''::text, ''::text, ''::text, ''::text, ''::text, 'epoch'::timestamptz, NULL::timestamptz, NULL::timestamptz,
      ''::text, ''::text, ''::text, ''::text, ''::text, NULL::text, ''::text, ''::text, ''::text;
    RETURN;
  END IF;
  UPDATE realmojis SET replaced_at = clock_timestamp()
   WHERE post_id = p_post_id AND reactor_id = p_actor AND replaced_at IS NULL AND deleted_at IS NULL;
  RETURN QUERY
    WITH image AS (
      INSERT INTO image_objects(id,owner_user_id,data,media_type) VALUES(p_image_id, p_actor, p_data, 'image/jpeg')
      RETURNING id,created_at,owner_user_id
    ), image_key AS (
      INSERT INTO image_keys(id,created_at,owner_user_id) SELECT id,created_at,owner_user_id FROM image
    ), r AS (
      INSERT INTO realmojis(id,post_id,reactor_id,image_id,reaction_type,created_at)
      VALUES(p_realmoji_id, p_post_id, p_actor, p_image_id, p_reaction_type, clock_timestamp())
      RETURNING id,post_id,reactor_id,image_id,reaction_type,created_at,replaced_at,deleted_at
    )
    SELECT 0, r.id::text, r.post_id::text, r.reactor_id::text, r.image_id::text, r.reaction_type::text, r.created_at, r.replaced_at, r.deleted_at,
      u.id::text, u.username::text, u.secret_key_hash::text, u.display_name::text, u.bio::text, u.profile_image_id::text,
      u.region::text, u.account_type::text, u.status::text
    FROM r JOIN users u ON u.id = r.reactor_id AND u.status = 'active';
  IF NOT FOUND THEN
    RAISE EXCEPTION 'create_realmoji: reactor % is not active', p_actor;
  END IF;
END $$;
