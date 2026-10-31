-- =====================================================================
-- 条件付き index / カラム追加（MySQL 8 向け）
-- =====================================================================
-- 目的: 何回流しても同じ結果になる DDL を init.sh に組み込む。
-- 背景: MySQL 8 には CREATE INDEX IF NOT EXISTS がない。
--       2 回目の ALTER ... ADD INDEX は 1061 Duplicate key name で落ちる。
-- 方法: information_schema を見て、無いときだけ ALTER を実行する。
-- 使い方:
--   1. 末尾の CALL を、足したい index の数だけ書く。
--   2. init.sh の SQL_FILES で、初期データ投入の後ろに置く。
--   3. 本番 DB に 1 回手で流して効果を確認する。
--      mysql -uisucon -pisucon <DB名> < 80-add-index.sql
-- MariaDB の場合: このファイルは不要。次の構文がそのまま使える。
--   CREATE INDEX IF NOT EXISTS idx_x ON t (a, b);
--   ALTER TABLE t ADD COLUMN IF NOT EXISTS c INT NULL;
-- 前提: mysql コマンドに DB 名を渡して実行する（DATABASE() を使うため）。
-- =====================================================================

DROP PROCEDURE IF EXISTS isu_add_index;
DROP PROCEDURE IF EXISTS isu_add_column;

DELIMITER //

-- index が無いときだけ追加する。
-- p_def には ADD 以降をそのまま書く（例: 'INDEX idx_x (a, b)'、'UNIQUE idx_y (c)'）。
CREATE PROCEDURE isu_add_index(IN p_table VARCHAR(64), IN p_index VARCHAR(64), IN p_def TEXT)
BEGIN
  IF NOT EXISTS (
    SELECT 1 FROM information_schema.statistics
    WHERE table_schema = DATABASE()
      AND table_name = p_table
      AND index_name = p_index
  ) THEN
    SET @isu_ddl = CONCAT('ALTER TABLE `', p_table, '` ADD ', p_def);
    PREPARE isu_stmt FROM @isu_ddl;
    EXECUTE isu_stmt;
    DEALLOCATE PREPARE isu_stmt;
  END IF;
END//

-- カラムが無いときだけ追加する（過去データ引き継ぎパターン用）。
-- p_def には ADD COLUMN 以降をそのまま書く（例: 'c INT NULL, ALGORITHM=INSTANT'）。
CREATE PROCEDURE isu_add_column(IN p_table VARCHAR(64), IN p_column VARCHAR(64), IN p_def TEXT)
BEGIN
  IF NOT EXISTS (
    SELECT 1 FROM information_schema.columns
    WHERE table_schema = DATABASE()
      AND table_name = p_table
      AND column_name = p_column
  ) THEN
    SET @isu_ddl = CONCAT('ALTER TABLE `', p_table, '` ADD COLUMN ', p_def);
    PREPARE isu_stmt FROM @isu_ddl;
    EXECUTE isu_stmt;
    DEALLOCATE PREPARE isu_stmt;
  END IF;
END//

DELIMITER ;

-- ---------------------------------------------------------------------
-- ここから下を当日書き換える（例は isucon14 のテーブル）
-- ---------------------------------------------------------------------
CALL isu_add_index('chair_locations', 'idx_chair_id_created_at', 'INDEX idx_chair_id_created_at (chair_id, created_at)');
CALL isu_add_index('rides', 'idx_chair_id_updated_at', 'INDEX idx_chair_id_updated_at (chair_id, updated_at)');
CALL isu_add_index('ride_statuses', 'idx_ride_id_created_at', 'INDEX idx_ride_id_created_at (ride_id, created_at)');

-- カラム追加の例（INSTANT で一瞬で終わる。MySQL 8.0.29 未満は末尾追加のみ）
-- CALL isu_add_column('chairs', 'total_distance', 'total_distance INT NOT NULL DEFAULT 0, ALGORITHM=INSTANT');

-- 後片付け（スキーマ差分に手続きを残さない）
DROP PROCEDURE IF EXISTS isu_add_index;
DROP PROCEDURE IF EXISTS isu_add_column;
