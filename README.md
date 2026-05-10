# Sing-Matrix

Sing-Matrix（S-Matrix）是一个 Go-only 的 sing-box 可视化管理面板，用来在单机上创建、编译、运行和分享 sing-box 入站节点。它把常见代理节点的参数、订阅链接、二维码、运行状态和 sing-box Core 版本管理收进一个轻量 Web UI，不依赖 Node/Vite/Vue 构建链路。

项目目标很直接：让 sing-box 的日常运维从“手写 JSON + 手动重启 + 手动拼分享链接”变成“面板创建/编辑 + 自动编译配置 + 自动重启 + 直接复制订阅”。

## 当前能力

- 登录鉴权：默认账号密码 `admin / admin`，支持面板内修改密码。
- 节点管理：新增、编辑、启停、删除、搜索入站节点。
- 一键创建：一键 REALITY、一键 Hysteria2，自动选端口、生成密钥/密码、写配置并重启 sing-box。
- 多协议入站：REALITY/VLESS、Hysteria2、Hysteria v1、TUIC、Naive、ShadowTLS、AnyTLS、VMess、Trojan、Shadowsocks、SOCKS5、HTTP。
- 配置编译：把面板节点转换成 sing-box config，写入配置文件并重启 sing-box。
- 分享与订阅：为节点生成分享链接、二维码和订阅地址；提供发现接口方便外部订阅系统对接。
- 运行状态：显示 CPU、内存、sing-box 运行状态和当前 Core 版本。
- 日志终端：通过 WebSocket 实时查看 sing-box 日志。
- Core 管理：检测稳定版/Alpha 版 sing-box release，并支持切换到稳定版、Alpha 或指定版本。
- 纯 Go Web UI：HTML/CSS/JS 通过 Go embed 内置在后端里，部署时只需要一个二进制和 sing-box。

## 适合谁用

Sing-Matrix 适合：

- 已经在 VPS 上运行 sing-box，希望减少手写配置的人。
- 需要快速创建 REALITY/HY2 节点并生成订阅链接的人。
- 想把 sing-box 节点能力接入外部订阅系统、面板或自动化工具的人。
- 希望用 systemd 直接管理单机代理服务，而不是引入大型前后端栈的人。

它不是一个多租户商业面板，也不是传统 x-ui 的完整替代品。当前重点是单机 sing-box 编排、节点生成、订阅输出和运维可视化。

## 架构

- 后端：Go、Gin、SQLite、GORM、WebSocket。
- 前端：原生 HTML/CSS/JS，通过 Go embed 打包。
- Core：本机 `sing-box` 二进制。
- 运行方式：后端管理配置文件和 sing-box 进程，推荐由 systemd 托管。

主要目录：

- `s-matrix-backend/`：主后端、API、Web UI、sing-box 编译器。
- `s-matrix-backend/api/web/`：内置 Web UI 静态资源。
- `s-matrix-backend/core/singbox/`：配置生成、端口选择、进程管理。
- `scripts/`：发布构建、安装脚本、systemd 模板。
- `deploy/`：Docker、Nginx、systemd 示例。
- `docs/`：API、部署和产品设计文档。

## 快速开始

### 本地开发运行

需要 Go 1.21+ 和 sing-box。

1. 进入后端目录：
   `cd s-matrix-backend`
2. 下载依赖：
   `go mod tidy`
3. 启动：
   `go run ./cmd/server`
4. 打开：
   `http://127.0.0.1:19088`
5. 默认登录：
   `admin / admin`

默认启动参数：

- 监听地址：`127.0.0.1:19088`
- 数据库：`s-matrix.db`
- sing-box 配置：`./config.json`
- sing-box 日志：`./singbox.log`
- sing-box 二进制：`sing-box`

### 构建发布包

在仓库根目录执行：

`./scripts/build-release.sh`

构建产物会放到 `scripts/s-matrix`，可配合 `scripts/install.sh` 安装。

### 一键安装脚本

安装脚本要求 root 权限，会：

- 安装或检测 `/usr/local/bin/sing-box`。
- 安装 `s-matrix` 到 `/usr/local/bin/s-matrix`。
- 创建 `/etc/s-matrix/` 配置目录。
- 安装并启动 `s-matrix.service`。

执行：

`sudo ./scripts/install.sh`

