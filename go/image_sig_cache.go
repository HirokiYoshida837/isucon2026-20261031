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
