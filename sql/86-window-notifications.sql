-- 走行中の通知だけを持つ表（#166）。notifications は index が 450MB あり、asia の切り替えの 4,867 行 INSERT がランダムなディスク読みで 1 秒かかる。
-- hub が読むのは今開いている投稿期間（走行中に作った行）の通知だけなので、初期データの notifications は読まない。
-- PK を (posting_window_id, user_id) にして 1 回の INSERT を index の連続した範囲に入れる。FK は持たない（user_id は INSERT ... SELECT FROM users が保証する）
CREATE TABLE IF NOT EXISTS window_notifications (
  posting_window_id varchar(36) NOT NULL,
  user_id varchar(36) NOT NULL,
  id varchar(36) NOT NULL,
  created_at timestamptz NOT NULL,
  PRIMARY KEY (posting_window_id, user_id)
);
TRUNCATE window_notifications;
