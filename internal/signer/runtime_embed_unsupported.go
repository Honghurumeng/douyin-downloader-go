//go:build douyin_embed_runtime && !((darwin && arm64) || (linux && amd64) || (linux && arm64))

package signer

// 带 douyin_embed_runtime 构建，但当前平台没有预置运行时。
// 让编译仍然通过，运行时退回查找系统 node。

var runtimeArchive []byte

