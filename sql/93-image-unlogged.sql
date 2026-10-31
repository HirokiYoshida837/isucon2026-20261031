-- 走行中の画像（投稿・リアル文字・プロフィール画像）が入る image_objects_20261028 を UNLOGGED にして、画像本体（約 400KB/件）の WAL を書かない（#245）。
-- このパーティションは初期データの境界（10-28 01:36）より後だけで、reset.sql が毎回 TRUNCATE するので空のときに切り替わる。
-- 正常 shutdown（pg_ctlcluster -m fast）なら中身は残る。クラッシュ・強制停止だと空になって起動する。
-- 戻し方: ALTER TABLE image_objects_20261028 SET LOGGED;（このファイルを外すだけでは戻らない。空のときに流せば一瞬）
-- 2 回目以降の初期化で ACCESS EXCLUSIVE を取りに行かないよう、LOGGED のときだけ切り替える
DO $$
BEGIN
  IF (SELECT relpersistence FROM pg_class WHERE oid = 'image_objects_20261028'::regclass) = 'p' THEN
    ALTER TABLE image_objects_20261028 SET UNLOGGED;
  END IF;
END $$;
