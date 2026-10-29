// Package signer 管理抖音 web 接口签名器（signer/signer.js）的生命周期，并代理详情查询。
//
// 抖音要求请求携带 x-secsdk-web-signature，这个值只能由官方 secsdk 运行时在类浏览器环境里
// 实时计算，Go 无法复现，所以把这一小块交给 Node。进程按需拉起：
//
//	第一次调用 -> 探测 127.0.0.1:8099，没人监听就 spawn 一个
//	空闲 5 分钟 -> Node 侧自行退出，Go 下次调用时再拉，不常驻占内存
//
// 下载仍然留在 Go（见 downloader.DownloadToFile），Node 只负责签名和转发一次 API 请求。
package signer

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	defaultPort = 8099
	// 端口被占用时最多往后试这么多个
	portScanLimit = 10

	bootTimeout    = 30 * time.Second
	healthTimeout  = 800 * time.Millisecond
	requestTimeout = 30 * time.Second
	// Node 侧空闲多久自行退出
	defaultIdleTimeout = 5 * time.Minute
)

// DefaultIdle 是 sidecar 的空闲退出时间，测试里可以调小。
var DefaultIdle = defaultIdleTimeout

// ErrNodeMissing 表示 PATH 里找不到 node。
//
// 这是发布态最常见、也最容易被忽略的失败原因：签名器本身已经编进二进制，
// 但它必须由 Node 执行。单独暴露这个错误，好让上层给出可操作的提示，
// 而不是只把一个 exec 错误丢给前端。
var ErrNodeMissing = errors.New("signer: node not found in PATH")

// node 来源，只用于日志，方便确认这份二进制到底用了哪个运行时。
const (
	nodeSourceEnv      = "DOUYIN_NODE_BIN"
	nodeSourceLocal    = "二进制同目录"
	nodeSourceEmbedded = "内置运行时"
	nodeSourcePath     = "PATH"
)

// Preflight 探测签名器的外部依赖，返回 node 路径、版本号和来源。
//
// 服务启动时调用一次，把「能不能解析」提前写进日志——否则第一个失败的请求
// 只会返回一个 HTTP 错误，日志里连 [signer] 都不会出现，很难看出是缺 node。
//
// 内置运行时的首次调用会解压 100MB 左右到缓存目录，放在启动阶段做，
// 问题能早暴露，也不至于让第一个用户请求多等一两秒。
func Preflight() (string, string, string, error) {
	nodePath, source, err := resolveNode()
	if err != nil {
		return "", "", "", err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	out, err := exec.CommandContext(ctx, nodePath, "--version").Output()
	if err != nil {
		return nodePath, "", source, fmt.Errorf("node found at %s but failed to run: %w", nodePath, err)
	}

	return nodePath, strings.TrimSpace(string(out)), source, nil
}

// resolveNode 定位 node 可执行文件。
//
// 查找顺序：
//  1. DOUYIN_NODE_BIN（显式指定，最高优先级）
//  2. 二进制所在目录 / 当前目录下的 node（发布包自带运行时，解压即用，不装也不碰 PATH）
//  3. 内置运行时（单文件发布版，xz 压缩后编进二进制，首次使用解压到缓存目录）
//  4. PATH
//
// 内置排在 PATH 前面是有意的：它是随二进制发布的确定版本，而系统里的 node
// 可能是 12 那种跑不起来的老版本。
func resolveNode() (string, string, error) {
	if custom := strings.TrimSpace(os.Getenv("DOUYIN_NODE_BIN")); custom != "" {
		abs, err := filepath.Abs(custom)
		if err != nil {
			return "", "", fmt.Errorf("signer: invalid DOUYIN_NODE_BIN %q: %w", custom, err)
		}
		info, statErr := os.Stat(abs)
		if statErr != nil {
			return "", "", fmt.Errorf("signer: DOUYIN_NODE_BIN points to a missing file: %s", abs)
		}
		if info.IsDir() {
			return "", "", fmt.Errorf("signer: DOUYIN_NODE_BIN is a directory: %s", abs)
		}
		return abs, nodeSourceEnv, nil
	}

	for _, candidate := range localNodeCandidates() {
		if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
			return candidate, nodeSourceLocal, nil
		}
	}

	if EmbeddedRuntime() {
		exe, err := ensureEmbeddedRuntime()
		if err != nil {
			return "", "", err
		}
		return exe, nodeSourceEmbedded, nil
	}

	nodePath, err := exec.LookPath("node")
	if err != nil {
		return "", "", fmt.Errorf("%w (需要 Node.js 20+；也可把 node 可执行文件放到二进制同目录，或用 DOUYIN_NODE_BIN 指定绝对路径)", ErrNodeMissing)
	}

	return nodePath, nodeSourcePath, nil
}

