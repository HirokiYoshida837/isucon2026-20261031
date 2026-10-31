# マイグレーション手法（ISUCON2026 予選 当日用）

- 対象: チーム 2 人、Go、MySQL 8 系（MariaDB の差分は都度注記）
- 決めた会議: 2026-10-25 事前準備会議（[Discord #事前準備](https://discord.com/channels/1563780097625034803/1563789733702926436)、メモは [meeting-2026-10-25.md](https://github.com/suecideTech/isucon2026-prep/blob/main/docs/meeting-2026-10-25.md)）
  - テーブル作り直し／過去データ引き継ぎの 2 パターンを用意する。
  - index 追加は initialize の手順に組み込む。
- テンプレート: このフォルダの `templates/` にある SQL・init.sh・Go（末尾の表）

## TL;DR

- ベンチは最初に `POST /api/initialize` を叩く。
- 参考実装はそこで `../sql/init.sh` を実行し、DB を初期状態に戻す。
- だから DB の変更は「init.sh で毎回再現される形」で入れる。
- 最初に `grep -n "DROP TABLE\|TRUNCATE\|DELETE" ~/webapp/sql/*` で初期化方式を見る。
- DROP → CREATE し直す回は「テーブル作り直しパターン」。スキーマファイルを直接書き換える。
- DROP しない回は「過去データ引き継ぎパターン」。冪等な ALTER とバックフィルで足していく。
- index は MySQL 8 なら `80-add-index.sql`（information_schema を見て無いときだけ追加）で入れる。
- initialize は 20〜42 秒でタイムアウトする。`time ~/webapp/sql/init.sh` を半分以内に保つ。
- 最後に「initialize 2 連続」「全台再起動後のベンチ」を必ず通す。

## 当日どれを選ぶか（判断フロー）

1. `~/webapp/sql/init.sh` と、そこから流れる SQL を全部読む。
2. スキーマ（CREATE TABLE）が init.sh から毎回流れるかを見る。
   - 流れる（`DROP TABLE IF EXISTS` → `CREATE TABLE`）: [3. テーブル作り直しパターン](#3-テーブル作り直しパターン) へ。
   - 流れない（TRUNCATE や DELETE だけ）: [4. 過去データ引き継ぎパターン](#4-過去データ引き継ぎパターン) へ。
3. index を足したいだけなら、どちらの回でも [2. index の追加](#2-index-の追加) の方法 B を使う。
   - 作り直し型の回は、スキーマファイルに書く方法 A でもよい。
4. 初期データの INSERT が列名なし（`INSERT INTO t VALUES (...)`）かを見る。
   - 列名なしなら、スキーマに列を足すと初期データ投入が落ちる。
   - 列は初期データを入れた後に ALTER で足す（`90-recompute.sql` の形）。
5. 画像が BLOB で DB に入っていたら [5. 画像を DB の外に出す](#5-画像を-db-の外に出すパターン) を検討する。
6. DB を別サーバーに移すときは [6. DB を別サーバーに分けたとき](#6-db-を別サーバーに分けたときの-initialize) を見てから移す。
7. 変更のたびに `time ~/webapp/sql/init.sh` を測る。
8. 迷ったら「追加だけで済む変更」を選ぶ（列・index・テーブルを足す。消す・型を変えるは後回し）。

## 1. initialize の仕組み

### 流れ

```
ベンチマーカー                 nginx → Go アプリ                       MySQL
    |  POST /api/initialize        |                                     |
    |----------------------------->|  exec ../sql/init.sh                |
    |                              |------------------------------------>| スキーマ再作成 / TRUNCATE
    |                              |                                     | 初期データ投入
    |                              |  追加処理（設定 UPDATE 等）          |
    |  200 {"language":"go"}       |                                     |
    |<-----------------------------|                                     |
    |  整合性チェック → 負荷走行（60 秒）                                   |
```

- Go の参考実装は `exec.Command("../sql/init.sh")` を呼ぶだけ。
- 例: isucon14 の [postInitialize](https://github.com/isucon/isucon14/blob/53f8b627e040c30ebec600457c6c97da008b84b0/webapp/go/main.go#L123-L142)。
- init.sh の後に、リクエストで渡された外部サービス URL を DB に保存する回が多い（isucon11・14）。
- マニュアルは「初期化処理が行っている内容を漏れなく提供してください」と書いている。
  - 出典: [isucon14 manual.md](https://github.com/isucon/isucon14/blob/main/docs/manual.md)「データベースのデータの初期化」
- レスポンスの `language` は言語賞の集計に使われる。`go` のまま返す。

### 過去回の実例

| 回 | 初期化のやり方 | タイムアウト | パターン |
|---|---|---|---|
| ISUCON11 予選 | `0_Schema.sql`（DROP → CREATE）＋ `1_InitData.sql`。DB は MariaDB | 20 秒 | 作り直し |
| ISUCON12 予選 | `init.sql` で競技中に増えた行だけ DELETE。SQLite ファイルはコピーで戻す | 30 秒 | 引き継ぎ |
| ISUCON13 | `init.sql` で TRUNCATE ＋ `initial_*.sql` を INSERT。スキーマは初回構築時の `initdb.d` だけ。DNS ゾーンも再作成 | 42 秒 | 引き継ぎ（スキーマ）＋作り直し（データ） |
| ISUCON14 | `1-schema.sql`（DROP → CREATE）＋マスタ＋`3-initial-data.sql.gz` | 30 秒 | 作り直し |

- 出典（init.sh）:
  - [isucon11-qualify webapp/sql/init.sh](https://github.com/isucon/isucon11-qualify/blob/main/webapp/sql/init.sh)
  - [isucon12-qualify webapp/sql/init.sql](https://github.com/isucon/isucon12-qualify/blob/main/webapp/sql/init.sql)
  - [isucon13 webapp/sql/init.sh](https://github.com/isucon/isucon13/blob/main/webapp/sql/init.sh) ／ [init.sql](https://github.com/isucon/isucon13/blob/main/webapp/sql/init.sql) ／ [initdb.d/10_schema.sql](https://github.com/isucon/isucon13/blob/main/webapp/sql/initdb.d/10_schema.sql)
  - [isucon14 webapp/sql/init.sh](https://github.com/isucon/isucon14/blob/main/webapp/sql/init.sh)
- 出典（タイムアウト）:
  - ISUCON11: [manual.md](https://github.com/isucon/isucon11-qualify/blob/main/docs/manual.md)「20 秒以内にレスポンスを返す必要があります」
  - ISUCON12: [当日マニュアル](https://gist.github.com/mackee/4320c18919c8f6f1867849378a17e651)「POST /initialize（30 秒でタイムアウト）」
  - ISUCON13: [cautionary_note.md](https://github.com/isucon/isucon13/blob/main/docs/cautionary_note.md)「POST /api/initialize（42秒でタイムアウト）」
  - ISUCON14: [manual.md](https://github.com/isucon/isucon14/blob/main/docs/manual.md)「POST /api/initialize（30秒でタイムアウト）」

### タイムアウトの目安

- 過去 4 回は 20〜42 秒。
- 当日マニュアルの値を最初に確認し、チームで共有する。
- 目標は「タイムアウトの半分以内」。ベンチ環境のほうが遅いことがあるため。
- 測り方: `time ~/webapp/sql/init.sh`（テンプレートの init.sh はファイルごとの秒数も出す）。
- 重い再計算を足したら必ず測り直す。
- 間に合わないときの逃げ道:
  - 再計算結果をダンプしておき、計算の代わりにロードする。
  - 初期データ由来の集計を事前に計算した SQL ファイルとしてリポジトリに入れる。

### 追試（最後に必ず効く制約）

- 競技後に主催者が全サーバーを再起動し、負荷走行をやり直す。
- 再起動後に fail すると失格。
- 上位チームは「負荷走行で書き込んだデータが再起動後に読めるか」も見られる。
- 出典: [isucon14 manual.md「追試」](https://github.com/isucon/isucon14/blob/main/docs/manual.md) ／ [isucon13 cautionary_note.md「追試」](https://github.com/isucon/isucon13/blob/main/docs/cautionary_note.md)
- だから DB の変更は「リポジトリの init.sh を流せば再現する」状態で終える。

## 2. index の追加

### 前提

- MySQL 8.0 の `CREATE INDEX` に `IF NOT EXISTS` はない（[MySQL 8.0 CREATE INDEX](https://dev.mysql.com/doc/refman/8.0/en/create-index.html)）。
- 同じ ALTER を 2 回流すと `ERROR 1061 Duplicate key name` で止まる（init.sh は `set -e` なので initialize が 500 になる）。
- MariaDB は `CREATE INDEX IF NOT EXISTS` と `ALTER TABLE ... ADD INDEX IF NOT EXISTS` が使える（[MariaDB CREATE INDEX](https://mariadb.com/kb/en/create-index/)）。

### 方法の比較

| 方法 | やり方 | 向いている回 | 向かない回 |
|---|---|---|---|
| A. スキーマに書く | `CREATE TABLE` の中に `INDEX idx_x (a, b)` を足す | 毎回 DROP → CREATE する回（11・14） | スキーマが init で流れない回（12・13）。本番に別途 ALTER が要る |
| B. 条件付き ALTER | information_schema を見て無いときだけ ALTER（[80-add-index.sql](templates/80-add-index.sql)） | どの回でも使える。冪等 | 特になし。SQL が少し長い |
| C. ALTER を init SQL に素で足す | `ALTER TABLE t ADD INDEX ...` を init.sql の末尾に書く | 毎回 DROP → CREATE する回だけ | DROP しない回。2 回目の initialize で 1061 が出る |
| D. Go 側で ALTER してエラー 1061 を無視 | [initialize_handler.go](templates/initialize_handler.go) の `idempotentDDL` | SQL ファイルを触りたくないとき | エラー判定の書き漏らし。init.sh を手で流しても index が付かない |

- 当日の標準は B にする。
  - 理由: 初期化方式を読み違えても壊れない。
- 作り直し型の回では A でもよい。
  - スキーマを読む人に index が見えるのが利点。
- `mysql --force` で C のエラーを無視する手もある。
  - 他のエラーまで握りつぶすので使わない方針にする。

### 方法 B の使い方

1. [80-add-index.sql](templates/80-add-index.sql) を `~/webapp/sql/` にコピーする。
2. 末尾の `CALL isu_add_index(...)` を書き換える。
3. 本番 DB に 1 回流す: `mysql -uisucon -pisucon <DB名> < 80-add-index.sql`。
4. [init.sh](templates/init.sh) の `SQL_FILES` で、初期データの後ろに置く。
5. `time ~/webapp/sql/init.sh` を 2 回続けて流し、2 回とも成功することを確認する。

- 初期データの後ろに置く理由: 大量 INSERT は index が少ない状態のほうが速い。
- 動作確認: isucon14 の SQL＋MySQL 8.0.46 で 3 回連続実行して成功（2026-10-26）。

### index 設計のメモ（当日迷わないため）

- WHERE の等価条件の列 → ORDER BY の列、の順で複合 index にする。
- `EXPLAIN` で `type=ALL` と `Using filesort` を潰す。
- スロークエリと APM で上位のクエリから順に張る（推測で大量に張らない）。

## 3. テーブル作り直しパターン

### いつ使うか

- init.sh がスキーマを毎回作り直す回（ISUCON11・14 型）。
- ISUCON13 型（TRUNCATE ＋ INSERT）でも、データは毎回入れ直すので「再計算」の部分は同じ。

### 手順

1. 変更後の姿を決める（非正規化カラム／集計テーブル／型変更）。
2. 初期データの INSERT に列名があるかを見る。
   - あり: スキーマファイルの `CREATE TABLE` を直接書き換えてよい。
   - なし: スキーマは触らず、初期データの後ろで `ALTER TABLE ... ADD COLUMN` する。
3. 初期データから値を再計算する SQL を書く（[90-recompute.sql](templates/90-recompute.sql)）。
4. init.sh の `SQL_FILES` の末尾に足す。
5. アプリの書き込み側を直す（INSERT・UPDATE 時に新しい列や集計テーブルも更新する）。
6. アプリの読み込み側を新しい列・テーブルに切り替える。
7. `time init.sh` → ベンチ。

### 非正規化カラム（UPDATE ... JOIN）

```sql
ALTER TABLE chairs ADD COLUMN latitude INT NULL, ADD COLUMN longitude INT NULL, ALGORITHM=INSTANT;

UPDATE chairs c
JOIN (
  SELECT chair_id, latitude, longitude,
         ROW_NUMBER() OVER (PARTITION BY chair_id ORDER BY created_at DESC) AS rn
  FROM chair_locations
) l ON l.chair_id = c.id AND l.rn = 1
SET c.latitude = l.latitude, c.longitude = l.longitude;
```

- 「最新の 1 件」を毎回サブクエリで引いているクエリに効く。
- 書き込み側（座標の POST）で `chairs` も UPDATE する。

### 集計テーブル（INSERT ... SELECT）

```sql
DROP TABLE IF EXISTS chair_total_distances;
CREATE TABLE chair_total_distances (...);
INSERT INTO chair_total_distances (chair_id, total_distance, total_distance_updated_at)
SELECT chair_id, SUM(IFNULL(distance, 0)), MAX(created_at)
FROM (SELECT chair_id, created_at,
             ABS(latitude  - LAG(latitude)  OVER (PARTITION BY chair_id ORDER BY created_at)) +
             ABS(longitude - LAG(longitude) OVER (PARTITION BY chair_id ORDER BY created_at)) AS distance
      FROM chair_locations) t
GROUP BY chair_id;
```

- 元ネタは isucon14 の [ownerGetChairs](https://github.com/isucon/isucon14/blob/main/webapp/go/owner_handlers.go) にある LAG() のサブクエリ。
- 書き込み側は `INSERT ... ON DUPLICATE KEY UPDATE` で加算する。
- 集計テーブルは自分で DROP → CREATE するので、DROP しない回でも冪等になる。
- ウィンドウ関数は MySQL 8.0 と MariaDB 10.2 以降で使える。

### カラム型変更

- 初期データの値がそのまま入る型なら、スキーマファイルを直接書き換える。
  - 例: `VARCHAR(255)` → `VARCHAR(26)`、`TEXT` → `VARCHAR(n)`（index を張りたいとき）。
- 入らない型なら、新しい列を足して変換する。
  - 例: 文字列の日時 → `DATETIME`、文字列 ID → 数値。
  - `ADD COLUMN new_col ...` → `UPDATE t SET new_col = 変換(old_col)` → アプリを new_col に切り替え。
- 型変更の ALTER は `ALGORITHM=COPY`（テーブル全体の書き直し）になる（[Online DDL](https://dev.mysql.com/doc/refman/8.0/en/innodb-online-ddl-operations.html)）。
  - 作り直し型なら毎回テーブルが空から作られるので気にしなくてよい。

## 4. 過去データ引き継ぎパターン

### いつ使うか

- initialize がスキーマを作り直さない回（ISUCON12・13 型）。
- initialize が一部の行しか戻さない回（ISUCON12: 競技中に増えた行だけ DELETE）。
- 初期データを流し直す SQL が無いテーブルがある回（ISUCON12 の MySQL ダンプは init では使われない）。
- 共通の制約: 今ある DB を壊さずに変える。何度流しても同じ結果にする。

### 原則

- 足すだけにする（Expand）。列・index・テーブルを追加し、既存の列は消さない。
- 埋める（Backfill）。追加した列や集計テーブルを既存データから計算する。
- 切り替える（Switch）。アプリの読み込みを新しい列に向ける。
- 消す（Contract）は競技中はやらない。戻せなくなるため。
- 派生データは、元データと一緒に init でリセットする。
  - 例: ISUCON12 で `visit_history` から集計テーブルを作ったら、`init.sql` の DELETE と同じ条件で集計テーブルも戻す（または作り直す）。

### 手順

1. 冪等な DDL を書く（[80-add-index.sql](templates/80-add-index.sql) の `isu_add_column` / `isu_add_index`）。
2. `ALGORITHM` を明示する（期待どおりにできないとき、エラーで止まって気づける）。
3. 本番 DB に 1 回流し、所要時間を測る。
4. バックフィルの SQL を書く。
   - 大きいテーブルは id の範囲で区切って UPDATE する（長いロックを避ける）。
5. 同じ DDL・バックフィルを init の流れに入れる（再現性のため）。
   - init で毎回やると遅いなら、「無いときだけ作る」「派生テーブルは作り直す」に分ける。
6. アプリの書き込み側 → 読み込み側の順に切り替える。
7. initialize 2 連続 → ベンチ。

### ALTER の ALGORITHM（MySQL 8.0）

| 操作 | ALGORITHM | 速さの目安 |
|---|---|---|
| 列の追加 | INSTANT（8.0.12 以降の既定。8.0.29 未満は末尾のみ） | 一瞬 |
| 列の削除 | INSTANT（8.0.29 以降） | 一瞬 |
| セカンダリ index の追加 | INPLACE, LOCK=NONE | 行数に比例。読み書きは止まらない |
| VARCHAR を伸ばす（長さバイト数が同じ範囲） | INPLACE | 一瞬 |
| 列の型変更 | COPY | テーブル全体を書き直す。書き込みが止まる |
| VIRTUAL 生成列の追加 | INSTANT | 一瞬 |
| STORED 生成列の追加 | COPY | テーブル全体を書き直す |
| VIRTUAL 生成列への index 追加 | INPLACE | 行数に比例 |

- 出典: [MySQL 8.0 Online DDL Operations](https://dev.mysql.com/doc/refman/8.0/en/innodb-online-ddl-operations.html)
- 書き方: `ALTER TABLE t ADD COLUMN c INT NOT NULL DEFAULT 0, ALGORITHM=INSTANT;`
- INSTANT にできない操作と同じ ALTER に混ぜない（全体が INSTANT でなくなる）。
- MariaDB の差分:
  - INSTANT の列追加は 10.3 以降（[Instant ADD COLUMN](https://mariadb.com/kb/en/instant-add-column-for-innodb/)）。
  - `ADD COLUMN IF NOT EXISTS` / `ADD INDEX IF NOT EXISTS` が使えるので、手続きは不要。
  - `STORED` の別名として `PERSISTENT` が使える。

### 生成列（バックフィル不要の近道）

```sql
-- 例: JSON の中の値で検索したい / 計算結果でソートしたい
ALTER TABLE t ADD COLUMN score_sort INT AS (likes * 10 + comments) VIRTUAL, ALGORITHM=INSTANT;
ALTER TABLE t ADD INDEX idx_score_sort (score_sort), ALGORITHM=INPLACE, LOCK=NONE;
```

- 同じ行の値だけで計算できるものに使える。
- 既存行も新しい行も自動で埋まるので、アプリの書き込み側を直さなくてよい。
- 他テーブルの値は使えない。その場合は非正規化カラムか集計テーブルにする。

### バックフィルを区切る例

```sql
-- id 1〜10000 ずつ。終わるまで範囲をずらして繰り返す
UPDATE posts p
JOIN (SELECT post_id, COUNT(*) AS cnt FROM comments WHERE post_id BETWEEN 1 AND 10000 GROUP BY post_id) c
  ON c.post_id = p.id
SET p.comment_count = c.cnt
WHERE p.id BETWEEN 1 AND 10000;
```

- 数十万行程度なら、区切らず 1 文で数秒のことが多い。まず 1 文で測る。

## 5. 画像を DB の外に出すパターン

### 過去の出題

- [private-isu](https://github.com/catatsuy/private-isu): `posts.imgdata`（mime 列で拡張子が違う）。
- ISUCON11 予選: `isu.image`（LONGBLOB）。取得 API はログイン中のユーザーの ISU かを確認する（[main.go の getIsuIcon](https://github.com/isucon/isucon11-qualify/blob/main/webapp/go/main.go)）。認可付きなので X-Accel-Redirect を使う。
- ISUCON13: `icons.image`（LONGBLOB）。`icon_hash` と `If-None-Match` で 304 を返してよい（MAY）。アイコン更新は 2 秒以内に反映が必要（[isupipe.md](https://github.com/isucon/isucon13/blob/main/docs/isupipe.md)「アイコン画像配信についての特記事項」）。
  - ISUCON13 の初期データにアイコンは無い（`init.sql` が TRUNCATE するだけ）。書き出しは不要で、initialize でディレクトリを空にすればよい。

### 手順

1. 既存の画像をファイルに書き出す（[export-images.sh](templates/export-images.sh)）。
   - 出力先は初期スナップショット用のディレクトリ（例: `public/icons_init`）にする。
2. nginx で静的配信する（下の設定例）。
3. アプリの書き込み側を「ファイルに書く」に変える。
   - 一時ファイルに書いて `os.Rename` する（読みかけのファイルを返さないため）。
   - DB の BLOB 列には書かない（転送量と行サイズが減る）。
4. アプリの読み込み側から `SELECT image` を消す。
   - ハッシュが必要なら、書き込み時に計算して列に保存する。
5. initialize で画像ディレクトリを初期状態に戻す。
   - テンプレートの init.sh は `IMAGE_DIR` と `IMAGE_INIT_DIR` を設定すると `rm -rf` → `cp -a` する。
6. 再起動後も画像が読めることを確認する（ファイルはディスクに残るので通常は問題ない）。

### nginx の設定例

```nginx
# 公開してよい画像: ファイルがあれば nginx が返し、無ければアプリへ
location /image/ {
    root /home/isucon/private_isu/webapp/public;
    expires 1d;
    try_files $uri @app;
}

# 認可が要る画像: アプリが確認してから X-Accel-Redirect で nginx に渡す
location /internal/isu_image/ {
    internal;
    alias /home/isucon/webapp/public/isu_image/;
}

location @app {
    proxy_pass http://app;
}
```

- アプリ側は認可を確認したら、`X-Accel-Redirect: /internal/isu_image/<uuid>` ヘッダを付けて空のボディを返す。
- 出典: [nginx internal](https://nginx.org/en/docs/http/ngx_http_core_module.html#internal) ／ [try_files](https://nginx.org/en/docs/http/ngx_http_core_module.html#try_files)
- 304 の条件が仕様で決まっている回（ISUCON13 など）は、仕様に合う方法をアプリ側で選ぶ。
  - nginx の ETag は SHA256 ではないので、`icon_hash` と比べる用途には使えない。

### 複数台構成での注意

- 画像ファイルは nginx が動くサーバーに置く。
- アプリと nginx を別サーバーにしたら、画像を書くアプリと配る nginx を同じサーバーにそろえる。
  - またはフロントの nginx から画像パスだけそのサーバーの nginx に proxy する。

## 6. DB を別サーバーに分けたときの initialize

### どこで SQL が流れるか

- init.sh はベンチのリクエストを受けた Go アプリのサーバーで動く。
- init.sh はそこから `mysql --host $DB_HOST` で DB サーバーに接続して SQL を流す。
- DB サーバーで SQL ファイルを置き直す必要はない。アプリ側の `~/webapp/sql/` が使われる。

### 移す前に確認すること

- 接続先の環境変数を両方そろえる。
  - Go アプリ: `/home/isucon/env.sh`（systemd の EnvironmentFile）。
  - init.sh: 同じ `/home/isucon/env.sh` を読む（isucon13・14 の init.sh がそうなっている）。
  - 変数名は回ごとに違う（isucon14: `ISUCON_DB_HOST`、isucon13: `ISUCON13_MYSQL_DIALCONFIG_ADDRESS`、isucon11: `MYSQL_HOST`）。
- DB サーバーの MySQL が外から受け付けるか。
  - `bind-address = 0.0.0.0`（`/etc/mysql/mysql.conf.d/mysqld.cnf`）。
  - `isucon@'%'` ユーザーがあるか（isucon14 は `0-init.sql` と構築時に作っている）。
  - セキュリティグループで 3306 がサーバー間で通るか。
- アプリサーバーで `mysql -h <DBサーバーのプライベートIP> -uisucon -pisucon` が通るか。
- 移した後に `time init.sh` を測り直す（ネットワーク越しの初期データ投入は遅くなる）。
- アプリサーバーの MySQL を止める（`sudo systemctl disable --now mysql`）。
  - 止め忘れると、古い接続先に書いていても気づきにくい。
  - 再起動試験で「止めたまま」になっていることも確認する。

### アプリを複数台にしたとき

- initialize は 1 台のアプリにしか届かない。
- オンメモリキャッシュを持つなら、他のアプリのキャッシュも捨てる仕組みが要る。
  - 例: initialize を受けたアプリが、他のアプリの内部 API（`/api/internal/reset` など）を叩く。
- 画像ディレクトリのリセットも、画像を置いた全サーバーで行う。

## 7. 失敗したときの戻し方

### コード

1. `git revert <コミット>` で戻す（履歴を残すので、どこまで戻したか追える）。
2. デプロイ・再起動する（Makefile の手順）。
3. `curl -X POST http://localhost/api/initialize -H 'Content-Type: application/json' -d '{}'` か init.sh で DB を戻す。
4. ベンチを流してスコアが戻ったか確認する。

### DB

- 作り直し型の回: revert して initialize すれば元のスキーマに戻る。
- 引き継ぎ型の回: revert してもスキーマは戻らない。
  - 追加した列・index は残っても無害なことが多い。
  - 列名なし INSERT や `NOT NULL`（DEFAULT なし）の列は既存の INSERT を壊す。逆向きの DDL（`DROP INDEX` / `DROP COLUMN`）を用意しておく。
- 危ない DDL の前にバックアップを取る（DB サーバーで実行）。
  - `mysqldump --single-transaction -uisucon -pisucon <DB名> | gzip > ~/backup-$(date +%H%M).sql.gz`
  - 戻すとき: `gzip -dc ~/backup-XXXX.sql.gz | mysql -uisucon -pisucon <DB名>`
- 初期データそのものを壊したら、元の初期化手段から作り直す。
  - ISUCON13: `initdb.d` のスキーマを流し直してから init.sh。
  - ISUCON12: `~/webapp/sql/admin/` のダンプ（マニュアル記載）。
  - 当日、競技開始直後に `mysqldump` を 1 本取っておくと安心。

## 本番提出前のチェックリスト

- [ ] リポジトリの init.sh と SQL が、本番 DB に入れた変更をすべて含んでいる。
- [ ] `mysqldump --no-data` の結果が、init.sh を流した直後のスキーマと一致する。
- [ ] `time ~/webapp/sql/init.sh` がタイムアウトの半分以内。
- [ ] initialize を 2 回続けて叩いても 2 回とも 200 になる。
- [ ] initialize の後、オンメモリキャッシュと画像ディレクトリも初期状態に戻る。
- [ ] DB 接続先の環境変数が全サーバーで正しい（`/home/isucon/env.sh`）。
- [ ] 使わないサーバーの MySQL が止まり、`disable` されている。
- [ ] nginx の設定（画像の静的配信など）がリポジトリに入っている。
- [ ] 全サーバーを再起動して、ベンチが通る。
- [ ] 再起動前にベンチで書いたデータが、再起動後も読める。
- [ ] initialize のレスポンスの `language` が `go`。
- [ ] スロークエリログ・APM などの計測を外した後にもう一度ベンチが通る。

## テンプレート一覧

| ファイル | 使いどころ |
|---|---|
| [init.sh](templates/init.sh) | 複数 SQL を順番に流す。DB ホストを環境変数で切り替える。所要秒数を出す |
| [80-add-index.sql](templates/80-add-index.sql) | MySQL 8 で index・列を「無いときだけ」追加する |
| [90-recompute.sql](templates/90-recompute.sql) | 初期データからの再計算（集計テーブル・非正規化カラム・型変更）の例 |
| [initialize_handler.go](templates/initialize_handler.go) | Go の initialize に冪等 DDL・キャッシュ破棄・所要時間ログを足す例 |
| [export-images.sh](templates/export-images.sh) | BLOB をファイルに書き出す |

- 動作確認（2026-10-26、ローカルの Docker で MySQL 8.0.46）:
  - isucon14 の SQL で init.sh を 2 回連続実行して成功（各 1 秒程度）。
  - 80-add-index.sql を 3 回連続で流して成功。素の ALTER は 2 回目に 1061 になることも確認。
  - export-images.sh で 300KB の乱数・改行やタブを含むバイナリを書き出し、SHA256 が一致。
  - initialize_handler.go はスタブを足して `go vet` が通ることを確認。
