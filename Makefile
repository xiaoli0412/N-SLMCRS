# N-SLMCRS Gateway Makefile（v0.9.0）
#
# 常用：
#   make build        # 构建前端 + Go 二进制到 bin/gateway
#   make test         # go test ./...
#   make vet          # go vet ./...
#   make publish TAG=v0.9.0   # 本地构建双 Docker 镜像并推 ghcr（latest + TAG）
#   make deploy TAG=v0.9.0    # 发布后部署到服务器（= scripts/deploy.sh）
#
# Windows 下需 Git Bash / WSL；目标按 sh 语法书写。

VERSION ?= v0.9.0
GATEWAY_IMG ?= ghcr.io/xiaoli0412/n-slmcrs-gateway
KERNEL_IMG   ?= ghcr.io/xiaoli0412/n-slmcrs-kernel
GO           ?= go

.PHONY: build-web build-go build test vet fmt clean docker-build-gateway docker-build-kernel publish deploy run

# ─── 本地构建 ──────────────────────────────────────────────
build-web:
	cd web && npm ci --no-audit --no-fund && npm run build

build-go:
	$(GO) build -trimpath -ldflags="-s -w -X main.version=$(VERSION)" -o bin/gateway ./cmd/gateway

# build = 前端 + Go（embed 进二进制）
build: build-web build-go

run:
	$(GO) run ./cmd/gateway

# ─── 检查 ──────────────────────────────────────────────────
vet:
	$(GO) vet ./...

test:
	$(GO) test ./...

fmt:
	$(GO) fmt ./...

# ─── Docker 镜像（ghcr）────────────────────────────────────
# 双标签：latest + VERSION
docker-build-gateway:
	docker build -f Dockerfile -t $(GATEWAY_IMG):latest -t $(GATEWAY_IMG):$(VERSION) .

docker-build-kernel:
	docker build -f Dockerfile.kernel -t $(KERNEL_IMG):latest -t $(KERNEL_IMG):$(VERSION) .

# publish = 构建双镜像 + 推送（需先 docker login ghcr.io）
publish: docker-build-gateway docker-build-kernel
	docker push $(GATEWAY_IMG):latest
	docker push $(GATEWAY_IMG):$(VERSION)
	docker push $(KERNEL_IMG):latest
	docker push $(KERNEL_IMG):$(VERSION)
	@echo "✓ 已推送 $(GATEWAY_IMG):$(VERSION) 与 $(KERNEL_IMG):$(VERSION)"

# deploy = 发布镜像后部署到服务器
deploy:
	bash scripts/deploy.sh $(VERSION)

# ─── 清理 ──────────────────────────────────────────────────
clean:
	rm -rf bin/ internal/entry/dist/ kernel-rs/target/
