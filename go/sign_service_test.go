package isureal

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestSignHandler(t *testing.T) {
	key := bytes.Repeat([]byte{7}, 32)
	body, _ := json.Marshal(signRequest{
		Exp:   1793415639,
		Keys:  []string{base64.RawURLEncoding.EncodeToString(key)},
		Items: []signItem{{K: 0, ID: "a"}, {K: 0, ID: "b"}},
	})
	h := signHandler("tok")

	req := httptest.NewRequest(http.MethodPost, "/internal/sign", bytes.NewReader(body))
	req.Header.Set("X-Sign-Token", "tok")
	rec := httptest.NewRecorder()
	h(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d", rec.Code)
	}
	var res signResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &res); err != nil || len(res.Sigs) != 2 {
		t.Fatalf("bad response: %v %s", err, rec.Body.String())
	}
	for i, id := range []string{"a", "b"} {
		want := base64.RawURLEncoding.EncodeToString(imageSignature(key, id, 1793415639))
		if res.Sigs[i] != want {
			t.Errorf("sig %d mismatch", i)
		}
	}

	req = httptest.NewRequest(http.MethodPost, "/internal/sign", bytes.NewReader(body))
	req.Header.Set("X-Sign-Token", "wrong")
	rec = httptest.NewRecorder()
	h(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Errorf("wrong token: status %d", rec.Code)
	}
}
