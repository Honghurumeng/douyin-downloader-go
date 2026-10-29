# 抖音视频下载与标注服务

一个基于 Go 的抖音视频下载与管理服务。  
前端使用 React + shadcn/ui，后端主体是 Go，负责解析分享链接、下载视频与封面，并用 SQLite 持久化保存元数据、评分、标签和本地文件信息；另有一个小的 Node 签名器负责计算接口签名。

## 功能概览

- 解析抖音分享文案或分享短链（走 web 接口，签名由本地 sidecar 计算）
- 下载视频到本地，并保存封面到本地
- 使用 SQLite 持久化保存视频元数据
- 视频列表展示、播放、删除
- 1 到 5 分评分
- 标签创建、删除
- 一个视频可绑定多个标签
- 按评分筛选
- 按多个标签交集筛选
- 支持服务端密码登录，未校验前不会返回视频页数据
- 登录失败次数过多时会临时限流
- 前端打包并嵌入到单个二进制中
- 支持部署在根路径 `/` 或子路径，例如 `/douyin/`

## 技术栈

- 后端：Go
- 前端：React + Vite + Tailwind CSS + shadcn/ui
- 存储：SQLite
- 前端嵌入：`go:embed`

## 数据存储位置

运行时数据默认保存在当前二进制所在目录下的 `data/` 目录中：

- `data/videos/`：下载的视频文件
- `data/covers/`：本地封面文件
- `data/videos.db`：SQLite 数据库

说明：

- 数据目录跟随二进制文件，不跟随当前 shell 工作目录
- 如果迁移部署目录，需要同时迁移 `data/` 目录

## 目录说明

```text
cmd/server/                HTTP 服务入口
internal/downloader/       作品详情解析与视频下载
internal/signer/           签名器的进程管理、HTTP 调用与内嵌资源
internal/signer/assets/    签名器打包产物（由 signer/build.js 生成）
signer/                    抖音 web 接口签名器源码（Node.js）
internal/store/            SQLite 存储层
frontend/                  React 前端
embedded_assets.go         前端打包产物嵌入
build/                     构建输出目录
data/                      运行时数据目录
```

## 本地构建

### 环境要求

- Go 1.26+
- Node.js 20+（运行期需要，签名器跑在 Node 上）
- npm（只在重新打包签名器时需要）

### 准备签名器

签名器的打包产物 `internal/signer/assets/` 已随仓库提供，直接构建 Go 即可，不需要 `npm install`。

只有在改动 `signer/signer.js` 或升级 jsdom 之后才需要重新打包：

```bash
cd signer
npm install      # 首次需要，装 jsdom 和 esbuild
npm run build    # 生成 ../internal/signer/assets/
cd ..
```

`go generate ./internal/signer` 等价的也是这条命令。

### 构建前端

```bash
cd frontend
npm install
npm run build
cd ..
```

### 构建当前平台二进制

```bash
go build -trimpath -ldflags='-s -w' -o build/douyin-server ./cmd/server
```

### 一次生成三个二进制文件

下面的命令会先确保 `build/` 目录存在，然后一次生成这三个目标：

- `build/douyin-server-darwin-arm64`
- `build/douyin-server-linux-amd64`
- `build/douyin-server-linux-arm64`

```bash
mkdir -p build

GOOS=darwin GOARCH=arm64 go build -trimpath -ldflags='-s -w' -o build/douyin-server-darwin-arm64 ./cmd/server
GOOS=linux GOARCH=amd64 go build -trimpath -ldflags='-s -w' -o build/douyin-server-linux-amd64 ./cmd/server
GOOS=linux GOARCH=arm64 go build -trimpath -ldflags='-s -w' -o build/douyin-server-linux-arm64 ./cmd/server
```

如果只想生成其中一个目标，也可以单独执行对应那一行命令。

## 签名器（sidecar）

抖音 web 接口要求请求携带 `x-secsdk-web-signature`，这个值只能由抖音自己的 secsdk 运行时在类浏览器的环境里实时算出，Go 侧无法复现。所以 `signer/` 下有一个 Node 程序专门负责这一件事。

工作方式：

- 第一次提交分享链接时，Go 自动拉起签名器（监听 `127.0.0.1:8099`）
- 签名器用 jsdom 构造 DOM 环境，加载抖音的 secsdk 运行时，为请求计算签名
- Go 拿到签名后的作品详情 JSON，视频、封面、图片仍由 Go 自己下载
- 空闲 5 分钟后签名器自行退出，不常驻占内存

