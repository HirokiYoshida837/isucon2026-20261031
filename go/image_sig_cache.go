package isureal

import "sync"

// 検証に成功した (image_id, exp, sig) を覚えて HMAC の再計算を省く。
// 署名は (持ち主の鍵, image_id, exp) だけで決まり、image_objects の持ち主は変わらない。
// exp の期限判定と生存確認は呼び出し側で毎回行う。成功したものだけ入れる。
// 上限に達したら全消去ではなく 1 世代前（prev）に回し、直近 verifiedSigMaxEntries 件は必ず残す（#160。
// 全消去だとウォームアップの 9 万件 + 走行中の発行で 10 万件に達した瞬間に全部消え、以後の GET /api/images が全部 HMAC をやり直していた）。
// 1 件約 100B なので 2 世代で最大約 100MB
const verifiedSigMaxEntries = 500000

type verifiedSigKey struct {
	imageID string
	exp     int64
	sig     [32]byte
}

type verifiedSigCache struct {
	mu    sync.Mutex
	items map[verifiedSigKey]struct{} // 現在の世代
	prev  map[verifiedSigKey]struct{} // 1 世代前（nil のことがある。nil map の読み出しは安全）
}

var verifiedImageSigs = &verifiedSigCache{items: map[verifiedSigKey]struct{}{}}

func newVerifiedSigKey(imageID string, exp int64, sig []byte) verifiedSigKey {
	k := verifiedSigKey{imageID: imageID, exp: exp}
	copy(k.sig[:], sig)
	return k
}

func (c *verifiedSigCache) has(k verifiedSigKey) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, ok := c.items[k]; ok {
		return true
	}
	_, ok := c.prev[k]
	return ok
}

func (c *verifiedSigCache) add(k verifiedSigKey) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.items) >= verifiedSigMaxEntries {
		c.prev = c.items
		c.items = make(map[verifiedSigKey]struct{}, verifiedSigMaxEntries/4)
	}
	c.items[k] = struct{}{}
}

func (c *verifiedSigCache) reset() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.items = map[verifiedSigKey]struct{}{}
	c.prev = nil
}

// 検証に失敗した (image_id, exp, sig) も覚える。署名は (持ち主の鍵, image_id, exp) だけで決まり鍵は変わらないので、
// 一度失敗した組は永久に失敗する。ベンチは同じ不正 URL を 1 IP あたり約 2,800 回叩く（asia1 の画像リクエストの 80% が
// この 403）ので、2 回目以降は DB の生存確認と HMAC 1000 回を飛ばして 403 を返す。guard の検知（abuse フラグ）はそのまま。
var rejectedImageSigs = &verifiedSigCache{items: map[verifiedSigKey]struct{}{}}

// 発行した署名付き URL のキャッシュ。(鍵, image_id) ごとに最後に発行した URL と exp を持ち、signedURLReuse の間は使い回す。
// 上限に達したら 1 世代前（prev）に回し、直近 signedURLMaxEntries 件は必ず残す（#160。全消去だと発行側の HMAC もやり直しになる）
const signedURLMaxEntries = 200000

type signedURLKey struct {
	key     string
	imageID string
}

type signedURLEntry struct {
	url  string
	exp  int64
	warm bool // 起動時のウォームアップで入れた分
}

type signedURLCache struct {
	mu    sync.Mutex
	items map[signedURLKey]signedURLEntry // 現在の世代
	prev  map[signedURLKey]signedURLEntry // 1 世代前（nil のことがある）
}

var issuedImageURLs = &signedURLCache{items: map[signedURLKey]signedURLEntry{}}

// 現在の世代を先に見る（同じ鍵が両方にあれば新しいほう）
func (c *signedURLCache) get(k signedURLKey) (signedURLEntry, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if e, ok := c.items[k]; ok {
		return e, true
	}
	e, ok := c.prev[k]
	return e, ok
}

func (c *signedURLCache) put(k signedURLKey, e signedURLEntry) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.items) >= signedURLMaxEntries {
		c.prev = c.items
		c.items = make(map[signedURLKey]signedURLEntry, signedURLMaxEntries/4)
	}
	c.items[k] = e
}

func (c *signedURLCache) reset() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.items = map[signedURLKey]signedURLEntry{}
	c.prev = nil
}

// ウォームアップで入れた分だけ残す。前のベンチ走行中に作った分は捨てて、再起動直後と同じ状態から測る。
func (c *signedURLCache) dropCold() {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, m := range []map[signedURLKey]signedURLEntry{c.items, c.prev} {
		for k, e := range m {
			if !e.warm {
				delete(m, k)
			}
		}
	}
}
