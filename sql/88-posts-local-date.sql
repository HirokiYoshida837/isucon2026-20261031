-- posts に posting_windows.local_date を持たせ、「その日に投稿したか」を posting_windows の自己 JOIN なしで引く（#163）。
-- posting_windows.local_date は更新されないので値はずれない。書くのは create_post（87-create-post-func.sql）だけ。
-- 列があるときは ALTER しない（IF NOT EXISTS でも ACCESS EXCLUSIVE を取りに行く。#162）。
-- 列を足すときだけ既存行を埋める。重いのでデプロイ前に一度流しておく。
-- 列が無かった頃に入った投稿は境界より後なので reset.sql が消す。
DO $$
BEGIN
  IF NOT EXISTS (SELECT 1 FROM information_schema.columns
                 WHERE table_name = 'posts' AND column_name = 'local_date') THEN
    ALTER TABLE posts ADD COLUMN local_date date;
    UPDATE posts p SET local_date = w.local_date FROM posting_windows w WHERE w.id = p.posting_window_id;
  END IF;
END $$;
CREATE INDEX IF NOT EXISTS idx_posts_author_id_local_date ON posts (author_id, local_date);
