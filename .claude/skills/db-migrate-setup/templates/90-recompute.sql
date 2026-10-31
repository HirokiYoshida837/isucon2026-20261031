-- =====================================================================
-- テーブル作り直しパターン: 初期データからの再計算（例は isucon14）
-- =====================================================================
-- 前提: init.sh がスキーマを DROP → CREATE し、初期データを入れ直す回。
--       このファイルは init.sh で初期データ投入の後ろに流す。
-- 理由: 初期データの INSERT が列名なし（INSERT INTO t VALUES ...）だと、
--       スキーマ側に列を足した瞬間に Column count doesn't match で落ちる。
--       だから列は「初期データを入れた後」に ALTER で足す。
-- 確認: time ~/webapp/sql/init.sh が initialize のタイムアウトの半分以内に収まること。
-- =====================================================================

-- ---------------------------------------------------------------------
-- 例 1: 集計テーブル（椅子ごとの総移動距離）
--   元のクエリ: owner_handlers.go の ownerGetChairs にある LAG() のサブクエリ
--   アプリ側: POST /api/chair/coordinate で UPSERT して追従させる
-- ---------------------------------------------------------------------
DROP TABLE IF EXISTS chair_total_distances;
CREATE TABLE chair_total_distances (
  chair_id                  VARCHAR(26) NOT NULL,
  total_distance            INT         NOT NULL DEFAULT 0,
  total_distance_updated_at DATETIME(6) NULL,
  PRIMARY KEY (chair_id)
);

INSERT INTO chair_total_distances (chair_id, total_distance, total_distance_updated_at)
SELECT chair_id,
       SUM(IFNULL(distance, 0)),
       MAX(created_at)
FROM (
  SELECT chair_id,
         created_at,
         ABS(latitude  - LAG(latitude)  OVER (PARTITION BY chair_id ORDER BY created_at)) +
         ABS(longitude - LAG(longitude) OVER (PARTITION BY chair_id ORDER BY created_at)) AS distance
  FROM chair_locations
) t
GROUP BY chair_id;

-- ---------------------------------------------------------------------
-- 例 2: 非正規化カラム（椅子の最新座標を chairs に持たせる）
--   アプリ側: POST /api/chair/coordinate で chairs も UPDATE する
-- ---------------------------------------------------------------------
ALTER TABLE chairs
  ADD COLUMN latitude  INT NULL,
  ADD COLUMN longitude INT NULL,
  ALGORITHM=INSTANT;

UPDATE chairs c
JOIN (
  SELECT chair_id, latitude, longitude,
         ROW_NUMBER() OVER (PARTITION BY chair_id ORDER BY created_at DESC) AS rn
  FROM chair_locations
) l ON l.chair_id = c.id AND l.rn = 1
SET c.latitude = l.latitude,
    c.longitude = l.longitude;

-- ---------------------------------------------------------------------
-- 例 3: カラム型変更（文字列の日時を DATETIME にする等）
--   方法 A: 初期データの値がそのまま入る型なら、スキーマファイルを直接書き換える
--   方法 B: 入らないなら、新カラムを足して変換し、アプリの参照先を切り替える
-- ---------------------------------------------------------------------
-- ALTER TABLE t ADD COLUMN created_at_dt DATETIME(6) NULL, ALGORITHM=INSTANT;
-- UPDATE t SET created_at_dt = FROM_UNIXTIME(created_at_epoch);
-- ALTER TABLE t ADD INDEX idx_created_at_dt (created_at_dt);
