# 🪐 Antigravity2Api (Go Edition)

针对 Google Cloud Code / Antigravity 内部接口的高性能、超轻量 **OpenAI 兼容协议反向代理网关**。  
基于 Go 语言原生实现，零第三方重量级依赖，支持 Native macOS 桌面应用和无头服务器 Docker 容器化部署。

---

## ✨ 核心特性

- 🚀 **标准 OpenAI API 格式**：支持 `/v1/chat/completions` 和 `/v1/models`，无缝接入 NextChat、Cherry Studio、Chatbox、LobeChat、Cursor 等所有主流客户端。
- ⚡️ **全流式打字输出 (SSE)**：高响应度 Server-Sent Events，打字机实时吐字流畅。
- 🧠 **思维链支持 (Thinking Process)**：支持标准 `reasoning_content` 以及 `<think>` 思维标签输出。
- 🍏 **原生 macOS 桌面 GUI**：专为 Apple Silicon (M系列芯片) 打造的 AppKit 原生窗口应用，Dock 栏正常显示，支持一键复制 Refresh Token、Docker Compose 配置以及在桌面窗口实时测试。
- 🐳 **无头 Docker 友好**：支持环境变量注入 `REFRESH_TOKEN`，无需在服务器端繁琐配置浏览器授权。
- 📦 **超低资源占用**：多阶段构建极简镜像，单二进制仅约 9MB，运行时内存占用仅约 10MB~15MB。
- 🔄 **自动静默续期**：后台根据 Token 剩余有效期自动续期 Access Token，长久稳定运行无需重复登录。

---

## 🛠 架构设计与项目结构

```text
antigravity2api/
├── cmd/
│   ├── macapp/              # macOS 原生 AppKit GUI 应用程序入口
│   │   ├── gui.h / gui.m    # 原生 Objective-C / Cocoa 界面
│   │   ├── gui_darwin.go    # CGO 桥接层
│   │   └── main.go          # Mac 端入口
│   └── server/              # 独立无头服务器 CLI 入口 (用于 Docker / Linux)
│       └── main.go
├── pkg/
│   ├── auth/                # Google OAuth 授权、自动续期与令牌管理
│   ├── config/              # 配置解析与环境变量支持
│   ├── openai/              # OpenAI 格式请求/响应模型与转换
│   ├── server/              # HTTP 路由与反代服务器逻辑
│   └── upstream/            # 针对 Google 内部 RPC 协议的客户端实现
├── Dockerfile               # Alpine 多阶段快速构建配置
├── docker-compose.yml       # Docker Compose 一键部署模板
├── Makefile                 # 构建工具脚本
└── README.md
```

---

## 🚀 部署指南

### 1. 本地 macOS 桌面端使用
桌面端可作为独立网关使用，同时也是获取 `REFRESH_TOKEN` 的最便捷工具：
1. 编译或运行 `bin/Antigravity2Api.app`。
2. 首次打开点击 **🔑 重新登录授权**，通过浏览器完成 Google 账号登录。
3. 授权成功后，点击 **📋 复制 Refresh Token (供 Docker)** 即可将 Token 复制到剪贴板。

### 2. 服务器 Docker 一键部署

在服务器上创建 `docker-compose.yml`：

```yaml
services:
  antigravity2api:
    build: .
    container_name: antigravity2api
    restart: always
    ports:
      - "8088:8088"
    environment:
      - PORT=8088
      - HOST=0.0.0.0
      # 粘贴从 Mac 桌面端复制的 Refresh Token
      - REFRESH_TOKEN=在这里粘贴你的_REFRESH_TOKEN
      - PROJECT_ID=aicode-consumers
      - DEFAULT_MODEL=gemini-3.8-flash
      # 可选：API 访问密码 (客户端请求带 Bearer <KEY>)
      - API_KEY=your-custom-password
```

运行服务：
```bash
docker compose up -d --build
```

---

## 🔌 客户端接入配置

在 Cherry Studio / NextChat / Chatbox 等客户端中添加自定义 OpenAI 提供商：

- **API Base URL**: `http://你的服务器IP:8088/v1`
- **API Key**: 填入你在 `API_KEY` 中设置的密码
- **支持的模型**:
  - `gemini-3.8-flash-tiered` (推荐，速度极快)
  - `gemini-3.1-pro-low`
  - `claude-sonnet-4-6`

---

## 📄 开源许可证

本项目基于 [MIT License](LICENSE) 开源。
