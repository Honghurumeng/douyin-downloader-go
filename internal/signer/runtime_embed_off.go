//go:build !douyin_embed_runtime

package signer

// 默认构建不带 node 运行时：runtimeArchive 为 nil，改为在系统里找 node。
// 单文件发布版用 -tags douyin_embed_runtime 构建，见 runtime.go。

var runtimeArchive []byte