耗时代价：首次调用约 1.2～1.5 秒（拉起进程 + 初始化运行时），之后每次 0.5～0.9 秒。

### 已经打包进二进制

签名器（jsdom + secsdk 运行时，共 5.7MB）在构建时打成 `internal/signer/assets/`，通过 `go:embed` 编译进二进制。**发布只需要一个文件**：目标机器不需要 `signer/` 目录，不需要 `node_modules`，也不需要 npm。

首次运行时，二进制会把内嵌的签名器释放到系统缓存目录（macOS 是 `~/Library/Caches/douyin-downloader/signer-<指纹>/`，Linux 是 `~/.cache/douyin-downloader/...`），之后直接复用。目录名带内容指纹：内容没变就命中缓存、不重解压；变了才落新目录，并顺带清掉旧目录（细节见「缓存与升级」）。

唯一的外部依赖是 **Node.js 20+**：放进 `PATH`、放在二进制同目录、或用 `DOUYIN_NODE_BIN` 指定绝对路径，三种都行。想连这一步都省掉，用 `-tags douyin_embed_runtime` 构建，见下面「单文件发布」。

脚本查找顺序：

1. 环境变量 `DOUYIN_SIGNER_SCRIPT`（指定后不再回退）
2. 工作目录及其上层的 `signer/signer.js`
3. 二进制所在目录及其上层的 `signer/signer.js`
4. 内嵌的打包版本

也就是说，开发时改 `signer/signer.js` 立即生效；发布后没有源码目录，自动用内嵌版。

### 关于 NODE_OPTIONS

某些宿主环境（IDE 插件、CI、runtime manager）会往 `NODE_OPTIONS` 里注入 `--require` 钩子，其中一些会包装 `fs.readFileSync`，让每次文件读取都多花几十毫秒。jsdom 冷启动要读九百多个模块，这个开销会被放大成二十多秒。

两道保险：

1. 启动签名器时从环境里剔除 `NODE_OPTIONS`，钩子根本不加载
2. 打包成单文件后，模块读取次数从上千次降到一次

实测：

| 组合 | 冷启动 | 取详情 |
| --- | --- | --- |
| 打包版 + 有 `--require` 钩子 | 2.27 s | ok |
| 打包版，无钩子 | 1.99 s | ok |
| 源码 + 有 `--require` 钩子 | 25.95 s | ok |
| 源码，无钩子 | 2.19 s | ok |

### 不想装 Node：让 node 跟着发布包走

签名器本身已经在二进制里，唯一要解决的是"谁来执行这份 JS"。可以把官方的 node 可执行文件
直接放在 `douyin-server` 旁边（或 `runtime/node`），程序启动时自己会找到它——不用安装、不用改
PATH、systemd 里也不用配环境变量：

```bash
# 只取 tar 包里的 bin/node，落到 ./runtime/node（x64 约 110MB，压缩包 31MB）
mkdir -p runtime
curl -fsSL https://nodejs.org/dist/v22.23.4/node-v22.23.4-linux-x64.tar.xz \
  | tar -xJ -C ./runtime --strip-components=2 node-v22.23.4-linux-x64/bin/node

# ARM 服务器把上面两处 linux-x64 换成 linux-arm64

./douyin-server-linux-amd64        # 启动日志里会显示用了哪个 node
```

查找顺序：

1. `DOUYIN_NODE_BIN` 环境变量（显式指定，优先级最高）
2. 二进制所在目录 / 当前目录下的 `node`、`runtime/node`、`runtime/bin/node`
3. `PATH` 里的 `node`

用 nvm / fnm 装的 node 通常只出现在交互式 shell 的 PATH 里，服务被 systemd、supervisor 或
`nohup` 拉起时 PATH 很干净，这时第 1、2 条都能绕过问题：

```bash
which node      # 例如 /root/.nvm/versions/node/v22.13.0/bin/node
DOUYIN_NODE_BIN=/root/.nvm/versions/node/v22.13.0/bin/node PORT=8080 ./douyin-server-linux-amd64
```

### 单文件发布（自带 node 运行时）

上面两条路都还要求目标机器有个 node。如果就要一个文件、机器上什么都不装，可以把 node
一起编进二进制：xz 压缩后 22～25MB，产物 42～44MB。