// localNodeCandidates 列出「跟着发布包走」的 node 位置。
func localNodeCandidates() []string {
	names := []string{"node", "node.exe", filepath.Join("runtime", "node"), filepath.Join("runtime", "bin", "node")}

	var roots []string
	seen := make(map[string]bool)
	add := func(dir string) {
		if dir == "" || seen[dir] {
			return
		}
		seen[dir] = true
		roots = append(roots, dir)
	}

	if exe, err := os.Executable(); err == nil {
		add(filepath.Dir(exe))
	}
	if wd, err := os.Getwd(); err == nil {
		add(wd)
	}

	candidates := make([]string, 0, len(roots)*len(names))
	for _, root := range roots {
		for _, name := range names {
			candidates = append(candidates, filepath.Join(root, name))
		}
	}
	return candidates
}

// Client 是一个惰性启动的签名器客户端。零值不可用，请用 New。
type Client struct {
	script string
	port   int
	idle   time.Duration
	logger *log.Logger

	http *http.Client

	mu      sync.Mutex
	baseURL string
	proc    *subprocess
}

type subprocess struct {
	cmd  *exec.Cmd
	done chan error
}

// New 创建客户端。script 为空时按默认顺序查找 signer.js，port 为 0 时用 8099。
func New(script string, port int) *Client {
	if port <= 0 {
		port = defaultPort
	}
	idle := DefaultIdle
	if idle <= 0 {
		idle = defaultIdleTimeout
	}
	return &Client{
		script: script,
		port:   port,
		idle:   idle,
		logger: log.New(os.Stderr, "[signer] ", log.LstdFlags),
		http: &http.Client{
			Timeout: requestTimeout,
			Transport: &http.Transport{
				Proxy:             nil, // 本地回环不要走代理
				DisableKeepAlives: false,
				MaxIdleConns:      8,
			},
		},
	}
}

// Detail 取作品详情的原始 JSON（抖音 API 原样返回）。
func (c *Client) Detail(ctx context.Context, awemeID string) ([]byte, error) {
	awemeID = strings.TrimSpace(awemeID)
	if awemeID == "" {
		return nil, errors.New("signer: empty aweme id")
	}

	var lastErr error
	for attempt := 0; attempt < 2; attempt++ {
		base, err := c.ensureRunning(ctx)
		if err != nil {
			return nil, err
		}

		body, err := c.fetchDetail(ctx, base, awemeID)
		if err == nil {
			return body, nil
		}
		lastErr = err

		// 大概率是 sidecar 刚好空闲退出，重置状态后重来一次
		c.invalidate()
	}

	c.logger.Printf("detail %s failed: %v", awemeID, lastErr)
	return nil, fmt.Errorf("signer: detail %s: %w", awemeID, lastErr)
}

// Close 结束由本客户端拉起的进程。
func (c *Client) Close() error {
	c.mu.Lock()
	proc := c.proc
	c.proc = nil
	c.baseURL = ""
	c.mu.Unlock()

	if proc == nil {
		return nil
	}
	if proc.cmd.Process != nil {
		_ = proc.cmd.Process.Kill()
	}
	return nil
}

func (c *Client) fetchDetail(ctx context.Context, base, awemeID string) ([]byte, error) {
	endpoint := base + "/detail?aweme_id=" + url.QueryEscape(awemeID)

	reqCtx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 32<<20))
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("signer returned %s: %s", resp.Status, truncate(body, 300))
	}
	// 签名失败时抖音会回 200 + 空 body，Node 原样透传
	if len(strings.TrimSpace(string(body))) == 0 {
		return nil, errors.New("signer returned empty body (signature likely rejected)")
	}

	return body, nil
}

