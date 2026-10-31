package isureal

import "sync"

// login_sessions の読み取りキャッシュ。正本は DB で、書き込みは今まで通り同期。
// byToken: token_hash -> セッション、byUser: user_id -> token_hash（1 ユーザー 1 有効セッション）。
// gen は無効化のたびに進め、ミス時の DB 読み取り結果が無効化後に載るのを防ぐ。
type loginSessionCache struct {
	mu      sync.RWMutex
	byToken map[string]Session
	byUser  map[string]string
	gen     uint64
}

var sessionCache = &loginSessionCache{byToken: map[string]Session{}, byUser: map[string]string{}}

func (c *loginSessionCache) get(tokenHash string) (Session, bool, uint64) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	s, ok := c.byToken[tokenHash]
	return s, ok, c.gen
}

// put はミス時の DB 読み取り用。get で受け取った gen が変わっていない（間に無効化が無い）ときだけ載せる。
func (c *loginSessionCache) put(s Session, gen uint64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.gen != gen {
		return
	}
	c.putLocked(s)
}

// putLogin はログイン commit 後に呼ぶ。同じユーザーの旧トークンを消して置き換える。
func (c *loginSessionCache) putLogin(s Session) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.putLocked(s)
}

func (c *loginSessionCache) putLocked(s Session) {
	if oldHash, ok := c.byUser[s.UserID]; ok && oldHash != s.TokenHash {
		if old, ok := c.byToken[oldHash]; ok && old.CreatedAt.After(s.CreatedAt) {
			return
		}
		delete(c.byToken, oldHash)
	}
	c.byToken[s.TokenHash] = s
	c.byUser[s.UserID] = s.TokenHash
}

// remove はログアウト用。現在のトークンと一致するときだけ消す。
func (c *loginSessionCache) remove(userID, tokenHash string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.gen++
	delete(c.byToken, tokenHash)
	if c.byUser[userID] == tokenHash {
		delete(c.byUser, userID)
	}
}

// removeUser は退会用。
func (c *loginSessionCache) removeUser(userID string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.gen++
	if h, ok := c.byUser[userID]; ok {
		delete(c.byToken, h)
		delete(c.byUser, userID)
	}
}

func (c *loginSessionCache) reset() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.gen++
	c.byToken = map[string]Session{}
	c.byUser = map[string]string{}
}