```bash
scripts/prepare-runtime.sh        # 生成 internal/signer/runtime/<platform>/node.xz

GOOS=linux GOARCH=amd64 go build -tags douyin_embed_runtime \
  -trimpath -ldflags='-s -w' -o build/douyin-server-linux-amd64 ./cmd/server
```

| 构建 | 产物 | 目标机器 |
| --- | --- | --- |
| 默认（`go build`） | **17MB** | 需要 node |
| `-tags douyin_embed_runtime` | **42～44MB** | 什么都不需要 |

首次启动会把内置的 node 解压到缓存目录（Linux `~/.cache/douyin-downloader/node-<指纹>/`，
约 108MB，实测 2～3 秒），之后直接复用。启动日志会写明用的是哪一个：

```text
signer: node v22.23.4 就绪（内置运行时: /root/.cache/douyin-downloader/node-c7350f9a45b604ab/node）
```

此时查找顺序变成：`DOUYIN_NODE_BIN` → 二进制同目录 → **内置运行时** → `PATH`。
内置排在 PATH 前面是有意的：它是随二进制发布的确定版本，而系统里可能躺着 node 12。

#### 缓存与升级

两类产物都释放到缓存目录，目录名带**内容指纹**（sha256 前 8 字节）：

| 目录 | 内容 | 大小 | 指纹来源 |
| --- | --- | --- | --- |
| `node-<16hex>/node` | 解压后的 node | 约 112MB | 压缩包字节 |
| `signer-<16hex>/` | 签名器脚本 + secsdk | 约 5.7MB | `signer.bundle.js` 字节 |

于是升级时的行为是可预期的：

- **换程序版本、但没换 node 也没换签名器** → 指纹不变，命中缓存，**不解压**，启动日志里不会出现「已释放」
- **换了 node 版本**（比如 22.23.4 → 22.24.x）→ 新指纹，落到新目录，重新解压一次；旧目录在同一次启动里被清理
- **签名器 bundle 变了**（抖风控更新）→ 同上，只是 5.7MB
- **回滚到旧版本** → 旧目录如果还没被清掉就直接复用；已清掉就重解压一次，不影响正确性

清理只在「确实解压了新东西」之后做，稳态启动零扫描。规则是：同类型（`node-` / `signer-`）
且不是当前指纹、mtime 超过 10 分钟的目录会被删除，包括进程被强杀留下的 `*.tmp-*`。
10 分钟宽限期是给滚动重启留的——新旧两个进程可能同时在跑，不能删掉对方正在用的文件。

```text
[signer] 已释放内置 node 运行时到 /root/.cache/douyin-downloader/node-<新指纹>
[signer] 清理旧缓存 node-<旧指纹>（释放 112.0MB）
```

所以磁盘占用是有界的：稳态只有一份（112MB + 5.7MB），升级瞬间最多两份，随后回到一份。
指纹只认内容，不认版本号：同一份 node 重新构建的二进制不会因为版本号变了而重解压。

`DOUYIN_CACHE_DIR` 可以把缓存放到别处（容器里挂数据卷，避免每次重启都往 `/tmp` 里解压 112MB）：

```bash
DOUYIN_CACHE_DIR=/data/douyin-cache ./douyin-server-linux-amd64
```

`scripts/prepare-runtime.sh` 只做三件事：下载官方 node、linux 目标用 docker 里的 `strip`
瘦身（macOS 的 strip 只认 Mach-O）、`xz -9` 压缩。产物不进 git。

两个部署注意点：

- **ca-certificates**：node 自带根证书，Go 用系统证书池。`slim` / `alpine` / `distroless`
  这类极简镜像如果没装 CA，会出现「解析成功、下载失败」，报错是
  `x509: certificate signed by unknown authority`
- **缓存目录**：`HOME` 没有写权限（systemd 服务常见）时会退到 `/tmp`，重启后重新解压，
  不影响功能；要固定位置就设 `DOUYIN_CACHE_DIR`

### 为什么不是 Bun

Bun 1.3.11 实测跑不起来，两个卡点都在 jsdom + secsdk 这一层：

- 直接跑 bundle：`worker_threads.markAsUncloneable` 未实现，undici 初始化即崩（Bun 的 `node:util` 也没有这个 API）
- 打补丁绕过之后：卡在 secsdk 初始化，`addEventListener` 抛 `'addEventListener' called on an object that is not a valid instance of EventTarget`
- `bun build --compile` 出的单文件同样崩在第一处，而且 vendor 下的四个 secsdk 脚本是运行时 `readFileSync(__dirname/vendor/...)` 读的，编译进虚拟文件系统后路径不存在
- 就算能跑，secsdk 是抖音持续更新的风控代码，靠逐个打补丁维持兼容，随时会被下一次更新打回原形

