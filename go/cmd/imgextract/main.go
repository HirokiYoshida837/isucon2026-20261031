package main

import (
	"context"
	"fmt"

	"example.com/isureal-company/isureal"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/jmoiron/sqlx"
)

// このリージョンのユーザーが持つ初期データの画像を /home/isucon/imgcache/base に書き出す（#120）。
// 既にあるファイルは飛ばすので、途中で止めても再実行でよい。ベンチ中は流さない（asia2 のディスクを読む）。
// 使い方: cd ~/webapp/go && go build -o imgextract ./cmd/imgextract && (set -a; . ~/env.sh; ./imgextract)
func main() {
	config := isureal.ConfigFromEnv()
	dbConfig, err := pgx.ParseConfig(config.DatabaseDSN())
	if err != nil {
		panic(err)
	}
	db := sqlx.NewDb(stdlib.OpenDB(*dbConfig), "pgx")
	n, err := isureal.ExtractImagesToDisk(context.Background(), db, config.Region)
	fmt.Println("images:", n)
	if err != nil {
		panic(err)
	}
}
