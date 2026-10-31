package isureal

import (
	"bytes"
	"context"
	"fmt"
	"net/http/httptest"
	"strings"
	"testing"
)

func testPrefetchList(n int) []sigReq {
	key := bytes.Repeat([]byte{5}, 32)
	list := make([]sigReq, n)
	for i := range list {
		list[i] = sigReq{key: key, imageID: fmt.Sprintf("pf-%d", i)}
	}
	return list
}

func TestPrefetchSplitsAndStores(t *testing.T) {
	srv := httptest.NewServer(signHandler("t"))
	defer srv.Close()
	oldPeers, oldToken := signPeers, signToken
	signPeers, signToken = []string{strings.TrimPrefix(srv.URL, "http://")}, "t"
	defer func() { signPeers, signToken = oldPeers, oldToken }()
	issuedImageURLs.reset()
	verifiedImageSigs.reset()

	list := testPrefetchList(prefetchBatch + 500) // 2 バッチにまたがる
	prefetchSigs(context.Background(), list, 75, 1, false)

	for _, r := range list {
		e, ok := issuedImageURLs.get(signedURLKey{key: string(r.key), imageID: r.imageID})
		if !ok {
			t.Fatalf("%s: キャッシュに入っていない", r.imageID)
		}
		if want := buildSignedURL(r.imageID, e.exp, imageSignature(r.key, r.imageID, e.exp)); want != e.url {
			t.Fatalf("%s: URL が自前計算と違う", r.imageID)
		}
		if !verifiedImageSigs.has(newVerifiedSigKey(r.imageID, e.exp, imageSignature(r.key, r.imageID, e.exp))) {
			t.Fatalf("%s: 検証済みに入っていない", r.imageID)
		}
	}
}

func TestPrefetchListIsBounded(t *testing.T) {
	old := prefetchMax
	prefetchMax = 100
	defer func() { prefetchMax = old; setPrefetchList(nil) }()
	setPrefetchList(testPrefetchList(1000))
	if len(prefetchList) != 100 {
		t.Fatalf("prefetchList = %d 件", len(prefetchList))
	}
}

func TestStartPrefetchSkipsWithoutPeers(t *testing.T) {
	oldPeers := signPeers
	signPeers = nil // 頼めない
	defer func() { signPeers = oldPeers; setPrefetchList(nil) }()
	issuedImageURLs.reset()
	setPrefetchList(testPrefetchList(50))
	startPrefetch() // asia1 だけで抱えないので、何も始めない
	if prefetchCancel != nil {
		t.Fatal("peer が無いのに先読みを始めた")
	}
}
