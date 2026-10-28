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

首次运行时，二进制会把内嵌的签名器释放到系统缓存目录（macOS 是 `~/Library/Caches/douyin-downloader/signer-<指纹>/`，Linux 是 `~/.cache/douyin-downloader/...`），之后直接复用。目录名带内容指纹，换版本会落到新目录。

唯一的外部依赖是 **Node.js 20+**，在 PATH 里能找到即可。

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

### 排查

- `signer: node not found in PATH`：机器上没装 Node，或没进 PATH
- `signer: embedded bundle missing`：构建时没生成 `internal/signer/assets/`，回到 `signer/` 目录执行 `npm run build`
- 日志里带 `[signer:out]` / `[signer:err]` 前缀的行是签名器的输出，它启动失败的原因会直接打在这里

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