体积上也不划算（这份 JS 一共 5.7MB）：

| 方案 | 产物 | 目标机器还需要 |
| --- | --- | --- |
| 当前 | Go 二进制 **17MB** | node（装、或放同目录） |
| Bun `--compile` 打包签名器 | **68MB**（hello world 起步就 61MB） | 无 |
| Node SEA 单文件 | 约 **110MB** | 无 |

也就是说：把运行时塞进单文件，代价是 4～6 倍体积；当前 17MB + 一个 node 是最省的组合。

### 排查

服务启动时会打印一行签名器状态，先看这里：

```text
signer: node v22.22.2 (/usr/bin/node) 就绪          # 正常
解析功能不可用: signer: node not found in PATH (...)    # 缺 node，解析一定失败
```

- `解析功能不可用: signer: node not found in PATH`：机器上没找到 Node。装 Node 20+、把 node 放到二进制同目录、或设置 `DOUYIN_NODE_BIN` 三者任选其一。**这种情况下日志里不会出现任何 `[signer]` 行**，因为进程根本没被拉起来；失败原因只会出现在 `POST /api/videos` 那一行的响应体里
- `start sidecar failed: ...`：node 找到了但拉起失败，原因跟在后面（脚本缺失、版本过低等）
- `[signer:out]` / `[signer:err]` 前缀的行是签名器自身的输出
- `POST /api/videos 502`：解析阶段失败，紧挨着上面的 `extract share link failed:` 行就是原因
- `signer: embedded bundle missing`：构建时没生成 `internal/signer/assets/`，回到 `signer/` 目录执行 `npm run build`

调试时可以手动起一个，Go 会直接复用它：

```bash
cd signer
node signer.js --serve 8099
```

## 启动方式

### 根路径部署

服务直接挂在域名根路径时：

```bash
LOGIN_PASSWORD='your-password' PORT=8080 ./build/douyin-server
```

或 Linux 服务器上：

```bash
LOGIN_PASSWORD='your-password' PORT=8080 ./douyin-server-linux-amd64
```

### 子路径部署

如果服务需要部署在 `https://example.com/douyin/` 下：

```bash
BASE_PATH=/douyin LOGIN_PASSWORD='your-password' PORT=8090 ./douyin-server-linux-amd64
```

后台运行示例：

```bash
nohup env BASE_PATH=/douyin LOGIN_PASSWORD='your-password' PORT=8090 ./douyin-server-linux-amd64 > douyin-server.log 2>&1 &
```

## 启动参数

| 参数 | 默认值 | 说明 |
| --- | --- | --- |
| `PORT` | `8080` | HTTP 服务监听端口 |
| `BASE_PATH` | 空 | 部署路径前缀。为空表示部署在根路径；设置为 `/douyin` 表示部署在 `/douyin/` 下 |
| `LOGIN_PASSWORD` | 空 | 访问密码。设置后，前端会先显示密码登录页；只有服务端校验成功后，才允许加载视频、标签和本地媒体资源 |
| `DOUYIN_NODE_BIN` | 空 | node 可执行文件的绝对路径。不设置时会先找二进制同目录的 `node` / `runtime/node`，再找内置运行时，最后找 PATH |
| `DOUYIN_CACHE_DIR` | 空 | 缓存目录（存放解压后的 node 与签名器脚本）。默认 `<系统缓存目录>/douyin-downloader`，容器里建议指向数据卷 |

参数说明：

- `BASE_PATH` 建议写成 `/douyin`
- 不要写成 `/douyin/`
- 不设置 `BASE_PATH` 时，所有接口和静态资源默认从根路径提供
- 设置 `LOGIN_PASSWORD` 后，服务端会通过 Cookie 维护登录态
- 设置 `LOGIN_PASSWORD` 后，访问根路径 `/` 会按登录状态重定向到 `/login/` 或 `/app/`
- 未登录直接访问 `/app/` 时，服务端会重定向到 `/login/`
- 同一来源短时间内密码错误过多时，登录接口会返回 `429 Too Many Requests`
- 如果不设置 `LOGIN_PASSWORD`，服务保持无密码直入模式，兼容原来的部署方式

