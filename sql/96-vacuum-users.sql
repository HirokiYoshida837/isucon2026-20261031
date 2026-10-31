-- reset.sql の UPDATE users（search_display の復元）で全ページの visibility map が消え、
-- GET /api/users の Index Only Scan が heap を読みに行く（Heap Fetches 1,459/回）。初期化の最後に VACUUM して戻す（#248、約 50ms）。
-- VACUUM はトランザクションの中では実行できないので、独立したファイルにして psql の autocommit で流す
VACUUM users;
