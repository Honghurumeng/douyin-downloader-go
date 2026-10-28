package signer

import (
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sync"
)

// 签名器的打包产物：jsdom + signer.js 打成一个 bundle（4.9MB），外加四个 secsdk
// 运行时文件。由 signer/build.js 生成，随二进制一起分发，这样部署时不需要
// npm install，也不用在机器上留一千多个小文件。
//
//go:generate node ../signer/build.js
//go:embed assets
var assetsFS embed.FS

const (
	assetRoot  = "assets"
	bundleName = "signer.bundle.js"
)

var (
	assetMu   sync.Mutex
	assetPath string
)

// ensureAssets 把内嵌的签名器释放到缓存目录，返回可执行脚本的路径。
//
// 目录名带内容指纹，签名器版本变化时会落到新目录，旧目录不影响使用（也不会被自动清理，
// 但每个目录只有 5.7MB）。解压先写临时目录再 rename，避免中断后留下半个目录。
func ensureAssets() (string, error) {
	assetMu.Lock()
	defer assetMu.Unlock()

	if assetPath != "" {
		return assetPath, nil
	}

	fingerprint, err := assetsFingerprint()
	if err != nil {
		return "", err
	}

	cacheRoot, err := os.UserCacheDir()
	if err != nil {
		// 拿不到标准缓存目录时退到系统临时目录
		cacheRoot = os.TempDir()
	}

	dir := filepath.Join(cacheRoot, "douyin-downloader", "signer-"+fingerprint)
	entry := filepath.Join(dir, bundleName)

	if info, statErr := os.Stat(entry); statErr == nil && !info.IsDir() {
		assetPath = entry
		return entry, nil
	}

	if err := extractAssets(dir); err != nil {
		return "", fmt.Errorf("signer: release embedded assets: %w", err)
	}

	assetPath = entry
	return entry, nil
}

func assetsFingerprint() (string, error) {
	data, err := assetsFS.ReadFile(assetRoot + "/" + bundleName)
	if err != nil {
		return "", fmt.Errorf("signer: embedded bundle missing: %w", err)
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:8]), nil
}

func extractAssets(dir string) error {
	if err := os.MkdirAll(filepath.Dir(dir), 0o755); err != nil {
		return err
	}

	// 同一台机器上可能有多个进程同时首次启动，先解压到进程独占的临时目录
	tmp, err := os.MkdirTemp(filepath.Dir(dir), filepath.Base(dir)+".tmp-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmp)

	err = fs.WalkDir(assetsFS, assetRoot, func(p string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}

		rel, relErr := filepath.Rel(assetRoot, p)
		if relErr != nil {
			return relErr
		}
		target := filepath.Join(tmp, rel)

		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}

		data, readErr := assetsFS.ReadFile(p)
		if readErr != nil {
			return readErr
		}
		if writeErr := os.WriteFile(target, data, 0o644); writeErr != nil {
			return writeErr
		}
		return nil
	})
	if err != nil {
		return err
	}

	// 目标目录可能已被另一个进程建好，此时丢弃自己的临时目录即可
	if err := os.Rename(tmp, dir); err != nil {
		if info, statErr := os.Stat(filepath.Join(dir, bundleName)); statErr == nil && !info.IsDir() {
			return nil
		}
		return err
	}

	return nil
}

// embeddedScript 返回内嵌签名器解压后的入口路径。仅在磁盘上找不到源码时使用。
func embeddedScript() (string, error) {
	path, err := ensureAssets()
	if err != nil {
		return "", err
	}
	if _, statErr := os.Stat(path); statErr != nil {
		return "", errors.New("signer: embedded bundle vanished after extraction")
	}
	return path, nil
}