## 反向代理示例

下面是部署到 `https://example.com/douyin/` 的 Nginx 示例：

```nginx
location = /douyin {
    return 302 /douyin/;
}

location /douyin/ {
    proxy_pass http://127.0.0.1:8090;

    proxy_set_header Host $host;
    proxy_set_header X-Real-IP $remote_addr;
    proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
    proxy_set_header X-Forwarded-Proto $scheme;

    proxy_http_version 1.1;
    proxy_connect_timeout 60s;
    proxy_send_timeout 60s;
    proxy_read_timeout 60s;
}
```

注意：

- `proxy_pass` 后面不要带结尾的 `/`
- 如果写成 `proxy_pass http://127.0.0.1:8090/;`，Nginx 会错误剥离 `/douyin/` 前缀

## 常用接口

| 方法 | 路径 | 说明 |
| --- | --- | --- |
| `GET` | `/api/health` | 健康检查 |
| `GET` | `/api/auth/session` | 获取当前登录状态 |
| `POST` | `/api/auth/login` | 提交密码并建立登录态 |
| `POST` | `/api/auth/logout` | 清除登录态 |
| `GET` | `/api/videos` | 获取视频列表 |
| `POST` | `/api/videos` | 提交分享文案并下载视频 |
| `PATCH` | `/api/videos/:id/rating` | 更新评分 |
| `PATCH` | `/api/videos/:id/tags` | 更新视频标签 |
| `DELETE` | `/api/videos/:id` | 删除视频、封面和数据库记录 |
| `GET` | `/api/tags` | 获取标签列表 |
| `POST` | `/api/tags` | 创建标签 |
| `DELETE` | `/api/tags/:id` | 删除标签 |

当使用 `BASE_PATH=/douyin` 时，实际访问路径会自动变成：

- `/douyin/login/`
- `/douyin/app/`
- `/douyin/api/health`
- `/douyin/api/videos`
- `/douyin/api/tags`

## 使用示例

### 健康检查

根路径部署：

```bash
curl -i http://127.0.0.1:8080/api/health
```

子路径部署：

```bash
curl -i http://127.0.0.1:8090/douyin/api/health
```

### 提交抖音分享文案

根路径部署：

```bash
curl -X POST http://127.0.0.1:8080/api/videos \
  -H 'Content-Type: application/json' \
  --data '{"shareText":"粘贴抖音分享文案或分享链接"}'
```

子路径部署：

```bash
curl -X POST http://127.0.0.1:8090/douyin/api/videos \
  -H 'Content-Type: application/json' \
  --data '{"shareText":"粘贴抖音分享文案或分享链接"}'
```

## 前端开发

如果只是开发前端页面：

```bash
cd frontend
npm install
npm run dev
```

如果需要联调后端，另开一个终端启动后端服务即可。

## 运行与排查

查看服务日志：

```bash
tail -n 100 douyin-server.log
```

查看进程：

```bash
ps -ef | grep douyin-server
```

检查端口：

```bash
ss -lntp | grep ':8080\|:8090'
```

## 当前实现说明

- 支持视频与图文（图集）作品
- 数据来源是 `/aweme/v1/web/aweme/detail/` 接口，不是分享页 HTML：分享页在触发风控时会返回验证码中间页，接口只要签名正确就能稳定拿到结构化的 `aweme_detail`
- 接口签名由 `signer/` 下的 Node sidecar 计算，Go 负责进程管理、字段映射和文件下载
- 视频优先取 `play_addr`。`download_addr` 是「保存到相册」版本，末尾会拼三秒左右的水印片尾，实测 5.08 秒的作品从它下下来是 8.10 秒
- 画质从 `bit_rate` 多档位里自己挑：分辨率最高、同分辨率取体积大的，并跳过 `is_h265` / `is_bytevc1` / HDR 档位。接口即使在请求里带 `support_h265=0` 也照样返回 HEVC 档，而那一档往往体积更大，直接取会用上浏览器播不了的编码
- 元数据的宽高取自实际选中的档位，不会出现「写 720p、文件是 540p」
- 档位集合由服务端按请求分配，同一作品不同时刻可能只给到 540p（实测三次里有一次没有 H.264 的 720p 档），这是接口侧行为，不是选档逻辑的问题
- 前端列表封面使用原生懒加载
- 视频播放区域按原始比例展示，不强制裁切
