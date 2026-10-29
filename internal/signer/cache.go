package signer

// 缓存目录布局与清理。
//
// 两类产物都以内容指纹命名，换版本会落到新目录：
//
//	<cache>/douyin-downloader/node-<16hex>/node                解压后的 node 运行时（约 102MB）
//	<cache>/douyin-downloader/signer-<16hex>/signer.bundle.js  签名器脚本与 secsdk（约 5.7MB）
//
// 指纹目录的好处是升级和回滚都不用重新解压；代价是旧目录没人清。所以只在「确实解压了
// 新东西」之后扫一遍父目录，把同类产物的旧指纹目录、以及进程被强杀时留下的 .tmp- 目录
// 删掉。稳态启动（缓存命中）完全不做扫描。

import (
	"log"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// pruneGrace 是旧目录的宽限期。
//
// 滚动重启时新旧两个版本的进程可能同时在跑：新进程刚解压完就删掉旧目录，会让旧进程
// 在 sidecar 重启时找不到脚本。10 分钟内建出来的目录一律留着，让对方先跑完。
const pruneGrace = 10 * time.Minute

var cacheEntryPattern = regexp.MustCompile(`^(node|signer)-[0-9a-f]{16}(\.tmp-[0-9]+)?$`)

// cacheRoot 是两类产物共用的父目录。
//
// 默认落在 <系统缓存目录>/douyin-downloader；设置 DOUYIN_CACHE_DIR 时直接把它当作
// 该目录（不再套一层 douyin-downloader），方便容器里挂到数据卷上。
func cacheRoot() string {
	if custom := strings.TrimSpace(os.Getenv("DOUYIN_CACHE_DIR")); custom != "" {
		return custom
	}
	return filepath.Join(userCacheDir(), "douyin-downloader")
}

// userCacheDir 返回系统缓存根目录。
func userCacheDir() string {
	if root, err := os.UserCacheDir(); err == nil && root != "" {
		return root
	}
	// 拿不到标准缓存目录（比如 systemd 下没有 HOME）时退到系统临时目录
	return os.TempDir()
}

// pruneOldCache 清理同一类产物的旧版本目录，keep 是当前版本目录名。
//
// 只动 family 前缀且格式完全匹配的目录，父目录下别的东西一概不碰。
func pruneOldCache(family, keep string) {
	root := cacheRoot()
	entries, err := os.ReadDir(root)
	if err != nil {
		// 目录还没建出来（首次运行）或其他进程正在操作，跳过即可
		return
	}

	cutoff := time.Now().Add(-pruneGrace)
	prefix := family + "-"
	for _, entry := range entries {
		name := entry.Name()
		if name == keep || !strings.HasPrefix(name, prefix) || !cacheEntryPattern.MatchString(name) {
			continue
		}

		info, err := entry.Info()
		if err != nil || info.ModTime().After(cutoff) {
			continue
		}

		path := filepath.Join(root, name)
		size := dirSize(path)
		if err := os.RemoveAll(path); err != nil {
			log.Printf("[signer] 清理旧缓存 %s 失败: %v", path, err)
			continue
		}
		log.Printf("[signer] 清理旧缓存 %s（释放 %.1fMB）", name, float64(size)/(1<<20))
	}
}

// dirSize 统计目录占用。只 stat 不读文件，100MB 的运行时也很快。
func dirSize(path string) int64 {
	var total int64
	_ = filepath.WalkDir(path, func(_ string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		if info, statErr := d.Info(); statErr == nil {
			total += info.Size()
		}
		return nil
	})
	return total
}
