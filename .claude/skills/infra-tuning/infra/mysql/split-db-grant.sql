-- DB を別サーバーに分けるときのユーザー作成・権限付与（DB サーバー上で実行する）
-- 実行: sudo mysql < split-db-grant.sql
-- 前提: アプリは isucon / isucon で接続している（問題の env.sh 等で要確認）。
-- 192.168.0.% は VPC 内のプライベート IP 帯の例。ip -4 addr で実際の帯を確認して書き換える。
-- skip_name_resolve = ON のため、ホスト部はホスト名ではなく IP かワイルドカードで書く。

CREATE USER IF NOT EXISTS 'isucon'@'192.168.0.%' IDENTIFIED BY 'isucon';
GRANT ALL PRIVILEGES ON *.* TO 'isucon'@'192.168.0.%';
FLUSH PRIVILEGES;

-- 確認
SELECT user, host, plugin FROM mysql.user WHERE user = 'isucon';
