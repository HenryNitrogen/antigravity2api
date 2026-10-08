package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"antigravity2api/pkg/auth"
	"antigravity2api/pkg/config"
	"antigravity2api/pkg/server"
	"antigravity2api/pkg/upstream"
)

func main() {
	loginCmd := flag.Bool("login", false, "启动命令行 Google OAuth 授权流程")
	statusCmd := flag.Bool("status", false, "查看当前配置与登录状态")
	portFlag := flag.Int("port", 0, "覆盖监听端口 (如 8080)")
	proxyFlag := flag.String("proxy", "", "覆盖代理地址 (如 http://127.0.0.1:7890)")
	flag.Parse()

	cfg, err := config.LoadConfig()
	if err != nil {
		fmt.Printf("加载配置错误: %v\n", err)
	}

	if *portFlag > 0 {
		cfg.Port = *portFlag
	}
	if *proxyFlag != "" {
		cfg.ProxyURL = *proxyFlag
	}

	tokenManager := auth.NewTokenManager(cfg)
	upstreamClient := upstream.NewUpstreamClient(cfg, tokenManager)

	// Status command
	if *statusCmd {
		fmt.Println("=== Antigravity2Api 状态 ===")
		fmt.Printf("配置文件路径: %s\n", config.GetConfigPath())
		fmt.Printf("服务端口: %d\n", cfg.Port)
		fmt.Printf("已登录: %v\n", tokenManager.IsAuthenticated())
		fmt.Printf("Google 邮箱: %s\n", cfg.UserEmail)
		fmt.Printf("项目 ID: %s\n", cfg.ProjectID)
		fmt.Printf("上游代理: %s\n", cfg.ProxyURL)
		fmt.Printf("API Key: %s\n", cfg.APIKey)
		return
	}

	// Login command
	if *loginCmd {
		fmt.Println("=== 启动 Google OAuth 登录流程 ===")
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
		defer cancel()

		err := tokenManager.StartOAuthServer(ctx, func(authURL string) {
			fmt.Println("\n👉 请在浏览器中打开以下链接完成 Google 账号授权：")
			fmt.Println("--------------------------------------------------")
			fmt.Println(authURL)
			fmt.Println("--------------------------------------------------")
			fmt.Println("等待授权回调中...")
		})

		if err != nil {
			fmt.Printf("❌ 授权失败: %v\n", err)
			os.Exit(1)
		}

		fmt.Println("🎉 授权成功！配置已保存，你可以直接启动服务了。")
		return
	}

	// Default: Run Server
	srv := server.NewServer(cfg, tokenManager, upstreamClient)

	// Graceful shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	go func() {
		<-sigChan
		fmt.Println("\n正在关闭 Antigravity2Api 服务...")
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(ctx)
		os.Exit(0)
	}()

	if !tokenManager.IsAuthenticated() {
		fmt.Println("⚠️  提示：当前尚未绑定 Google 账号。")
		fmt.Printf("👉 你可以在浏览器打开 http://localhost:%d/ 点击「Google 登录授权」一键绑定，\n", cfg.Port)
		fmt.Println("   或者运行命令 `antigravity2api -login` 进行终端授权。")
	}

	if err := srv.Start(); err != nil {
		fmt.Printf("服务启动失败: %v\n", err)
		os.Exit(1)
	}
}
