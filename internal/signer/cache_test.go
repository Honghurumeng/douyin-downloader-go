package signer

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestPruneOldCache(t *testing.T) {
	root := t.TempDir()
	t.Setenv("DOUYIN_CACHE_DIR", root)

	old := time.Now().Add(-48 * time.Hour)
	fresh := time.Now()

	// 造一个目录并把 mtime 设成指定时间
	mk := func(name string, when time.Time) string {
		dir := filepath.Join(root, name)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("建目录 %s: %v", name, err)
		}
		if err := os.WriteFile(filepath.Join(dir, "node"), []byte("x"), 0o755); err != nil {
			t.Fatalf("写文件: %v", err)
		}
		if err := os.Chtimes(dir, when, when); err != nil {
			t.Fatalf("改 mtime: %v", err)
		}
		return dir
	}

	current := mk("node-aaaaaaaaaaaaaaaa", old)             // 当前版本：必须保留
	stale := mk("node-bbbbbbbbbbbbbbbb", old)               // 上一个版本：应删除
	staleTmp := mk("node-cccccccccccccccc.tmp-123456", old) // 强杀留下的半成品：应删除
	sibling := mk("signer-ddddddddddddddde", old)           // 另一类产物：不归这次清理管
	freshDir := mk("node-eeeeeeeeeeeeeeee", fresh)          // 宽限期内（可能还有进程在用）：保留
	unrelated := mk("keepme", old)                          // 不符合命名：绝不碰

	pruneOldCache("node", filepath.Base(current))

	for _, path := range []string{current, sibling, freshDir, unrelated} {
		if _, err := os.Stat(path); err != nil {
			t.Errorf("应该保留 %s: %v", filepath.Base(path), err)
		}
	}
	for _, path := range []string{stale, staleTmp} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Errorf("应该删除 %s，实际 err=%v", filepath.Base(path), err)
		}
	}
}

// 开发构建（不带 build tag，只释放签名器脚本）不能把单文件版的 node 目录删掉。
func TestPruneOnlyTouchesOwnFamily(t *testing.T) {
	root := t.TempDir()
	t.Setenv("DOUYIN_CACHE_DIR", root)

	old := time.Now().Add(-48 * time.Hour)
	nodeDir := filepath.Join(root, "node-aaaaaaaaaaaaaaaa")
	if err := os.MkdirAll(nodeDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(nodeDir, old, old); err != nil {
		t.Fatal(err)
	}

	pruneOldCache("signer", "signer-bbbbbbbbbbbbbbbb")

	if _, err := os.Stat(nodeDir); err != nil {
		t.Errorf("node 目录被误删: %v", err)
	}
}

func TestCacheRootHonoursEnv(t *testing.T) {
	custom := t.TempDir()
	t.Setenv("DOUYIN_CACHE_DIR", custom)
	if got := cacheRoot(); got != custom {
		t.Errorf("cacheRoot() = %q, 期望 %q", got, custom)
	}

	t.Setenv("DOUYIN_CACHE_DIR", "")
	if got := cacheRoot(); !strings.HasSuffix(got, string(os.PathSeparator)+"douyin-downloader") {
		t.Errorf("默认 cacheRoot() = %q, 应以 douyin-downloader 结尾", got)
	}
}

func TestDirSize(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a"), make([]byte, 1024), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "sub", "b"), make([]byte, 512), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := dirSize(dir); got != 1536 {
		t.Errorf("dirSize() = %d, 期望 1536", got)
	}
}