安装后默认访问：

`http://服务器IP:19088`

默认登录：`admin / admin`。第一次登录后请立刻修改密码。

## 配置项

后端通过环境变量配置：

- `SMATRIX_ADDR`：后端监听地址，默认 `127.0.0.1:19088`。
- `SMATRIX_DB`：SQLite 数据库路径，默认 `s-matrix.db`。
- `SMATRIX_CONFIG`：sing-box 配置文件路径，默认 `./config.json`。
- `SMATRIX_SINGBOX_LOG`：sing-box 日志路径，默认 `./singbox.log`。
- `SMATRIX_SINGBOX_BIN`：sing-box 二进制路径，默认 `sing-box`。

注意：部分旧部署模板里可能出现 `SMATRIX_SINGBOX_CONFIG` 或 `SMATRIX_GENERATED_CONFIG`，当前主程序实际读取的是 `SMATRIX_CONFIG`。如果你改 systemd，请以 `s-matrix-backend/cmd/server/main.go` 为准。

## 常用 API

基础路径：`/api/v1`

公开接口：

- `POST /login`：登录并获取 JWT。
- `POST /token`：登录兼容入口。
- `GET /auth/challenge`：登录挑战信息。
- `GET /sub/:token`：订阅输出。
- `GET /discover`：对接发现接口。

需要 JWT 的接口：

- `GET /system/status`：系统与 sing-box 状态。
- `GET /system/singbox/version`：检测 sing-box 版本。
- `POST /system/singbox/switch`：切换 sing-box Core 版本。
- `POST /system/change-password`：修改面板密码。
- `GET /system/gen-reality-keypair`：生成 REALITY keypair。
- `GET /inbounds`：列出节点。
- `POST /inbounds/:id/toggle`：启停节点。
- `PUT /inbounds/:id/rename`：重命名节点。
- `DELETE /inbounds/:id`：删除节点。
- `GET /inbounds/:id/links`：生成单节点链接。
- `POST /quick/reality`：一键创建 REALITY。
- `POST /quick/hy2`：一键创建 Hysteria2。
- `POST /singbox/compile`：编译并应用配置。
- `GET /singbox/share-links`：获取全部分享链接。
- `GET /logs/ws`：日志 WebSocket。

更多细节见 `docs/API.md`。

## 推荐生产部署

生产环境建议：

- `SMATRIX_ADDR` 监听 `127.0.0.1:19088`。
- 前面放 Nginx/Caddy，负责 HTTPS、访问控制和日志。
- 用 systemd 管理 `s-matrix`。
- sing-box 配置、数据库和日志放到专用目录，例如 `/etc/s-matrix/` 或 `/opt/sing-matrix/data/`。
- 面板不要裸奔公网；至少启用 HTTPS、强密码和防爆破策略。

Nginx 示例见：`deploy/nginx.example.conf`。

systemd 示例见：`scripts/s-matrix.service` 或 `deploy/sing-matrix.service`。

Docker Compose 示例见：`deploy/docker-compose.yml`。当前 Docker 更适合开发/演示；生产更推荐 systemd，因为需要更清晰地控制 sing-box 进程权限和配置文件权限。

## 安全提醒

Sing-Matrix 可以写 sing-box 配置、启动/停止 sing-box、切换 Core 版本，也能生成可连接节点的订阅链接。部署到公网前务必注意：

- 修改默认密码。
- 使用 HTTPS。
- 不要把数据库、配置、日志目录设成可公开访问。
- 反代层限制暴力登录和异常请求。
- 给 systemd 服务配置最小权限，不要无必要地用高权限暴露面板。
- 妥善保管订阅链接和 JWT；它们能暴露节点连接信息。

## 开发与测试

常用命令：

- 后端测试：`cd s-matrix-backend && go test ./...`
- 本地运行：`cd s-matrix-backend && go run ./cmd/server`
- 构建二进制：`go build -o scripts/s-matrix ./s-matrix-backend/cmd/server`

## Roadmap

- 更完整的拓扑编排画布。
- 配置 diff、回滚和操作审计。
- 更细的权限模型和登录安全策略。
- 更完整的订阅格式适配。
- sing-box metrics / Clash API / 日志解析的实时流量面板。
- 多实例或多机器管理能力。

## License

MIT
