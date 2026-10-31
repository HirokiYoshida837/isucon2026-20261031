package isureal

import (
	"bytes"
	"context"
	"fmt"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestPrefetchThenServe(t *testing.T) {
	srv := httptest.NewServer(signHandler("t"))
	defer srv.Close()
	oldPeers, oldToken := signPeers, signToken
	signPeers, signToken = []string{strings.TrimPrefix(srv.URL, "http://")}, "t"
	defer func() { signPeers, signToken = oldPeers, oldToken }()
	issuedImageURLs.reset()
	verifiedImageSigs.reset()

	key := bytes.Repeat([]byte{5}, 32)
	list := make([]sigReq, 0, 3*prefetchBatch/2)
	for i := 0; i < cap(list); i++ { // 複数バッチにまたがる件数
		list = append(list, sigReq{key: key, imageID: fmt.Sprintf("pf-%d", i)})
	}
	prefetchSigs(context.Background(), list)

	k := signedURLKey{key: string(key), imageID: "pf-7"}
	e, ok := issuedImageURLs.get(k)
	if !ok || !e.unverified {
		t.Fatalf("先読みの分が unverified で入っていない: ok=%v", ok)
	}
	if verifiedImageSigs.has(newVerifiedSigKey("pf-7", e.exp, e.sig[:])) {
		t.Fatal("配る前に検証済みへ入っている")
	}
	// 配ると、自前の計算なしで同じ URL が返り、検証済みになる
	if u := signedImageURL(User{ImageKey: key}, "pf-7"); u != e.url {
		t.Fatalf("url が違う: %s", u)
	}
	if !verifiedImageSigs.has(newVerifiedSigKey("pf-7", e.exp, e.sig[:])) {
		t.Fatal("配った後に検証済みになっていない")
	}
	if e2, _ := issuedImageURLs.get(k); e2.unverified {
		t.Fatal("unverified が残っている")
	}
	// 先読みの署名は自前の計算と一致する
	if want := imageSignature(key, "pf-7", e.exp); !bytes.Equal(want, e.sig[:]) {
		t.Fatal("署名が自前計算と違う")
	}
}
