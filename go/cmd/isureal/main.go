package main

import (
	"os"

	"example.com/isureal-company/isureal"
)

// 2026-09-23

func main() {
	config := isureal.ConfigFromEnv()
	if os.Getenv("ISUREAL_SIGN_ONLY") == "1" {
		// asia2 で署名の計算だけを受け持つ（infra/isureal-sign.service）
		panic("sign service stopped: " + isureal.RunSignOnly(config).Error())
	}
	server := isureal.NewServer(config)
	if err := server.Start(":8080"); err != nil {
		panic("failed to start server: " + err.Error())
	}
}
