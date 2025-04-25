#!/bin/bash
# 使用 Bash 作为脚本解释器

# 设置脚本执行选项：
# -e 表示如果任何命令返回非零退出状态（即执行失败），则立即退出脚本
set -e

# 检查 /data 目录是否存在
if [ ! -d "/data" ]; then
    # 如果 /data 目录不存在，提示用户需要挂载 /data 目录
    echo "Please mount /data"
    # 退出脚本，返回状态码 1 表示错误
    exit 1
fi

# 检查环境变量 EXTERNAL_ADDRESS 是否已设置
if [ -z "$EXTERNAL_ADDRESS" ]; then
    # 如果 EXTERNAL_ADDRESS 未设置，提示用户需要指定该变量
    echo "Please specify EXTERNAL_ADDRESS"
    # 退出脚本，返回状态码 1 表示错误
    exit 1
fi

# 确保 /data 目录下存在 authorized_keys 和 authorized_controllee_keys 文件
# 如果文件不存在则创建空文件
touch /data/authorized_keys /data/authorized_controllee_keys

# 检查是否提供了 SEED_AUTHORIZED_KEYS 环境变量
if [ ! -z "$SEED_AUTHORIZED_KEYS" ]; then
    # 检查 authorized_keys 文件是否非空（-s 检查文件存在且大小大于0）
    if [ -s /data/authorized_keys ]; then
        # 如果 authorized_keys 文件已存在且非空，忽略 SEED_AUTHORIZED_KEYS
        echo "authorized_keys is not empty, ignoring SEED_AUTHORIZED_KEYS\n"
    else
        # 如果 authorized_keys 文件为空，则使用 SEED_AUTHORIZED_KEYS 的值初始化它
        echo "Seeding authorized_keys...\n"
        # 将 SEED_AUTHORIZED_KEYS 的值写入 authorized_keys 文件
        echo $SEED_AUTHORIZED_KEYS > /data/authorized_keys
    fi
fi

# 切换到应用程序的 bin 目录
cd /app/bin

# 使用 exec 启动服务器程序，替换当前 shell 进程
# 传递以下参数：
# --datadir /data         指定数据目录为 /data
# --enable-client-downloads 启用客户端下载功能
# --tls                   启用 TLS 加密
# --external_address $EXTERNAL_ADDRESS 设置外部地址
# :2222                   监听 2222 端口
exec ./server --datadir /data --enable-client-downloads --tls --external_address $EXTERNAL_ADDRESS :2222