-- image_objects は created_at のパーティションで、id だけで引くと全パーティションの PK を見る（#119 #121）。
-- id → created_at・持ち主の対応表を持ち、生存確認はこの表だけで、本体取得は created_at を足して 1 パーティションだけ見る。
-- 画像の INSERT / DELETE と同じ文で書く・消す（go/post_api.go, go/realmoji_api.go, go/profile_image_api.go）
CREATE TABLE IF NOT EXISTS image_keys (
  id varchar(36) PRIMARY KEY,
  created_at timestamptz NOT NULL,
  owner_user_id varchar(36) NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_image_keys_created_at ON image_keys (created_at);

-- reset.sql が image_objects の境界より後を消すのに合わせる
DELETE FROM image_keys WHERE created_at > (SELECT created_at FROM initial_data_boundary);

-- 空のときだけ初期データ分を埋める（重いのでデプロイ前に一度流しておく）
INSERT INTO image_keys (id, created_at, owner_user_id)
SELECT id, created_at, owner_user_id FROM image_objects
WHERE NOT EXISTS (SELECT 1 FROM image_keys);
