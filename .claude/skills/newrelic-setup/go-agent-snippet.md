# NewRelic Go agent 差分パターン集（ISUCON 当日用、15 分で入れる）

主催者から渡される Go 実装に `github.com/newrelic/go-agent/v3`（v3.45.0）を入れる手順。
順番は「初期化 → ルーターの計装 → DB の計装 → 確認」。
当日の環境変数の配置と systemd の drop-in は `docs/newrelic-agent-install.md` の 0. と 2-3. に従う。

## 0. 依存を足す

```bash
cd <webapp>/go     # 問題の Go 実装のディレクトリ
go get github.com/newrelic/go-agent/v3/newrelic@v3.45.0
go get github.com/newrelic/go-agent/v3/integrations/nrmysql@latest   # DB が MySQL のとき
# ルーター用の統合は 2. の表の go get を追加する
go mod tidy
```

- 統合パッケージ（`integrations/*`）は本体と別モジュールなので、個別に `go get` する

## 1. 共通: 初期化（main の先頭に置く）

設定は環境変数 `NEW_RELIC_LICENSE_KEY` / `NEW_RELIC_APP_NAME` / `NEW_RELIC_ENABLED` から読む。
キーが無い・不正なときは APM 無効のままアプリを続行する（実機で動作確認済み）。

```go
import (
    "log"

    "github.com/newrelic/go-agent/v3/newrelic"
)

var nrApp *newrelic.Application

func initNewRelic() {
    var err error
    nrApp, err = newrelic.NewApplication(
        newrelic.ConfigFromEnvironment(),
        func(cfg *newrelic.Config) {
            if cfg.License == "" {
                cfg.Enabled = false
            }
        },
    )
    if err != nil {
        log.Printf("NewRelic disabled: %s", err.Error())
        nrApp, _ = newrelic.NewApplication(newrelic.ConfigEnabled(false))
    }
}
```

- `ConfigFromEnvironment()` は上記 3 つに加えて `NEW_RELIC_DISTRIBUTED_TRACING_ENABLED` / `NEW_RELIC_LOG` / `NEW_RELIC_LOG_LEVEL` なども読む
- `nrApp` が nil や無効でも `WrapHandleFunc` と各統合は素通しで動く（nil 安全）
- 最終提出前は `NEW_RELIC_ENABLED=false` にして再起動すれば通信が止まる（コードは残してよい）

## 2. ルーター別の計装

トランザクション名は、どの方式でも `GET /posts/{id}` のように「メソッド + ルートパターン」で付く。
URL の実値（`/posts/123`）でバラけない方式だけを載せる。

| ルーター | 使うもの | go get |
|---|---|---|
| 標準 net/http | `newrelic.WrapHandleFunc` / `WrapHandle`（本体に同梱） | 不要 |
| go-chi/chi v5 | `newrelic.WrapHandleFunc`（本体に同梱） | 不要 |
| gorilla/mux | `integrations/nrgorilla` | `go get github.com/newrelic/go-agent/v3/integrations/nrgorilla@latest` |
| labstack/echo v4 | `integrations/nrecho-v4` | `go get github.com/newrelic/go-agent/v3/integrations/nrecho-v4@latest` |
| gin-gonic/gin | `integrations/nrgin` | `go get github.com/newrelic/go-agent/v3/integrations/nrgin@latest` |
| julienschmidt/httprouter | `integrations/nrhttprouter`（`nrhttprouter.New(nrApp)` で置き換える） | `go get github.com/newrelic/go-agent/v3/integrations/nrhttprouter@latest` |
| goji / その他 net/http 互換 | `newrelic.WrapHandleFunc`（本体に同梱） | 不要 |

### 2-1. 標準 net/http（http.ServeMux / http.HandleFunc）

```go
// before
mux.HandleFunc("/posts/", getPosts)
mux.Handle("/api/users", usersHandler)

// after（WrapHandleFunc は (pattern, handler) をそのまま返すので、引数の位置に包むだけ）
mux.HandleFunc(newrelic.WrapHandleFunc(nrApp, "/posts/", getPosts))
mux.Handle(newrelic.WrapHandle(nrApp, "/api/users", usersHandler))
```

- 名前は `r.Method + " " + pattern`（例: `GET /posts/`）
- Go 1.22 の `mux.HandleFunc("GET /posts/{id}", h)` 形式に包むと、名前が `GET GET /posts/{id}` になる見込み（ソースを読んだ推定、未確認）。集計はできるので当日はそのままでよい

### 2-2. go-chi/chi v5（実機で確認済み）

```go
// before
r.Get("/posts/{id}", getPostsID)
r.Post("/comment", postComment)

// after
r.Get(newrelic.WrapHandleFunc(nrApp, "/posts/{id}", getPostsID))
r.Post(newrelic.WrapHandleFunc(nrApp, "/comment", postComment))
```

- 名前は `GET /posts/{id}` のようにルートパターンになる
- 公式統合 `nrgochi.Middleware` は使わない。名前が `r.URL.Path` で付くため `/posts/123` が全部別名になる（nrgochi.go で確認）
- 機械的に置き換える sed の例（GNU sed 向け。置換後に `go build` で確認する）:

