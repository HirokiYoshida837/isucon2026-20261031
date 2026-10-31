package isureal

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"runtime"
	"strings"
	"sync"
	"time"
)

// 画像の署名だけを計算する内部 API。asia1 の CPU を europe1 / america1 に逃がすために使う。
// private IP（192.168.x.x）だけで待ち受ける。公開の 8080 とは別の口。
// 認証用のトークンは DB パスワードから導く（4 台に同じ値があるので、新しい秘密を配らなくて済む）。
const signServicePort = "8081"
const signMaxItems = 20000

type signRequest struct {
	Exp   int64      `json:"exp"`
	Keys  []string   `json:"keys"` // 持ち主の鍵（base64url 32B）。items から添字で参照する
	Items []signItem `json:"items"`
}

type signItem struct {
	K  int    `json:"k"`
	ID string `json:"i"`
}

type signResponse struct {
	Sigs []string `json:"sigs"` // base64url 32B。items と同じ順
}

func signTokenFor(dbPassword string) string {
	sum := sha256.Sum256([]byte("sign:" + dbPassword))
	return hex.EncodeToString(sum[:])
}

func signHandler(token string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if subtle.ConstantTimeCompare([]byte(r.Header.Get("X-Sign-Token")), []byte(token)) != 1 {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		var req signRequest
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 32<<20)).Decode(&req); err != nil || len(req.Items) > signMaxItems {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		keys := make([][]byte, len(req.Keys))
		for i, k := range req.Keys {
			key, err := base64.RawURLEncoding.DecodeString(k)
			if err != nil || len(key) != sha256.Size {
				http.Error(w, "bad key", http.StatusBadRequest)
				return
			}
			keys[i] = key
		}
		for _, it := range req.Items {
			if it.K < 0 || it.K >= len(keys) {
				http.Error(w, "bad key index", http.StatusBadRequest)
				return
			}
		}
		sigs := make([]string, len(req.Items))
		workers := runtime.GOMAXPROCS(0)
		var wg sync.WaitGroup
		for n := 0; n < workers; n++ {
			wg.Add(1)
			go func(n int) {
				defer wg.Done()
				for i := n; i < len(sigs); i += workers {
					it := req.Items[i]
					sigs[i] = base64.RawURLEncoding.EncodeToString(imageSignature(keys[it.K], it.ID, req.Exp))
				}
			}(n)
		}
		wg.Wait()
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(signResponse{Sigs: sigs})
	}
}

// このサーバーの private IP（192.168.x.x）。見つからなければ空
func privateIP() string {
	addrs, _ := net.InterfaceAddrs()
	for _, a := range addrs {
		if ipnet, ok := a.(*net.IPNet); ok && ipnet.IP.To4() != nil && strings.HasPrefix(ipnet.IP.String(), "192.168.") {
			return ipnet.IP.String()
		}
	}
	return ""
}

func newSignServer(cfg *Config) *http.Server {
	ip := privateIP()
	if ip == "" {
		return nil
	}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /internal/sign", signHandler(signTokenFor(cfg.DBPassword)))
	return &http.Server{Addr: net.JoinHostPort(ip, signServicePort), Handler: mux, ReadHeaderTimeout: 5 * time.Second}
}

// アプリ本体の中で待ち受ける（europe1 / america1 / asia1）。開けなくても本体は止めない。
func startSignService(cfg *Config) {
	if srv := newSignServer(cfg); srv != nil {
		go func() { _ = srv.ListenAndServe() }()
	}
}

// 署名 API だけを動かす（asia2 用。DB も画面も持たない）。ISUREAL_SIGN_ONLY=1 のときに main から呼ぶ。
func RunSignOnly(cfg *Config) error {
	srv := newSignServer(cfg)
	if srv == nil {
		return errors.New("private IP (192.168.x.x) が見つからない")
	}
	return srv.ListenAndServe()
}
