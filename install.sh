#!/bin/sh
# 构建 Aether 编译器（Linux、macOS）：  sh install.sh [--add-to-path]
set -e
root=$(cd "$(dirname "$0")" && pwd)

if ! command -v go >/dev/null 2>&1; then
    echo "找不到 go 命令。请先安装 Go 1.23 或更新版本：https://go.dev/dl/（国内可以用 https://golang.google.cn/dl/）" >&2
    exit 1
fi

# 如果 GOCACHE 指向的目录无法创建（例如所在的盘不存在），改用 Aether 自己的缓存目录
if [ -n "$GOCACHE" ] && ! mkdir -p "$GOCACHE" 2>/dev/null; then
    GOCACHE="${XDG_CACHE_HOME:-$HOME/.cache}/aether/go-build"
    export GOCACHE
    echo "GOCACHE 不可用，临时改用 $GOCACHE"
fi
# 编译器本身不用 cgo；用到 tensor 的程序由 aether 在 Linux 上自动用 cgo 构建（GPU 后端要加载 libOpenCL.so）
export GOTOOLCHAIN=local CGO_ENABLED=0

cd "$root"
go build -o bin/aether ./cmd/aether
echo "已生成 $root/bin/aether"
bin/aether run examples/hello.ae

if [ "$(uname -s)" = Linux ]; then
    tmp=$(mktemp -d)
    printf 'import tensor\nprint(tensor.device_info())\n' > "$tmp/gpu.ae"
    printf '检查 GPU：'
    bin/aether run "$tmp/gpu.ae" || true
    rm -rf "$tmp"
    if ! command -v gcc >/dev/null 2>&1 && ! command -v cc >/dev/null 2>&1 && ! command -v clang >/dev/null 2>&1; then
        echo "提示：没有找到 C 编译器，tensor 只能使用 CPU。要使用 GPU，请安装 gcc（Ubuntu/Debian：apt install build-essential）"
    fi
    if ! ldconfig -p 2>/dev/null | grep -q 'libOpenCL\.so\.1'; then
        echo "提示：没有找到 libOpenCL.so.1，tensor 只能使用 CPU。要使用 GPU，请安装 OpenCL 加载器（Ubuntu/Debian：apt install ocl-icd-libopencl1）与显卡驱动"
    fi
fi

if [ "$1" = "--add-to-path" ]; then
    rc="$HOME/.bashrc"
    if [ "$(basename "${SHELL:-sh}")" = zsh ]; then
        rc="$HOME/.zshrc"
    fi
    line="export PATH=\"$root/bin:\$PATH\""
    if grep -qF "$line" "$rc" 2>/dev/null; then
        echo "$root/bin 已经在 $rc 的 PATH 中"
    else
        echo "$line" >> "$rc"
        echo "已把 $root/bin 加入 $rc 的 PATH（重新打开终端或运行 . $rc 后生效）"
    fi
fi