```bash
sed -i -E 's/\br\.(Get|Post|Put|Delete|Patch)\("([^"]+)", *([A-Za-z0-9_.]+)\)/r.\1(newrelic.WrapHandleFunc(nrApp, "\2", \3))/' app.go
gofmt -l . && go build ./...
```

### 2-3. gorilla/mux

```go
import "github.com/newrelic/go-agent/v3/integrations/nrgorilla"

r := mux.NewRouter()
r.Use(nrgorilla.Middleware(nrApp))   // ルート定義より前に 1 行足す
r.HandleFunc("/posts/{id}", getPostsID)
```

- 名前はルートテンプレート（`GET /posts/{id}`）。`.Name("xxx")` 付きのルートはその名前になる
- 404 / 405 は Middleware の対象外。必要なら `r.NotFoundHandler = ` に `newrelic.WrapHandle` で包んだものを入れる

### 2-4. labstack/echo v4

```go
import "github.com/newrelic/go-agent/v3/integrations/nrecho-v4"   // パッケージ名は nrecho

e := echo.New()
e.Use(nrecho.Middleware(nrApp))   // 他のミドルウェアより前に登録する
e.GET("/posts/:id", getPostsID)
```

- 名前は `c.Request().Method + " " + c.Path()`（例: `GET /posts/:id`）
- ハンドラ内でトランザクションを取るときは `nrecho.FromContext(c)`
- nrecho-v4 の go.mod は `go 1.25` を要求する（v3.45.0 時点）。サーバーの Go が古いときはビルドが通るか当日確認する

### 2-5. gin-gonic/gin

```go
import "github.com/newrelic/go-agent/v3/integrations/nrgin"

router := gin.Default()
router.Use(nrgin.Middleware(nrApp))   // 他のミドルウェアより前に登録する
router.GET("/posts/:id", getPostsID)
```

- 名前は `c.Request.Method + " " + c.FullPath()`（例: `GET /posts/:id`）
- ハンドラ内でトランザクションを取るときは `nrgin.Transaction(c)`

### 2-6. goji / その他 net/http 互換

ハンドラが `func(http.ResponseWriter, *http.Request)` なら、2-1 と同じく `newrelic.WrapHandleFunc(nrApp, "<パターン>", h)` で包む。
`WrapHandleFunc` の第 1 戻り値（pattern）が不要なルーターでは `_, h := newrelic.WrapHandleFunc(...)` で受けてハンドラだけ渡す。

## 3. DB の計装

### 3-1. database/sql + go-sql-driver/mysql

```go
// before
import _ "github.com/go-sql-driver/mysql"
db, err := sql.Open("mysql", dsn)

// after
import _ "github.com/newrelic/go-agent/v3/integrations/nrmysql"
db, err := sql.Open("nrmysql", dsn)
```

- `nrmysql` は内部で `github.com/go-sql-driver/mysql` を import するので、元の import 行は消してよい（残しても動く）

### 3-2. jmoiron/sqlx（実機で確認済み）

```go
import (
    "github.com/jmoiron/sqlx"
    _ "github.com/newrelic/go-agent/v3/integrations/nrmysql"
)

sqlx.BindDriver("nrmysql", sqlx.QUESTION)   // ? バインドを mysql と同じ扱いにする
db, err := sqlx.Open("nrmysql", dsn)
```

- `BindDriver` を先に呼ぶと `sqlx.In` / `Rebind` / `Named` が `?` 形式で動く（未登録のドライバ名は UNKNOWN 扱いになるため）

### 3-3. クエリを Context 版にする（SQL を出すために必須）

SQL は `*Context` 版のメソッドにトランザクション入りの context を渡したときだけ Datastore として記録される。
`Query` / `Exec` / `Get` / `Select`（Context なし）はドライバ側で計装されず、何も出ない。
`WrapHandleFunc` やルーター統合を通ったハンドラでは、`r.Context()` にトランザクションが入っている。

| before | after |
|---|---|
| `db.Query(q, ...)` | `db.QueryContext(r.Context(), q, ...)` |
| `db.QueryRow(q, ...)` | `db.QueryRowContext(r.Context(), q, ...)` |
| `db.Exec(q, ...)` | `db.ExecContext(r.Context(), q, ...)` |
| `db.Get(&v, q, ...)`（sqlx） | `db.GetContext(r.Context(), &v, q, ...)` |
| `db.Select(&vs, q, ...)`（sqlx） | `db.SelectContext(r.Context(), &vs, q, ...)` |
| `tx.Exec(q, ...)` | `tx.ExecContext(r.Context(), q, ...)` |

機械的な置換の手順（`r *http.Request` が見える関数向け）:

```bash
cd <webapp>/go
grep -nE '\b(db|tx)\.(Query|QueryRow|Exec|Get|Select)\(' *.go | wc -l    # 対象の数を把握する
sed -i -E 's/\b(db|tx)\.(Query|QueryRow|Exec|Get|Select)\(/\1.\2Context(r.Context(), /g' *.go
gofmt -w . && go build ./... 2>&1 | head -30
```

