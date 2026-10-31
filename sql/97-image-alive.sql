-- GET /api/images の生存確認（EXISTS 3 本）を image_keys.alive に持たせる。読むときは image_keys + users の PK 引きだけにする。
-- alive の定義は従来の問い合わせと同じ（image_is_alive）。画像に関わる行（posts / realmojis / users）が変わった文の最後に、トリガーで再計算する。
-- 初期化の reset.sql は session_replication_role=replica で動きトリガーが走らないので、復元した users に関わる画像はこのファイルの最後で再計算する。
-- 列を足した直後（alive が 1 つも true でない）のときだけ全件を埋める。約 1.8M 行で重いので、デプロイ前に一度流しておく。
DO $$
BEGIN
  IF NOT EXISTS (SELECT 1 FROM information_schema.columns WHERE table_name = 'image_keys' AND column_name = 'alive') THEN
    ALTER TABLE image_keys ADD COLUMN alive boolean NOT NULL DEFAULT false;
  END IF;
END $$;

CREATE OR REPLACE FUNCTION image_is_alive(p_id text, p_owner text) RETURNS boolean
LANGUAGE sql STABLE AS $$
  SELECT COALESCE((SELECT u.profile_image_id = p_id FROM users u WHERE u.id = p_owner), false)
    OR EXISTS (
      SELECT 1 FROM posts p
      WHERE p.image_id = p_id AND p.author_id = p_owner AND p.deleted_at IS NULL
    )
    OR EXISTS (
      SELECT 1 FROM realmojis r
      JOIN posts p ON p.id = r.post_id AND p.deleted_at IS NULL
      JOIN users author ON author.id = p.author_id AND author.status = 'active'
      WHERE r.image_id = p_id AND r.reactor_id = p_owner AND r.replaced_at IS NULL AND r.deleted_at IS NULL
    )
$$;

CREATE OR REPLACE FUNCTION refresh_image_alive(p_ids text[]) RETURNS void
LANGUAGE sql AS $$
  UPDATE image_keys k SET alive = v.a
  FROM (SELECT i.id, image_is_alive(i.id, i.owner_user_id) AS a FROM image_keys i WHERE i.id = ANY(p_ids)) v
  WHERE k.id = v.id AND k.alive IS DISTINCT FROM v.a
$$;

CREATE OR REPLACE FUNCTION trg_image_keys_alive() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
  PERFORM refresh_image_alive(ARRAY[NEW.id]::text[]);
  RETURN NULL;
END $$;

CREATE OR REPLACE FUNCTION trg_posts_alive() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
  IF TG_OP = 'INSERT' THEN
    PERFORM refresh_image_alive(ARRAY[NEW.image_id]::text[]);
  ELSE
    -- 投稿の削除は、その投稿への realmoji の画像にも効く
    PERFORM refresh_image_alive(ARRAY[NEW.image_id]::text[] || ARRAY(SELECT r.image_id FROM realmojis r WHERE r.post_id = NEW.id)::text[]);
  END IF;
  RETURN NULL;
END $$;

CREATE OR REPLACE FUNCTION trg_realmojis_alive() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
  PERFORM refresh_image_alive(ARRAY[NEW.image_id]::text[]);
  RETURN NULL;
END $$;

CREATE OR REPLACE FUNCTION trg_users_profile_alive() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
  IF TG_OP = 'INSERT' THEN
    PERFORM refresh_image_alive(ARRAY[NEW.profile_image_id]::text[]);
  ELSE
    PERFORM refresh_image_alive(ARRAY[OLD.profile_image_id, NEW.profile_image_id]::text[]);
  END IF;
  RETURN NULL;
END $$;

-- 作者が active でなくなると、その人の投稿への realmoji の画像が死ぬ
CREATE OR REPLACE FUNCTION trg_users_status_alive() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
  PERFORM refresh_image_alive(ARRAY(SELECT r.image_id FROM realmojis r JOIN posts p ON p.id = r.post_id WHERE p.author_id = NEW.id)::text[]);
  RETURN NULL;
END $$;

DROP TRIGGER IF EXISTS image_keys_alive ON image_keys;
CREATE TRIGGER image_keys_alive AFTER INSERT ON image_keys FOR EACH ROW EXECUTE FUNCTION trg_image_keys_alive();
DROP TRIGGER IF EXISTS posts_alive_ins ON posts;
CREATE TRIGGER posts_alive_ins AFTER INSERT ON posts FOR EACH ROW EXECUTE FUNCTION trg_posts_alive();
DROP TRIGGER IF EXISTS posts_alive_del ON posts;
CREATE TRIGGER posts_alive_del AFTER UPDATE OF deleted_at ON posts FOR EACH ROW
  WHEN (OLD.deleted_at IS DISTINCT FROM NEW.deleted_at) EXECUTE FUNCTION trg_posts_alive();
DROP TRIGGER IF EXISTS realmojis_alive_ins ON realmojis;
CREATE TRIGGER realmojis_alive_ins AFTER INSERT ON realmojis FOR EACH ROW EXECUTE FUNCTION trg_realmojis_alive();
DROP TRIGGER IF EXISTS realmojis_alive_upd ON realmojis;
CREATE TRIGGER realmojis_alive_upd AFTER UPDATE OF replaced_at, deleted_at ON realmojis FOR EACH ROW
  WHEN (OLD.replaced_at IS DISTINCT FROM NEW.replaced_at OR OLD.deleted_at IS DISTINCT FROM NEW.deleted_at) EXECUTE FUNCTION trg_realmojis_alive();
DROP TRIGGER IF EXISTS users_profile_alive_ins ON users;
CREATE TRIGGER users_profile_alive_ins AFTER INSERT ON users FOR EACH ROW
  WHEN (NEW.profile_image_id IS NOT NULL) EXECUTE FUNCTION trg_users_profile_alive();
DROP TRIGGER IF EXISTS users_profile_alive_upd ON users;
CREATE TRIGGER users_profile_alive_upd AFTER UPDATE OF profile_image_id ON users FOR EACH ROW
  WHEN (OLD.profile_image_id IS DISTINCT FROM NEW.profile_image_id) EXECUTE FUNCTION trg_users_profile_alive();
DROP TRIGGER IF EXISTS users_status_alive ON users;
CREATE TRIGGER users_status_alive AFTER UPDATE OF status ON users FOR EACH ROW
  WHEN (OLD.status IS DISTINCT FROM NEW.status) EXECUTE FUNCTION trg_users_status_alive();

-- 列を足した直後だけ全件を埋める
DO $$
BEGIN
  IF NOT EXISTS (SELECT 1 FROM image_keys WHERE alive) THEN
    UPDATE image_keys k SET alive = image_is_alive(k.id, k.owner_user_id);
  END IF;
END $$;

-- reset.sql が復元した users（profile_image_id / status が戻る）に関わる画像を再計算する
DO $$
BEGIN
  IF to_regclass('pg_temp.restored_users') IS NOT NULL THEN
    PERFORM refresh_image_alive(
      ARRAY(SELECT u.profile_image_id FROM users u JOIN restored_users ru ON ru.id = u.id WHERE u.profile_image_id IS NOT NULL)::text[]
      || ARRAY(SELECT r.image_id FROM realmojis r JOIN posts p ON p.id = r.post_id JOIN restored_users ru ON ru.id = p.author_id)::text[]);
  END IF;
END $$;
