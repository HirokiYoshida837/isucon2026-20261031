-- GET /api/users の検索（go/user_read_api.go）は idx_users_username_id_search_cover の Index Only Scan で全行を見る（#261）。
-- reset.sql が users の display_name を戻すので可視性マップが欠け、Heap Fetches が 1,500 ほど出る。初期化の最後に VACUUM して all-visible にする。
-- 344 ページ・約 30ms。トランザクションの外で流す（psql は autocommit。reset.sql の BEGIN/COMMIT の後）。ANALYZE は付けない（plan を変えない）
VACUUM users;
