#!/usr/bin/env bash
#
# 生成 internal/signer/runtime/<platform>/node.xz —— 单文件版内嵌的 node 运行时。
#
# 用法：
#   scripts/prepare-runtime.sh                 # 三个平台都做
#   scripts/prepare-runtime.sh linux-amd64     # 只做一个
#   NODE_VERSION=v22.23.4 scripts/prepare-runtime.sh
#
# 依赖：curl、tar、xz。给 linux 目标 strip 时需要 docker —— macOS 自带的 strip 只认
# Mach-O，不 strip 的话二进制 125MB（xz 后约 30MB），strip 后 107MB（xz 约 25MB）。
# darwin 目标不 strip：会破坏代码签名，arm64 上直接跑不起来。
#
# 产物不进 git（见 .gitignore），构建单文件版前跑一次即可。

set -euo pipefail

NODE_VERSION="${NODE_VERSION:-v22.23.4}"

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
OUT="$ROOT/internal/signer/runtime"
CACHE="${NODE_DIST_CACHE:-$ROOT/.cache/node-dist}"

# 平台目录名 → node 官方分发名
declare -a PLATFORMS=("linux-amd64:linux-x64" "linux-arm64:linux-arm64" "darwin-arm64:darwin-arm64")

targets=("$@")
if [ ${#targets[@]} -eq 0 ]; then
  targets=(linux-amd64 linux-arm64 darwin-arm64)
fi

log() { printf '\033[2m%s\033[0m\n' "$*"; }

dist_for() {
  local want="$1" entry
  for entry in "${PLATFORMS[@]}"; do
    if [ "${entry%%:*}" = "$want" ]; then
      printf '%s' "${entry##*:}"
      return 0
    fi
  done
  echo "未知平台: $want" >&2
  return 1
}

need_docker() {
  command -v docker >/dev/null 2>&1 || return 1
  docker info >/dev/null 2>&1 || return 1
}

strip_linux() {
  local arch="$1" src="$2" dst="$3"
  # 容器里 /w 就是 $CACHE，所以只传文件名
  docker run --rm --platform "linux/$arch" -v "$CACHE:/w" -w /w debian:bookworm-slim \
    bash -lc "apt-get update -qq && apt-get install -y -qq binutils >/dev/null 2>&1 && strip -o '$(basename "$dst")' '$(basename "$src")'"
}

mkdir -p "$CACHE"

for platform in "${targets[@]}"; do
  dist="$(dist_for "$platform")"
  tarball="$CACHE/node-$NODE_VERSION-$dist.tar.xz"
  binary="$CACHE/node-$NODE_VERSION-$dist"

  mkdir -p "$OUT/$platform"

  if [ ! -f "$binary" ]; then
    if [ ! -f "$tarball" ]; then
      log "下载 $dist ..."
      curl -fsSL --retry 3 "https://nodejs.org/dist/$NODE_VERSION/node-$NODE_VERSION-$dist.tar.xz" -o "$tarball"
    fi

    log "解出 $dist/bin/node ..."
    rm -rf "$CACHE/extract"
    mkdir -p "$CACHE/extract"
    tar -xJf "$tarball" -C "$CACHE/extract" --strip-components=2 "node-$NODE_VERSION-$dist/bin/node"
    mv "$CACHE/extract/node" "$binary"
    rm -rf "$CACHE/extract"
  fi

  # linux 目标顺手 strip，省 18MB 左右
  if [[ "$platform" == linux-* ]]; then
    arch="${platform#linux-}"
    if need_docker; then
      log "strip $platform ..."
      strip_linux "$arch" "$binary" "$binary.stripped"
    else
      log "跳过 strip（没有可用的 docker），产物会大一些"
    fi
  fi

  src="$binary"
  [ -f "$binary.stripped" ] && src="$binary.stripped"

  log "压缩 $platform ..."
  xz -9 -T0 -c "$src" > "$OUT/$platform/node.xz"

  printf '%-14s %6.1fMB -> %5.1fMB  %s\n' \
    "$platform" \
    "$(wc -c < "$src" | awk '{print $1/1048576}')" \
    "$(wc -c < "$OUT/$platform/node.xz" | awk '{print $1/1048576}')" \
    "internal/signer/runtime/$platform/node.xz"

  rm -f "$binary.stripped"
done

echo
echo "完成。构建单文件版："
echo "  GOOS=linux GOARCH=amd64 go build -tags douyin_embed_runtime -trimpath -ldflags='-s -w' -o build/douyin-server-linux-amd64 ./cmd/server"
