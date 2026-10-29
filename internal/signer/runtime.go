package signer

// 单文件发布版的内置运行时。
//
// node 可执行文件以 xz 压缩后编进二进制（约 22~25MB），首次解析时解压到缓存目录，
// 之后直接复用——与 jsdom bundle 的释放逻辑一致。这样发布就是一个文件，目标机器
// 不用装 node，也不用配 PATH。
//
// 是否内置由 build tag 决定：
//
//	go build -tags douyin_embed_runtime ./cmd/server
//
// 不带该 tag 的构建（开发、测试）里 runtimeArchive 为 nil，退回查找系统 node。

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"sync"

	"github.com/ulikunitz/xz"
)

const runtimeExecutableName = "node"

// errNoEmbeddedRuntime 表示这份二进制没编入 node 运行时。
var errNoEmbeddedRuntime = errors.New("signer: 本二进制未内置 node 运行时")

var (
	runtimeMu        sync.Mutex
	runtimeCachePath string
)

// EmbeddedRuntime 报告当前二进制是否自带 node 运行时。
func EmbeddedRuntime() bool { return len(runtimeArchive) > 0 }

// ensureEmbeddedRuntime 把内置的 node 解压到缓存目录，返回可执行文件路径。
//
// 目录名带内容指纹（压缩包 sha256 前 8 字节），换版本会落到新目录，旧目录不影响使用。
func ensureEmbeddedRuntime() (string, error) {
	if len(runtimeArchive) == 0 {
		return "", errNoEmbeddedRuntime
	}

	runtimeMu.Lock()
	defer runtimeMu.Unlock()

	if runtimeCachePath != "" {
		if info, err := os.Stat(runtimeCachePath); err == nil && !info.IsDir() {
			return runtimeCachePath, nil
		}
		runtimeCachePath = ""
	}

	sum := sha256.Sum256(runtimeArchive)
	dirName := "node-" + hex.EncodeToString(sum[:8])
	dir := filepath.Join(cacheRoot(), dirName)
	exe := filepath.Join(dir, runtimeExecutableName)

	if info, err := os.Stat(exe); err == nil && !info.IsDir() && info.Mode()&0o111 != 0 {
		runtimeCachePath = exe
		return exe, nil
	}

	if err := releaseRuntime(dir, exe); err != nil {
		return "", fmt.Errorf("signer: 释放内置 node 运行时: %w", err)
	}
	log.Printf("[signer] 已释放内置 node 运行时到 %s（可能耗时数秒，之后启动直接复用）", dir)

	// 只有真的解压过才清理：node 版本没变时这里根本不会执行，稳态启动零扫描
	pruneOldCache("node", dirName)

	runtimeCachePath = exe
	return exe, nil
}

// releaseRuntime 先解压到进程独占的临时目录，再整目录 rename，避免中断留下半个文件。
func releaseRuntime(dir, exe string) error {
	parent := filepath.Dir(dir)
	if err := os.MkdirAll(parent, 0o755); err != nil {
		return err
	}

	tmpDir, err := os.MkdirTemp(parent, filepath.Base(dir)+".tmp-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmpDir)

	tmpExe := filepath.Join(tmpDir, runtimeExecutableName)
	out, err := os.OpenFile(tmpExe, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o755)
	if err != nil {
		return err
	}

	reader, err := xz.NewReader(bytes.NewReader(runtimeArchive))
	if err != nil {
		_ = out.Close()
		return err
	}
	if _, err := io.Copy(out, reader); err != nil {
		_ = out.Close()
		return err
	}
	if err := out.Close(); err != nil {
		return err
	}
	// 压缩包里的权限位不可靠，显式给可执行
	if err := os.Chmod(tmpExe, 0o755); err != nil {
		return err
	}

	if err := os.Rename(tmpDir, dir); err != nil {
		// 同一台机器上可能多个进程同时首次启动，对方已经建好就直接用
		if info, statErr := os.Stat(exe); statErr == nil && !info.IsDir() {
			return nil
		}
		return err
	}

	return nil
}
