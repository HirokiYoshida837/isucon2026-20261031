package isureal

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"io"
	"log"
	"net"
	"net/http"
	"strconv"
	"time"
)

// europe1 / america1 が計算した署名を受け取る内部 API（asia1 だけで動く）。
// private IP（192.168.x.x）だけで待ち受ける。公開の 8080 とは別の口。
// 認証用のトークンは DB パスワードから導く（4 台に同じ値があるので、新しい秘密を配らなくて済む）。
const sigPushPort = "8081"
const sigPushMaxBody = 64 << 20

func signTokenFor(dbPassword string) string {
	sum := sha256.Sum256([]byte("sign:" + dbPassword))
	return hex.EncodeToString(sum[:])
}

func privateIPv4() string {
	addrs, _ := net.InterfaceAddrs()
	for _, a := range addrs {
		if ipn, ok := a.(*net.IPNet); ok {
			if ip := ipn.IP.To4(); ip != nil && ip[0] == 192 && ip[1] == 168 {
				return ip.String()
			}
		}
	}
	return ""
}

func startSigPushServer(dbPassword string) {
	ip := privateIPv4()
	if ip == "" {
		return
	}
	token := signTokenFor(dbPassword)
	auth := func(h http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			if subtle.ConstantTimeCompare([]byte(r.Header.Get("X-Sign-Token")), []byte(token)) != 1 {
				http.Error(w, "forbidden", http.StatusForbidden)
				return
			}
			h(w, r)
		}
	}
	mux := http.NewServeMux()
	// 持っているバケットと件数。送る側が再送の要否を見る
	mux.HandleFunc("GET /internal/sigs/status", auth(func(w http.ResponseWriter, r *http.Request) {
		if !sigBucketSecondsMatch(r.URL.Query().Get("seconds")) { // バケット長が違う署名は使えない
			http.Error(w, "bucket seconds mismatch", http.StatusConflict)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(pushedSigs.counts())
	}))
	mux.HandleFunc("POST /internal/sigs", auth(func(w http.ResponseWriter, r *http.Request) {
		bucket, err := strconv.ParseInt(r.URL.Query().Get("bucket"), 10, 64)
		cur := sigBucketOf(time.Now())
		if err != nil || bucket < cur || bucket > cur+2 || !sigBucketSecondsMatch(r.URL.Query().Get("seconds")) {
			http.Error(w, "bad bucket", http.StatusBadRequest)
			return
		}
		data, err := io.ReadAll(http.MaxBytesReader(w, r.Body, sigPushMaxBody))
		if err != nil {
			http.Error(w, "bad body", http.StatusBadRequest)
			return
		}
		ids, sigs, err := decodeSigRows(data)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		pushedSigs.put(bucket, ids, sigs)
		pushedSigs.dropBefore(cur)
		w.WriteHeader(http.StatusNoContent)
	}))
	srv := &http.Server{Addr: ip + ":" + sigPushPort, Handler: mux, ReadTimeout: 120 * time.Second}
	go func() {
		if err := srv.ListenAndServe(); err != nil {
			log.Printf("sig push server stopped: %v", err)
		}
	}()
}
