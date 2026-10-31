package isureal

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/jmoiron/sqlx"
)

// 画像本体を app サーバーのローカルディスクにも置く（#120）。中身は変わらないので、返してよいかは今どおり毎回 DB で見る。
// base/ は初期データ（cmd/imgextract で事前に書き出す。初期化でも消さない）、run/ は走行中に書いた分（初期化で消す）。
// 無ければ DB に落ちる。media_type は初期データ・INSERT とも image/jpeg だけなので、image/jpeg だけを置く。
const imageDiskDir = "/home/isucon/imgcache"

func imageDiskPath(dir, id string) string {
	if len(id) < 2 {
		return filepath.Join(imageDiskDir, dir, "_", id)
	}
	return filepath.Join(imageDiskDir, dir, id[:2], id)
}

func imageDiskGet(id string) ([]byte, bool) {
	if filepath.Base(id) != id {
		return nil, false
	}
	for _, dir := range []string{"run", "base"} {
		if data, err := os.ReadFile(imageDiskPath(dir, id)); err == nil {
			return data, true
		}
	}
	return nil, false
}

func imageDiskPut(dir, id, mediaType string, data []byte) {
	if mediaType != "image/jpeg" || filepath.Base(id) != id {
		return
	}
	path := imageDiskPath(dir, id)
	if _, err := os.Stat(path); err == nil {
		return
	}
	if dir == "run" {
		if _, err := os.Stat(imageDiskPath("base", id)); err == nil {
			return
		}
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return
	}
	// 書きかけを読まないよう、一時ファイルに書いてから rename する
	tmp, err := os.CreateTemp(filepath.Dir(path), ".tmp-")
	if err != nil {
		return
	}
	_, err = tmp.Write(data)
	if cerr := tmp.Close(); err == nil {
		err = cerr
	}
	if err != nil || os.Rename(tmp.Name(), path) != nil {
		os.Remove(tmp.Name())
	}
}

// 走行中に書いた分を捨てる。rename だけ同期で行い、削除は裏で行う（初期化の時間を使わない）
func imageDiskResetRun() {
	run := filepath.Join(imageDiskDir, "run")
	old := filepath.Join(imageDiskDir, "run-old-"+strconv.FormatInt(time.Now().UnixNano(), 10))
	if os.Rename(run, old) == nil {
		go os.RemoveAll(old)
	}
}

// ExtractImagesToDisk は、このリージョンのユーザーが持つ初期データの画像を base/ に書き出す（cmd/imgextract）
func ExtractImagesToDisk(ctx context.Context, db *sqlx.DB, region string) (int, error) {
	rows, err := db.QueryxContext(ctx, `
   SELECT o.id, o.media_type, o.data
   FROM image_objects o JOIN initial_users u ON u.id = o.owner_user_id
   WHERE u.region = $1 AND o.created_at <= (SELECT created_at FROM initial_data_boundary)`, region)
	if err != nil {
		return 0, err
	}
	defer rows.Close()
	n := 0
	for rows.Next() {
		var id, mediaType string
		var data []byte
		if err := rows.Scan(&id, &mediaType, &data); err != nil {
			return n, err
		}
		imageDiskPut("base", id, mediaType, data)
		n++
	}
	return n, rows.Err()
}
