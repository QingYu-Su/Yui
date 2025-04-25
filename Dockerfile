# 使用官方 Golang 镜像作为基础镜像，基于 Debian bullseye 发行版
FROM golang:bullseye

# 设置工作目录为 /app（后续命令都在此目录下执行）
WORKDIR /app

# 更新系统包列表并升级所有已安装的包
RUN apt update -y
RUN apt upgrade -y
# 安装必要的构建工具：
# upx-ucl - 可执行文件压缩工具
# gcc-mingw-w64 - 跨平台Windows交叉编译工具链
RUN apt install -y upx-ucl gcc-mingw-wix

# 安装 garble 工具（Go代码混淆工具）
RUN go install mvdan.cc/garble@master

# 将 Go 的 GOPATH/bin 目录添加到系统PATH环境变量中
# 这样可以直接使用通过go install安装的工具（如garble）
ENV PATH="${PATH}:$(go env GOPATH)/bin"

# 先只复制go.mod和go.sum文件（利用Docker缓存层优化）
# -x 参数显示详细的下载过程
COPY go.mod go.sum ./
RUN go mod download -x

# 复制当前目录所有文件到容器的工作目录
COPY . .

# 执行Makefile中的server目标（通常用于构建服务器程序）
RUN make server

# 声明容器运行时监听的端口号（2222）
# 这只是文档性声明，实际映射需要在运行容器时通过-p参数指定
EXPOSE 2222

# 给入口点脚本添加可执行权限
RUN chmod +x /app/docker-entrypoint.sh

# 设置容器启动时执行的入口点脚本
# 使用exec格式（推荐），确保正确处理信号
ENTRYPOINT ["/app/docker-entrypoint.sh"]