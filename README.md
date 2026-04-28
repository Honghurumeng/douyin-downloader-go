# 抖音视频下载与标注服务

一个基于 Go 的抖音视频下载与管理服务。  
前端使用 React + shadcn/ui，后端使用纯 Go 解析抖音分享链接、下载视频与封面，并使用 SQLite 持久化保存元数据、评分、标签和本地文件信息。

## 功能概览

- 纯 Go 解析抖音分享文案或分享短链
- 下载视频到本地，并保存封面到本地
- 使用 SQLite 持久化保存视频元数据
- 视频列表展示、播放、删除
- 1 到 5 分评分
- 标签创建、删除
- 一个视频可绑定多个标签
- 按评分筛选
- 按多个标签交集筛选
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
internal/downloader/       抖音页面解析与视频下载
internal/store/            SQLite 存储层
frontend/                  React 前端
embedded_assets.go         前端打包产物嵌入
build/                     构建输出目录
data/                      运行时数据目录
```

## 本地构建

### 环境要求

- Go 1.26+
- Node.js 20+
- npm

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

### 构建 Linux 版本

Linux AMD64：

```bash
GOOS=linux GOARCH=amd64 go build -trimpath -ldflags='-s -w' -o build/douyin-server-linux-amd64 ./cmd/server
```

Linux ARM64：

```bash
GOOS=linux GOARCH=arm64 go build -trimpath -ldflags='-s -w' -o build/douyin-server-linux-arm64 ./cmd/server
```

## 启动方式

### 根路径部署

服务直接挂在域名根路径时：

```bash
PORT=8080 ./build/douyin-server
```

或 Linux 服务器上：

```bash
PORT=8080 ./douyin-server-linux-amd64
```

### 子路径部署

如果服务需要部署在 `https://example.com/douyin/` 下：

```bash
BASE_PATH=/douyin PORT=8090 ./douyin-server-linux-amd64
```

后台运行示例：

```bash
nohup env BASE_PATH=/douyin PORT=8090 ./douyin-server-linux-amd64 > douyin-server.log 2>&1 &
```

## 启动参数

| 参数 | 默认值 | 说明 |
| --- | --- | --- |
| `PORT` | `8080` | HTTP 服务监听端口 |
| `BASE_PATH` | 空 | 部署路径前缀。为空表示部署在根路径；设置为 `/douyin` 表示部署在 `/douyin/` 下 |

参数说明：

- `BASE_PATH` 建议写成 `/douyin`
- 不要写成 `/douyin/`
- 不设置 `BASE_PATH` 时，所有接口和静态资源默认从根路径提供

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
| `GET` | `/api/videos` | 获取视频列表 |
| `POST` | `/api/videos` | 提交分享文案并下载视频 |
| `PATCH` | `/api/videos/:id/rating` | 更新评分 |
| `PATCH` | `/api/videos/:id/tags` | 更新视频标签 |
| `DELETE` | `/api/videos/:id` | 删除视频、封面和数据库记录 |
| `GET` | `/api/tags` | 获取标签列表 |
| `POST` | `/api/tags` | 创建标签 |
| `DELETE` | `/api/tags/:id` | 删除标签 |

当使用 `BASE_PATH=/douyin` 时，实际访问路径会自动变成：

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

- 仅支持抖音视频类型内容，不支持图文合集等其它内容类型
- 分享链接解析、页面抓取与视频下载均由 Go 实现，不依赖 Node.js 运行时
- 前端列表封面使用原生懒加载
- 视频播放区域按原始比例展示，不强制裁切