- `go build` のエラー（`undefined: r`）が出た関数は、ハンドラ以外から呼ばれている
- その関数には `ctx context.Context` を第 1 引数に足し、呼び出し側から `r.Context()` を渡す
- 関数内の `r.Context()` は `ctx` に書き換える（`sed -i 's/r.Context()/ctx/g'` を該当ファイルの該当関数だけに当てる）
- echo は `c.Request().Context()`、gin は `c.Request.Context()` を `r.Context()` の代わりに使う

### 3-4. PostgreSQL のとき

`jackc/pgx/v5` は `integrations/nrpgx5`（`cfg.Tracer = nrpgx5.NewTracer()`）、`lib/pq` は `integrations/nrpq`（`sql.Open("nrpostgres", dsn)`）を使う。

## 4. memcached と Redis

- memcached（`bradfitz/gomemcache`）には公式統合が無い。初手では計測しない。後で必要なら `newrelic.DatastoreSegment` を手で置く
- Redis（`redis/go-redis/v9`）は `integrations/nrredis-v9` がある。クライアント作成直後に 1 行足し、各呼び出しに `r.Context()` を渡す

```go
import nrredis "github.com/newrelic/go-agent/v3/integrations/nrredis-v9"

client := redis.NewClient(opts)
client.AddHook(nrredis.NewHook(opts))
```

## 5. 確認方法

1. ビルドして再起動し、アプリのログを見る（例: `journalctl -u isu-go -n 30 --no-pager`）
2. `application connected` と `Reporting to: https://...` の行が出れば接続済み（`NEW_RELIC_LOG=stderr` を環境変数に入れておく）
3. ベンチを 1 回回し、1〜2 分待つ
4. 手元のリポジトリで `NEW_RELIC_APP_NAME=<appName> .claude/skills/newrelic-nrql/nrql.sh --table endpoints 5` と `--table sql 5` を見る

| 症状 | 見るところ |
|---|---|
| `endpoints` が空 | ログに `application connected` が無い → キー・`NEW_RELIC_ENABLED`・appName を確認する |
| `endpoints` は出るが `sql` が空 | driver 名が `mysql` のまま、または Context なしのクエリ（3-3） |
| 名前が `/posts/123` のようにバラける | `nrgochi` や URL パスで名前を付ける方式になっている → 2. の方式に戻す |

## 6. 落とし穴（短く）

- `NEW_RELIC_APP_NAME` は全台で同じにする（1 アプリとして集計される）
- ライセンスキーはコードやリポジトリに書かず、環境変数（`/home/isucon/newrelic.env`）だけに置く
- `nrgochi` は使わない（名前が URL パスになる）
- 静的ファイルの `http.FileServer` / `http.StripPrefix` は包まない（ノイズになる）
- ルーター統合の Middleware は他のミドルウェアより前に登録する
- 計測が終わったら `NEW_RELIC_ENABLED=false` で止める。再起動試験の前に必ずやる

## 参照

- 統合一覧: https://github.com/newrelic/go-agent/tree/master/v3/integrations
- WrapHandle / WrapHandleFunc: https://github.com/newrelic/go-agent/blob/master/v3/newrelic/instrumentation.go
- ConfigFromEnvironment: https://github.com/newrelic/go-agent/blob/master/v3/newrelic/config_options.go
- nrgochi（採用しない根拠。`name := r.Method + " " + r.URL.Path`）: https://github.com/newrelic/go-agent/blob/master/v3/integrations/nrgochi/nrgochi.go
- nrgorilla: https://github.com/newrelic/go-agent/blob/master/v3/integrations/nrgorilla/nrgorilla.go 、example: https://github.com/newrelic/go-agent/blob/master/v3/integrations/nrgorilla/example/main.go
- nrecho-v4: https://github.com/newrelic/go-agent/blob/master/v3/integrations/nrecho-v4/nrecho.go
- nrgin: https://github.com/newrelic/go-agent/blob/master/v3/integrations/nrgin/nrgin.go
- nrmysql: https://github.com/newrelic/go-agent/blob/master/v3/integrations/nrmysql/nrmysql.go
- SQL ドライバ計装（Context 版だけ記録される根拠）: https://github.com/newrelic/go-agent/blob/master/v3/newrelic/sql_driver.go
- nrpgx5: https://github.com/newrelic/go-agent/blob/master/v3/integrations/nrpgx5/nrpgx5.go
- nrredis-v9: https://github.com/newrelic/go-agent/blob/master/v3/integrations/nrredis-v9/nrredis.go
- 公式ドキュメント（セグメント・Datastore）: https://docs.newrelic.com/docs/apm/agents/go-agent/instrumentation/instrument-go-segments/
- go-agent v3.45.0 リリース: https://github.com/newrelic/go-agent/releases/tag/v3.45.0
- 当日の環境変数・systemd 設定・確認手順: [docs/newrelic-agent-install.md](https://github.com/suecideTech/isucon2026-prep/blob/main/docs/newrelic-agent-install.md)（prep）
