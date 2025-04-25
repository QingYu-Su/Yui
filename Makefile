# 如果定义了 RSSH_HOMESERVER 环境变量，则将其值传递给 Go 的 main.destination 变量
ifdef RSSH_HOMESERVER
	LDFLAGS += -X main.destination=$(RSSH_HOMESERVER)
endif

# 如果定义了 RSSH_FINGERPRINT 环境变量，则将其值传递给 Go 的 main.fingerprint 变量
ifdef RSSH_FINGERPRINT
	LDFLAGS += -X main.fingerprint=$(RSSH_FINGERPRINT)
endif

# 如果定义了 RSSH_PROXY 环境变量，则将其值传递给 Go 的 main.proxy 变量
ifdef RSSH_PROXY
	LDFLAGS += -X main.proxy=$(RSSH_PROXY)
endif

# 如果定义了 IGNORE 环境变量，则将其值传递给 Go 的 main.ignoreInput 变量
ifdef IGNORE
	LDFLAGS += -X main.ignoreInput=$(IGNORE)
endif

# 如果没有定义 CGO_ENABLED 环境变量，则设置 CGO_ENABLED=0（禁用 CGO）
ifndef CGO_ENABLED
	export CGO_ENABLED=0
endif

# 设置构建标志：-trimpath（移除文件系统中的绝对路径信息）
BUILD_FLAGS := -trimpath

# 将 Git 标签信息（版本号）传递给 Go 的内部.Version 变量
LDFLAGS += -X 'github.com/NHAS/reverse_ssh/internal.Version=$(shell git describe --tags)'

# 设置发布版本的链接器标志：除了之前的 LDFLAGS 外，还添加 -s -w（缩小二进制文件大小）
# -s: 省略符号表和调试信息
# -w: 省略 DWARF 符号表
LDFLAGS_RELEASE = $(LDFLAGS) -s -w

# debug 目标：构建调试版本
debug: .generate_keys
	# 构建所有包，输出到 bin 目录
	go build $(BUILD_FLAGS) -ldflags="$(LDFLAGS)" -o bin ./...
	# 交叉编译 Windows 64位版本
	GOOS=windows GOARCH=amd64 go build $(BUILD_FLAGS) -ldflags="$(LDFLAGS_RELEASE)" -o bin ./...

# release 目标：构建发布版本
release: .generate_keys 
	# 构建所有包，使用发布版本的链接器标志
	go build $(BUILD_FLAGS) -ldflags="$(LDFLAGS_RELEASE)" -o bin ./...
	# 交叉编译 Windows 64位版本
	GOOS=windows GOARCH=amd64 go build $(BUILD_FLAGS) -ldflags="$(LDFLAGS_RELEASE)" -o bin ./...

# e2e 目标：构建端到端测试版本
e2e: .generate_keys
	# 构建 e2e 测试版本，传递版本信息
	go build -ldflags="github.com/NHAS/reverse_ssh/e2e.Version=$(shell git describe --tags)" -o e2e ./...
	# 将公钥复制到 e2e 目录
	cp internal/client/keys/private_key.pub e2e/authorized_controllee_keys

# client 目标：仅构建客户端
client: .generate_keys
	# 构建客户端程序
	go build $(BUILD_FLAGS) -ldflags="$(LDFLAGS_RELEASE)" -o bin ./cmd/client

# client_dll 目标：构建客户端 DLL
client_dll: .generate_keys
	# 检查是否定义了 RSSH_HOMESERVER（DLL 需要预先设置回调服务器地址）
	test -n "$(RSSH_HOMESERVER)" # Shared objects cannot take arguments, so must have a callback server baked in (define RSSH_HOMESERVER)
	# 启用 CGO，构建为 C 共享库（DLL）
	CGO_ENABLED=1 go build $(BUILD_FLAGS) -tags=cshared -buildmode=c-shared -ldflags="$(LDFLAGS_RELEASE)" -o bin/client.dll ./cmd/client

# server 目标：仅构建服务器
server:
	# 创建 bin 目录
	mkdir -p bin
	# 构建服务器程序
	go build $(BUILD_FLAGS) -ldflags="$(LDFLAGS_RELEASE)" -o bin ./cmd/server

# .generate_keys 目标：生成 SSH 密钥对
.generate_keys:
	# 创建 bin 目录
	mkdir -p bin
	# 生成 ED25519 类型的 SSH 密钥对（无密码，无注释）
	# 如果密钥已存在，忽略错误（|| true）
	ssh-keygen -t ed25519 -N '' -C '' -f internal/client/keys/private_key || true
	# 确保 authorized_controllee_keys 文件存在
	touch bin/authorized_controllee_keys
	# 如果公钥不在 authorized_controllee_keys 中，则追加进去
	@grep -q "$$(cat internal/client/keys/private_key.pub)" bin/authorized_controllee_keys || cat internal/client/keys/private_key.pub >> bin/authorized_controllee_keys