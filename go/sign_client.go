package isureal

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// 署名を europe1 / america1 に計算してもらうクライアント（asia1 だけが使う）。
// 一覧のレスポンスを作る前に、キャッシュにない署名をまとめて頼み、結果をキャッシュに入れる。
// 頼めない・遅いときは何もしない（あとの signedImageURL が自前で計算する）。
const (
	signOffloadMin   = 8                      // これ未満は往復（約 100ms）のほうが高いので自前で計算する
	signTimeout      = 300 * time.Millisecond // 超えたら諦めて自前で計算する
	signBreakerFails = 3                      // 続けて失敗したら
	signBreakerPause = 5 * time.Second        // この間は頼まない
)

type sigReq struct {
	key     []byte
	imageID string
}

var (
	signPeers       []string
	signToken       string
	signHTTP        = &http.Client{Transport: &http.Transport{MaxIdleConns: 16, MaxIdleConnsPerHost: 8, IdleConnTimeout: 0}}
	signFails       atomic.Int32
	signPausedUntil atomic.Int64

	// 頼まずに asia1 自身で計算する割合（%）。残りは europe1 / america1 に等分（既定は asia 10 / europe 45 / america 45）。
	// ベンチ中は asia1 の CPU が足りないので少なめにする。env で変えられる。
	signLocalPercent = 10
)

func envPercent(name string, def int) int {
	if v, err := strconv.Atoi(envOrDefault(name, "")); err == nil && v >= 0 && v <= 100 {
		return v
	}
	return def
}

func initSignClient(cfg *Config) {
	signToken = signTokenFor(cfg.DBPassword)
	signLocalPercent = envPercent("ISUREAL_SIGN_LOCAL_PERCENT", signLocalPercent)
	if cfg.Region != "asia" {
		return
	}
	for _, p := range strings.Split(envOrDefault("ISUREAL_SIGN_PEERS", "192.168.2.10:8081,192.168.3.10:8081"), ",") {
		if p = strings.TrimSpace(p); p != "" {
			signPeers = append(signPeers, p)
		}
	}
}

func signAvailable() bool {
	return len(signPeers) > 0 && time.Now().UnixNano() >= signPausedUntil.Load()
}

func noteSignFailure() {
	if signFails.Add(1) >= signBreakerFails {
		signPausedUntil.Store(time.Now().Add(signBreakerPause).UnixNano())
		signFails.Store(0)
	}
}

// 画像と、持ち主のプロフィール画像（レスポンスに一緒に出る）の署名依頼
func imageSigReqs(imageID string, user User) []sigReq {
	reqs := []sigReq{{key: user.ImageKey, imageID: imageID}}
	if user.ProfileImageID != nil {
		reqs = append(reqs, sigReq{key: user.ImageKey, imageID: *user.ProfileImageID})
	}
	return reqs
}

func remoteSign(ctx context.Context, peer string, exp int64, reqs []sigReq) ([][]byte, error) {
	body := signRequest{Exp: exp, Items: make([]signItem, len(reqs))}
	keyIdx := map[string]int{}
	for i, r := range reqs {
		idx, ok := keyIdx[string(r.key)]
		if !ok {
			idx = len(body.Keys)
			keyIdx[string(r.key)] = idx
			body.Keys = append(body.Keys, base64.RawURLEncoding.EncodeToString(r.key))
		}
		body.Items[i] = signItem{K: idx, ID: r.imageID}
	}
	payload, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "http://"+peer+"/internal/sign", bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	req.Header.Set("X-Sign-Token", signToken)
	resp, err := signHTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, resp.Body)
		return nil, fmt.Errorf("sign: status %d", resp.StatusCode)
	}
	var out signResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil || len(out.Sigs) != len(reqs) {
		return nil, fmt.Errorf("sign: bad response")
	}
	sigs := make([][]byte, len(out.Sigs))
	for i, s := range out.Sigs {
		sig, err := base64.RawURLEncoding.DecodeString(s)
		if err != nil || len(sig) != sha256.Size {
			return nil, fmt.Errorf("sign: bad sig")
		}
		sigs[i] = sig
	}
	return sigs, nil
}

// 依頼のうち localPercent % は asia1 自身で計算し（往復が乗らない）、残りを peer に等分して並列に投げる。
// 返ってきた分を onSig に渡す。失敗した peer の分は返らない。
func signAcross(ctx context.Context, exp int64, reqs []sigReq, localPercent int, onSig func(sigReq, []byte)) {
	nLocal := len(reqs) * localPercent / 100
	local, remote := reqs[:nLocal], reqs[nLocal:]
	var wg sync.WaitGroup
	if len(local) > 0 {
		workers := runtime.GOMAXPROCS(0)
		for n := 0; n < workers; n++ {
			wg.Add(1)
			go func(n int) {
				defer wg.Done()
				for i := n; i < len(local); i += workers {
					onSig(local[i], imageSignature(local[i].key, local[i].imageID, exp))
				}
			}(n)
		}
	}
	n := len(signPeers)
	for i, peer := range signPeers {
		part := remote[i*len(remote)/n : (i+1)*len(remote)/n]
		if len(part) == 0 {
			continue
		}
		wg.Add(1)
		go func(peer string, part []sigReq) {
			defer wg.Done()
			sigs, err := remoteSign(ctx, peer, exp, part)
			if err != nil {
				noteSignFailure()
				return
			}
			signFails.Store(0)
			for j, sig := range sigs {
				onSig(part[j], sig)
			}
		}(peer, part)
	}
	wg.Wait()
}

// キャッシュにない署名をまとめて頼み、結果を発行済みとして入れる。
func prewarmSigs(ctx context.Context, reqs []sigReq) {
	if !signAvailable() {
		return
	}
	now := time.Now()
	exp := now.Add(imageURLLifetime).Unix()
	reuseFrom := now.Add(imageURLLifetime - signedURLReuse).Unix()
	seen := make(map[signedURLKey]struct{}, len(reqs))
	miss := make([]sigReq, 0, len(reqs))
	for _, r := range reqs {
		if len(r.key) != sha256.Size {
			continue
		}
		k := signedURLKey{key: string(r.key), imageID: r.imageID}
		if _, dup := seen[k]; dup {
			continue
		}
		seen[k] = struct{}{}
		if e, ok := issuedImageURLs.get(k); ok && e.exp >= reuseFrom {
			continue
		}
		miss = append(miss, r)
	}
	if len(miss) < signOffloadMin {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, signTimeout)
	defer cancel()
	signAcross(ctx, exp, miss, signLocalPercent, func(r sigReq, sig []byte) { storeIssued(r.key, r.imageID, exp, sig) })
}