// ensureRunning 返回一个可用的 base URL，必要时拉起 sidecar。
func (c *Client) ensureRunning(ctx context.Context) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.baseURL != "" {
		if c.healthy(c.baseURL) {
			return c.baseURL, nil
		}
		// 记录中的实例已经不可用（多半空闲退出了）
		c.baseURL = ""
		c.proc = nil
	}

	for port := c.port; port < c.port+portScanLimit; port++ {
		base := fmt.Sprintf("http://127.0.0.1:%d", port)

		// 复用已经在跑的实例（上次遗留的、或用户手工起的）
		if c.healthy(base) {
			c.baseURL = base
			// 记一行：复用时不打印任何东西的话，日志里会完全看不到签名器，
			// 排查「到底走没走到签名这一步」就没有依据了
			c.logger.Printf("reusing sidecar on %s", base)
			return base, nil
		}
		if !portFree(port) {
			continue
		}

		// spawn 失败通常不是端口问题（端口在上面已经探过），而是 node 缺失、
		// signer.js 找不到或脚本自身报错，这类问题重试也没用，直接把原因抛出去。
		if err := c.spawn(ctx, base, port); err != nil {
			// 必须留一行日志：这类错误只回给调用方，不写日志的话服务端日志里
			// 一个 [signer] 前缀都不会出现，排查时完全看不出是签名器的问题。
			c.logger.Printf("start sidecar failed: %v", err)
			return "", err
		}

		c.baseURL = base
		return base, nil
	}

	return "", fmt.Errorf("signer: no free port in %d-%d", c.port, c.port+portScanLimit-1)
}

// errProcessExited 表示子进程启动了但立刻退出（缺依赖、脚本抛错等）。
var errProcessExited = errors.New("signer process exited during boot")

func (c *Client) spawn(ctx context.Context, base string, port int) error {
	script, err := c.resolveScript()
	if err != nil {
		return err
	}
	nodePath, source, err := resolveNode()
	if err != nil {
		return err
	}
	c.logger.Printf("using node from %s: %s", source, nodePath)

	cmd := exec.Command(nodePath, script,
		"--serve", strconv.Itoa(port),
		"--idle-timeout", strconv.Itoa(int(c.idle.Seconds())),
	)
	cmd.Dir = filepath.Dir(script)
	cmd.Env = sidecarEnv()
	// sidecar 的输出对排查很有用，但不要污染主程序 stdout
	cmd.Stdout = &prefixWriter{prefix: "[signer:out] ", logger: c.logger}
	cmd.Stderr = &prefixWriter{prefix: "[signer:err] ", logger: c.logger}

	start := time.Now()
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("signer: start node: %w", err)
	}

	proc := &subprocess{cmd: cmd, done: make(chan error, 1)}
	go func() { proc.done <- cmd.Wait() }()

	c.logger.Printf("spawned node pid %d in %s, script %s", cmd.Process.Pid, time.Since(start).Round(time.Millisecond), script)

	deadline := time.Now().Add(bootTimeout)
	for time.Now().Before(deadline) {
		select {
		case err := <-proc.done:
			// 补读一次 stderr 一般已在日志里，这里只给退出状态
			if err == nil {
				return fmt.Errorf("%w: exited with code 0", errProcessExited)
			}
			return fmt.Errorf("%w: %v", errProcessExited, err)
		case <-ctx.Done():
			_ = cmd.Process.Kill()
			return ctx.Err()
		default:
		}

		if c.healthy(base) {
			c.proc = proc
			c.logger.Printf("sidecar ready on 127.0.0.1:%d in %s (pid %d)", port, time.Since(start).Round(time.Millisecond), cmd.Process.Pid)
			return nil
		}
		time.Sleep(60 * time.Millisecond)
	}

	_ = cmd.Process.Kill()
	return fmt.Errorf("signer: sidecar did not become ready within %s", bootTimeout)
}

