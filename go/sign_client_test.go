package isureal

import (
	"bytes"
	"context"
	"fmt"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestPrewarmSigsFillsCache(t *testing.T) {
	srv := httptest.NewServer(signHandler("t"))
	defer srv.Close()
	oldPeers, oldToken := signPeers, signToken
	signPeers, signToken = []string{strings.TrimPrefix(srv.URL, "http://")}, "t"
	defer func() { signPeers, signToken = oldPeers, oldToken }()
	issuedImageURLs.reset()
	verifiedImageSigs.reset()

	key := bytes.Repeat([]byte{9}, 32)
	reqs := make([]sigReq, 0, 12)
	for i := 0; i < 12; i++ {
		reqs = append(reqs, sigReq{key: key, imageID: fmt.Sprintf("img-%d", i)})
	}
	prewarmSigs(context.Background(), reqs)

	for _, r := range reqs {
		e, ok := issuedImageURLs.get(signedURLKey{key: string(key), imageID: r.imageID})
		if !ok {
			t.Fatalf("%s: キャッシュに入っていない", r.imageID)
		}
		if got := buildSignedURL(r.imageID, e.exp, imageSignature(key, r.imageID, e.exp)); got != e.url {
			t.Errorf("%s: URL が自前計算と違う", r.imageID)
		}
	}
	// 署名を自前で計算せずに同じ URL が返る
	if u := signedImageURL(User{ImageKey: key}, "img-3"); !strings.HasPrefix(u, "/api/images/img-3?exp=") {
		t.Errorf("unexpected url %s", u)
	}
}

func TestPrewarmSigsFallsBackWhenPeerDown(t *testing.T) {
	oldPeers := signPeers
	oldPct := signLocalPercent
	signPeers, signLocalPercent = []string{"127.0.0.1:1"}, 0 // 誰も待ち受けていない。自前の割合は 0 にして peer の失敗だけを見る
	defer func() { signPeers, signLocalPercent = oldPeers, oldPct; signPausedUntil.Store(0); signFails.Store(0) }()
	issuedImageURLs.reset()
	key := bytes.Repeat([]byte{9}, 32)
	reqs := make([]sigReq, 0, 10)
	for i := 0; i < 10; i++ {
		reqs = append(reqs, sigReq{key: key, imageID: fmt.Sprintf("down-%d", i)})
	}
	prewarmSigs(context.Background(), reqs) // エラーにならず何も入れない
	if _, ok := issuedImageURLs.get(signedURLKey{key: string(key), imageID: "down-0"}); ok {
		t.Error("失敗したのにキャッシュに入っている")
	}
	if u := signedImageURL(User{ImageKey: key}, "down-0"); u == "" {
		t.Error("自前計算に戻れていない")
	}
}
