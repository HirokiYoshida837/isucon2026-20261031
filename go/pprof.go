package isureal

import (
	"log"
	"net/http"
	_ "net/http/pprof"
)

// 計測用の pprof。ローカルからだけ届く（公開 API のポートとは別）
func startPprof() {
	go func() {
		if err := http.ListenAndServe("127.0.0.1:6060", nil); err != nil {
			log.Printf("pprof disabled: %s", err.Error())
		}
	}()
}
