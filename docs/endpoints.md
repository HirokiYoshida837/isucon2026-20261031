# エンドポイント一覧

- 出典は `go/server.go` のルーティング
- 読む・書くテーブルは、ハンドラと呼び出し先の SQL 文字列から機械的に抽出した。SQL を組み立てる箇所は漏れる可能性がある
- 全ルートで `IPBanMiddleware`（`ip_bans` を読む）と `ActionLogMiddleware` が動く
- `session` は `SessionMiddleware`（`login_sessions`・`users` を読む）。`admin` は `AdminSessionMiddleware`（`administrator_sessions` を読む）
- `region` は `RegionMiddleware`。利用者のリージョンとサーバーのリージョンが違うと `403`（`go/region.go`）
- `/api/` 以外は nginx が `public/` から返す。`location /` は `try_files $uri /index.html`（asia1 の `/etc/nginx/sites-enabled/isureal.conf`）

| メソッド | パス | ハンドラ | 読むテーブル | 書くテーブル | 認証 |
|---|---|---|---|---|---|
| POST | `/api/initialize` | `initializeHandler`（initialize.go） | - | - | - |
| POST | `/api/auth/register` | `registerHandler`（user_api.go） | users | users | - |
| POST | `/api/auth/login` | `loginHandler`（user_api.go） | login_sessions users | login_sessions | - |
| POST | `/api/auth/logout` | `logoutHandler`（user_api.go） | users | login_sessions | session |
| GET | `/api/users` | `searchUsersHandler`（user_read_api.go） | block_relations users | - | session,region |
| GET | `/api/users/:userId` | `getUserHandler`（user_read_api.go） | block_relations friendships users | - | session,region |
| GET | `/api/me/export` | `exportPostsHandler`（export_api.go） | image_objects posting_windows posts users | - | session,region |
| GET | `/api/me` | `getMeHandler`（user_api.go） | users | - | session,region |
| PATCH | `/api/me` | `updateMeHandler`（user_api.go） | users | users | session |
| PUT | `/api/me/profile-image` | `putProfileImageHandler`（profile_image_api.go） | image_objects users | image_objects users | session |
| DELETE | `/api/me/profile-image` | `deleteProfileImageHandler`（profile_image_api.go） | image_objects users | image_objects users | session |
| POST | `/api/me/account-deletion` | `deleteAccountHandler`（account_deletion_api.go） | users | login_sessions users | session,region |
| POST | `/api/posts` | `createPostHandler`（post_api.go） | posting_windows posts users | image_objects posts | session,region |
| GET | `/api/posting-window/current` | `getCurrentPostingWindowHandler`（posting_window_api.go） | posting_windows posts users | - | session,region |
| GET | `/api/posts/me/current` | `getMyCurrentPostHandler`（post_api.go） | posting_windows posts users | - | session,region |
| GET | `/api/posts/:postId` | `getPostHandler`（post_api.go） | block_relations friendships posting_windows posts users | - | session,region |
| PATCH | `/api/posts/:postId` | `updatePostHandler`（post_api.go） | posting_windows posts users | posts | session,region |
| DELETE | `/api/posts/:postId` | `deletePostHandler`（post_api.go） | posting_windows posts users | posts | session,region |
| GET | `/api/feeds/:feed` | `getFeedHandler`（feed_api.go） | block_relations comments follows friendships posting_windows posts realmojis users | - | session,region |
| GET | `/api/memories` | `getMemoriesHandler`（memory_api.go） | comments posting_windows posts realmojis users | - | session,region |
| GET | `/api/memories/:date` | `getMemoriesByDateHandler`（memory_api.go） | comments posting_windows posts realmojis users | - | session,region |
| POST | `/api/posts/:postId/comments` | `createCommentHandler`（comment_api.go） | block_relations comments friendships posting_windows posts users | comments | session,region |
| GET | `/api/posts/:postId/comments` | `getCommentsHandler`（comment_api.go） | block_relations comments friendships posting_windows posts users | - | session,region |
| DELETE | `/api/comments/:commentId` | `deleteCommentHandler`（comment_api.go） | comments posts users | comments | session,region |
| POST | `/api/posts/:postId/realmojis` | `replaceRealMojiHandler`（realmoji_api.go） | block_relations friendships posting_windows posts realmojis users | image_objects realmojis | session,region |
| GET | `/api/posts/:postId/realmojis` | `getRealMojisHandler`（realmoji_api.go） | block_relations friendships posting_windows posts realmojis users | - | session,region |
| DELETE | `/api/realmojis/:realMojiId` | `deleteRealMojiHandler`（realmoji_api.go） | posts realmojis users | realmojis | session,region |
| GET | `/api/images/:imageId` | `getImageHandler`（image_api.go） | image_objects posts realmojis users | - | - |
| GET | `/api/notifications` | `getNotificationsHandler`（notification_api.go） | notifications posting_windows users | - | session,region |
| POST | `/api/friend-requests` | `createFriendRequestHandler`（friend_request_api.go） | block_relations friend_requests friendships users | friend_requests friendships | session,region |
| POST | `/api/friend-requests/:requestId/:action` | `transitionFriendRequestHandler`（friend_request_api.go） | block_relations friend_requests users | friend_requests friendships | session,region |
| GET | `/api/relationships/:kind` | `getRelationshipsHandler`（relationship_list_api.go） | follows friend_requests friendships users | - | session,region |
| DELETE | `/api/friendships/:userId` | `deleteFriendshipHandler`（relationship_api.go） | friendships users | friendships | session,region |
| PUT | `/api/follows/:userId` | `followHandler`（relationship_api.go） | block_relations follows users | follows | session,region |
| DELETE | `/api/follows/:userId` | `unfollowHandler`（relationship_api.go） | follows users | follows | session,region |
| PUT | `/api/blocks/:userId` | `blockHandler`（relationship_api.go） | block_relations follows friendships users | block_relations follows friend_requests friendships | session,region |
| DELETE | `/api/blocks/:userId` | `unblockHandler`（relationship_api.go） | block_relations users | block_relations | session,region |
| POST | `/api/admin/auth/login` | `adminLoginHandler`（admin_api.go） | administrator_sessions administrators | administrator_sessions | - |
| POST | `/api/admin/auth/logout` | `adminLogoutHandler`（admin_api.go） | - | administrator_sessions | admin |
| PUT | `/api/admin/users/:userId/certification` | `certifyUserHandler`（admin_user_api.go） | - | users | admin |
| DELETE | `/api/admin/users/:userId/certification` | `deleteUserCertificationHandler`（admin_user_api.go） | - | - | admin |
| POST | `/api/admin/posting-window` | `openPostingWindowHandler`（admin_posting_window_api.go） | posting_windows users | notifications posting_windows | admin |

## コードから分かること
- `DELETE /api/admin/users/:userId/certification` は常に `409`（`go/admin_user_api.go`）
- `GET /api/notifications` は SSE。接続ごとに 500ms の ticker が動く（`go/notification_api.go`）
- `GET /api/images/:imageId` は署名付き URL で、HMAC を 1000 回まわす（`go/image_signature.go`）
- クライアント IP は `X-Client-IP` を使う（`go/ip_ban.go`）
