//go:build douyin_embed_runtime && linux && arm64

package signer

import _ "embed"

// 官方 node v22.23.4 linux-arm64，strip 后 xz -9 压缩
//
//go:embed runtime/linux-arm64/node.xz
var runtimeArchive []byte

