package isureal

import "sync"

// 検証に成功した (image_id, exp, sig) を覚えて HMAC の再計算を省く。
// 署名は (持ち主の鍵, image_id, exp) だけで決まり、image_objects の持ち主は変わらない。
// exp の期限判定と生存確認は呼び出し側で毎回行う。成功したものだけ入れる。
const verifiedSigMaxEntries = 100000

type verifiedSigKey struct {
	imageID string
	exp     int64
	sig     [32]byte
}

type verifiedSigCache struct {
	mu    sync.Mutex
	items map[verifiedSigKey]struct{}
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
	_, ok := c.items[k]
	return ok
}

func (c *verifiedSigCache) add(k verifiedSigKey) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.items) >= verifiedSigMaxEntries {
		c.items = map[verifiedSigKey]struct{}{}
	}
	c.items[k] = struct{}{}
}

func (c *verifiedSigCache) reset() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.items = map[verifiedSigKey]struct{}{}
}

// 発行した署名付き URL のキャッシュ。(鍵, image_id) ごとに最後に発行した URL と exp を持ち、signedURLReuse の間は使い回す。
// 上限を超えたら全消去する。
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
	items map[signedURLKey]signedURLEntry
}

var issuedImageURLs = &signedURLCache{items: map[signedURLKey]signedURLEntry{}}

func (c *signedURLCache) get(k signedURLKey) (signedURLEntry, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.items[k]
	return e, ok
}

func (c *signedURLCache) put(k signedURLKey, e signedURLEntry) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.items) >= signedURLMaxEntries {
		c.items = map[signedURLKey]signedURLEntry{}
	}
	c.items[k] = e
}

func (c *signedURLCache) reset() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.items = map[signedURLKey]signedURLEntry{}
}

// ウォームアップで入れた分だけ残す。前のベンチ走行中に作った分は捨てて、再起動直後と同じ状態から測る。
func (c *signedURLCache) dropCold() {
	c.mu.Lock()
	defer c.mu.Unlock()
	for k, e := range c.items {
		if !e.warm {
			delete(c.items, k)
		}
	}
}