// sidecarEnv 构造子进程环境。
//
// 这里必须剔除 NODE_OPTIONS。宿主环境（IDE 插件、CI、各种 runtime manager）常常会往
// 里面塞 --require 钩子，某些钩子会包装 fs.readFileSync，使每次读取多花几十毫秒。
// jsdom 冷启动要读九百多个模块文件，平时 0.3 秒的事会被拖到 20 秒以上。
// sidecar 只跑 signer.js，不需要那些钩子。
func sidecarEnv() []string {
	env := os.Environ()
	out := make([]string, 0, len(env))
	for _, kv := range env {
		if strings.HasPrefix(kv, "NODE_OPTIONS=") {
			continue
		}
		out = append(out, kv)
	}
	return out
}

func (c *Client) invalidate() {
	c.mu.Lock()
	c.baseURL = ""
	c.proc = nil
	c.mu.Unlock()
}

func (c *Client) healthy(base string) bool {
	ctx, cancel := context.WithTimeout(context.Background(), healthTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"/health", nil)
	if err != nil {
		return false
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return false
	}

	var payload struct {
		OK bool `json:"ok"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 4<<10)).Decode(&payload); err != nil {
		return false
	}
	return payload.OK
}

func (c *Client) resolveScript() (string, error) {
	if c.script != "" {
		abs, err := filepath.Abs(c.script)
		if err != nil {
			return "", err
		}
		if _, err := os.Stat(abs); err != nil {
			return "", fmt.Errorf("signer: script not found at %s", abs)
		}
		return abs, nil
	}

	if env := strings.TrimSpace(os.Getenv("DOUYIN_SIGNER_SCRIPT")); env != "" {
		abs, err := filepath.Abs(env)
		if err != nil {
			return "", fmt.Errorf("signer: invalid DOUYIN_SIGNER_SCRIPT %q: %w", env, err)
		}
		if _, statErr := os.Stat(abs); statErr != nil {
			// 显式配了就别静默退回默认查找，否则排查时会找不到头绪
			return "", fmt.Errorf("signer: DOUYIN_SIGNER_SCRIPT points to a missing file: %s", abs)
		}
		c.script = abs
		return abs, nil
	}

	var candidates []string

	// 从工作目录向上找，覆盖 go run ./cmd/server 和编译后从 build/ 启动两种情况
	if wd, err := os.Getwd(); err == nil {
		dir := wd
		for depth := 0; depth < 4; depth++ {
			candidates = append(candidates, filepath.Join(dir, "signer", "signer.js"))
			parent := filepath.Dir(dir)
			if parent == dir {
				break
			}
			dir = parent
		}
	}

	if exe, err := os.Executable(); err == nil {
		dir := filepath.Dir(exe)
		for depth := 0; depth < 4; depth++ {
			candidates = append(candidates, filepath.Join(dir, "signer", "signer.js"))
			parent := filepath.Dir(dir)
			if parent == dir {
				break
			}
			dir = parent
		}
	}

	for _, candidate := range candidates {
		if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
			abs, absErr := filepath.Abs(candidate)
			if absErr != nil {
				abs = candidate
			}
			c.script = abs
			return abs, nil
		}
	}

	// 磁盘上没有源码，说明是发布态：用编译进二进制的打包版本
	embedded, err := embeddedScript()
	if err != nil {
		return "", fmt.Errorf("signer: signer.js not found on disk, and %w", err)
	}
	c.script = embedded
	return embedded, nil
}

func portFree(port int) bool {
	listener, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		return false
	}
	_ = listener.Close()
	return true
}

func truncate(body []byte, limit int) string {
	text := strings.TrimSpace(string(body))
	if len(text) <= limit {
		return text
	}
	return text[:limit] + "..."
}

// prefixWriter 把子进程输出按行加上前缀转给 logger。
type prefixWriter struct {
	prefix string
	logger *log.Logger
	buf    []byte
}

func (w *prefixWriter) Write(p []byte) (int, error) {
	w.buf = append(w.buf, p...)
	for {
		idx := bytes.IndexByte(w.buf, '\n')
		if idx < 0 {
			break
		}
		line := strings.TrimRight(string(w.buf[:idx]), "\r")
		if strings.TrimSpace(line) != "" {
			w.logger.Print(w.prefix + line)
		}
		w.buf = w.buf[idx+1:]
	}
	// 防止长时间无换行导致无限增长
	if len(w.buf) > 8<<10 {
		w.buf = w.buf[:0]
	}
	return len(p), nil
}
