//go:build darwin

package main

import (
	"fmt"
	"os"

	"antigravity2api/pkg/auth"
	"antigravity2api/pkg/config"
	"antigravity2api/pkg/server"
	"antigravity2api/pkg/upstream"
)

func main() {
	cfg, err := config.LoadConfig()
	if err != nil {
		fmt.Printf("加载配置错误: %v\n", err)
	}

	tokenManager := auth.NewTokenManager(cfg)
	upstreamClient := upstream.NewUpstreamClient(cfg, tokenManager)
	srv := server.NewServer(cfg, tokenManager, upstreamClient)

	// Start server in background goroutine
	go func() {
		if err := srv.Start(); err != nil {
			fmt.Printf("HTTP 服务退出: %v\n", err)
			os.Exit(1)
		}
	}()

	// Start Native Mac Desktop GUI Window (blocks on NSApp run)
	launchNativeGUI(cfg, tokenManager)
}
