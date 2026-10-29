//go:build douyin_embed_runtime && darwin && arm64

package signer

import _ "embed"

// 官方 node v22.23.4 darwin-arm64，xz -9 压缩（macOS 上不能 strip，会破坏代码签名）
//
//go:embed runtime/darwin-arm64/node.xz
var runtimeArchive []byte

