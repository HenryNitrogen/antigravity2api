.PHONY: all mac server linux docker run clean

all: mac server linux

server:
	@mkdir -p bin
	go build -o bin/antigravity2api-server ./cmd/server
	@echo "✅ Server CLI 构建成功: bin/antigravity2api-server"

mac:
	@mkdir -p bin/Antigravity2Api.app/Contents/MacOS
	go build -o bin/antigravity2api-mac ./cmd/macapp
	@cp bin/antigravity2api-mac bin/Antigravity2Api.app/Contents/MacOS/Antigravity2Api
	@cp cmd/macapp/Info.plist bin/Antigravity2Api.app/Contents/Info.plist
	@echo "✅ Mac 桌面 GUI App 构建成功: bin/Antigravity2Api.app"

linux:
	@mkdir -p bin
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags="-s -w" -o bin/antigravity2api-linux-amd64 ./cmd/server
	CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -trimpath -ldflags="-s -w" -o bin/antigravity2api-linux-arm64 ./cmd/server
	@echo "✅ Linux 双架构构建成功: bin/antigravity2api-linux-amd64, bin/antigravity2api-linux-arm64"

docker:
	docker build -t antigravity2api:latest .
	@echo "✅ Docker 镜像构建成功: antigravity2api:latest"

run: server
	./bin/antigravity2api-server

clean:
	rm -rf bin/
