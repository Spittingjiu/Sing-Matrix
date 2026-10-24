# S-Matrix

> 一个二进制、一个面板，把 sing-box 从「手写 JSON + 手动重启 + 手动拼链接」变成点几下就完事。

S-Matrix（Sing-Matrix）是 sing-box 的单机可视化管理面板：Go 后端 + 内嵌前端，无 Node、无数据库服务、无外部依赖。创建节点、签发证书、生成订阅、查看日志、切换内核版本，全部在一个页面里完成。

```
浏览器 ──► S-Matrix (Go, 内嵌 UI) ──► sing-box core ──► 节点端口
                    │
                    └── Let's Encrypt (HTTP-01) ──► 节点证书
```

---

## 一键安装

```bash
curl -fsSL https://raw.githubusercontent.com/Spittingjiu/Sing-Matrix/main/install.sh | sudo bash
```

装完会直接打印访问地址和随机生成的面板密码。脚本自动完成：

- 安装依赖、按架构下载 S-Matrix 二进制
- 若没有 sing-box，自动安装最新稳定版 core
- 生成面板账号（随机强密码）与 JWT 密钥
- 写好 systemd 服务并启动，数据放在 `/opt/sing-matrix/data`

### 常用参数

```bash
# 换端口 / 换安装目录
curl -fsSL .../install.sh | sudo bash -s -- --port 8080 --dir /srv/smatrix

# 前面有 nginx/Caddy：只监听 127.0.0.1，并打印反代片段
curl -fsSL .../install.sh | sudo bash -s -- --behind-proxy

# 只要面板，不用它管 sing-box
curl -fsSL .../install.sh | sudo bash -s -- --no-singbox

# 升级 / 卸载 / 查看状态
curl -fsSL .../install.sh | sudo bash -s -- --upgrade
curl -fsSL .../install.sh | sudo bash -s -- --uninstall          # 保留数据
curl -fsSL .../install.sh | sudo bash -s -- --uninstall --purge  # 连数据一起删
curl -fsSL .../install.sh | sudo bash -s -- --status
```

参数也可以用环境变量传：`SMATRIX_PORT`、`SMATRIX_LISTEN`、`SMATRIX_INSTALL_DIR`、`SMATRIX_VERSION`、`SMATRIX_WITH_SINGBOX`、`SMATRIX_SYSTEMD`、`SMATRIX_WEBROOT`。

### 装完之后：`sm` 管理菜单

安装脚本会装一个 `sm` 命令，不带参数就是交互菜单：

```bash
sm
```

```
S-Matrix 管理
  1) 查看当前状态 / 账号密码
  2) 修改用户名
  3) 修改密码
  4) 修改面板端口
  5) 重启面板服务
  6) 查看面板日志
  7) 查看节点实时日志
  8) 升级到最新版本
  9) 卸载
  0) 退出
```

也可以直接下命令，适合脚本化：

```bash
sm --show-pass              # 打印当前用户名和密码（忘记密码时用）
sm --set-user ops           # 改用户名
sm --set-pass 'NewPass123'  # 改密码（留空则自动生成 20 位）
```

改完账号密码会自动重启服务。如果一台机器上装了多个面板，`sm` 只会操作自己那份安装对应的服务，不会误重启别的。

### 手动安装

```bash
# 从 release 取二进制
curl -fsSLO https://github.com/Spittingjiu/Sing-Matrix/releases/latest/download/smatrix-linux-amd64
install -m755 smatrix-linux-amd64 /usr/local/bin/smatrix
smatrix --version
```

```bash
# 或者从源码构建（需要 Go 1.21+，CGO 用于 SQLite）
cd s-matrix-backend && go build -o /usr/local/bin/smatrix ./cmd/server
```

---

## 核心特性

### 一个节点出错，不会拖垮整台机器

配置在替换前先跑 `sing-box check`：校验不通过就拒绝写入，正在运行的 core 保持不动。

早期版本有个真实事故：一键 Hysteria2 生成的节点没带证书，`sing-box` 直接报 `TLS required` 拒绝启动 —— 结果新建一个坏节点，把整台上所有正常节点一起带下线。现在：

- Hysteria2 / AnyTLS / TUIC / Naive 一律强制带证书，缺证书在生成阶段就报错
- 写配置前先 `check`，失败则保留旧配置与运行中的进程
- 启动时若发现磁盘上的配置被新版本 core 拒绝，会自动按数据库重建并修复

### 一键建节点

「新建节点」菜单里选一个，填个备注就完事：

| 入口 | 生成内容 |
|---|---|
| **AnyTLS · 域名证书** | 校验域名 → 签发 Let's Encrypt → 建 AnyTLS 节点（链接不带 `insecure`） |
| **AnyTLS · 自签证书** | 自动生成 ECDSA 自签证书 → 建节点（链接带 `insecure=1`） |
| **REALITY** | 自动生成密钥对 / shortId，伪装 `www.microsoft.com` |
| **Hysteria2** | 自动生成密码，带 masquerade 伪装 |
| **自定义参数** | 12 种协议全参数表单（VLESS / VMess / Trojan / SS / TUIC / Naive / ShadowTLS / AnyTLS / HY2 / Hysteria / SOCKS / HTTP） |

### 自带域名 = 自带证书

面板**不保存任何 DNS 厂商的凭据**，也不需要你提供 API Token。流程是标准的 HTTP-01：

1. 用户把自己的域名 A 记录指向服务器（DNS-only，别开 CDN 代理）
2. 面板里点「校验域名」→ 解析结果与本机地址比对
3. 匹配则签发 Let's Encrypt 证书，证书落盘后可被任意 TLS 协议复用

校验失败会直接说清楚差在哪：

```
域名解析到 172.66.147.243, 104.20.23.154
与本机地址 23.80.81.106 不一致；请先把 A 记录指向本机（并关闭 CDN 代理）
```

端口 80 空闲时面板自己绑定应答（standalone）；80 被 nginx/Caddy 占用时走 webroot，由前置代理提供 `/.well-known/acme-challenge/`。证书每 12 小时自检，30 天内到期自动续签。

### 面板自托管 HTTPS（可选）

独立部署不想装反代时，直接让面板自己收发 HTTPS：

```bash
SMATRIX_HTTPS_ADDR=:443   # 用已签发的证书按 SNI 提供服务
SMATRIX_HTTP_ADDR=:80     # 应答 ACME 校验，其余 301 跳 HTTPS
```

签发成功后前端会自动跳到 `https://域名`。

### 分享与订阅

- 每个节点一键出分享链接 + 二维码（二维码本地渲染，不经过任何第三方服务）
- 全局订阅地址 `GET /api/v1/sub/:token`，Base64 明文列表
- 发现接口 `/api/v1/discover` 方便外部订阅系统对接

### 运维

- **运行状态**：sing-box 状态、CPU、内存、内核版本，3 秒刷新
- **实时日志**：WebSocket 终端，按 ERROR/WARN/INFO 着色
- **内核管理**：检测最新稳定版 / Alpha，一键切换版本
- **一键对接 subgo**：把面板注册成下游订阅源（自动带地址与永久 Token，支持握手签名）

---

## 支持矩阵

| 能力 | 说明 |
|---|---|
| 系统 | Linux（Debian / Ubuntu / Alpine / RHEL 系），amd64 / arm64 |
| sing-box | 1.10+（安装脚本默认拉最新稳定版） |
| 浏览器 | 现代浏览器；移动端自适应 |
| 数据库 | SQLite 单文件，无需额外服务 |
| 依赖 | 无。一个二进制 + sing-box |

---

## 部署形态

**单机直连（最省事）**

```
面板 :19088 ── sing-box 节点 :随机端口
```

**反代 + 真证书**

```
nginx/Caddy :443 ──► 面板 127.0.0.1:19088
                 └─► /.well-known/acme-challenge/ (webroot)
```

**面板自带 TLS**

```
面板 :443 (证书) + :80 (ACME/跳转)
```

---

## 配置项

全部通过环境变量，systemd unit 里已经写好：

| 变量 | 默认 | 说明 |
|---|---|---|
| `SMATRIX_ADDR` | `127.0.0.1:19088` | 面板监听地址 |
| `SMATRIX_DB` | `./s-matrix.db` | SQLite 路径 |
| `SMATRIX_CONFIG` | `./config.json` | 生成的 sing-box 配置 |
| `SMATRIX_DATA_DIR` | `./data` | 数据目录（账号、密钥、ACME 记录） |
| `SMATRIX_CERT_DIR` | `./certs` | 证书目录 |
| `SMATRIX_SINGBOX_BIN` | `sing-box` | core 路径 |
| `SMATRIX_SINGBOX_LOG` | `./singbox.log` | core 日志 |
| `SMATRIX_ACME_WEBROOT` | `./acme-webroot` | HTTP-01 webroot |
| `SMATRIX_ACME_EMAIL` | 空 | ACME 联系邮箱（可留空） |
| `SMATRIX_HTTPS_ADDR` | 空 | 设了才启用内置 HTTPS |
| `SMATRIX_HTTP_ADDR` | 空 | 内置 80 端口（ACME + 跳转） |
| `SMATRIX_JWT_SECRET` | 自动生成 | 登录令牌签名密钥 |

---

## API 速览

登录后 `Authorization: Bearer <token>`：

```
GET  /api/v1/system/status          # 运行状态
GET  /api/v1/inbounds               # 节点列表
POST /api/v1/inbounds/:id/toggle    # 启停
GET  /api/v1/inbounds/:id/links     # 分享链接
POST /api/v1/quick/anytls           # 一键 AnyTLS（自签）
POST /api/v1/quick/anytls-domain    # 一键 AnyTLS（域名证书）
POST /api/v1/quick/reality          # 一键 REALITY
POST /api/v1/quick/hy2              # 一键 Hysteria2
GET  /api/v1/domain/check?domain=   # 校验域名是否指向本机
POST /api/v1/domain/issue           # 签发证书
GET  /api/v1/domain/status          # ACME 状态（本机 IP / 校验方式）
POST /api/v1/panel/connect-sub      # 一键对接 subgo
```

完整接口见 [`docs/API.md`](docs/API.md)。

---

## AI 增强开发

项目大量使用 AI 辅助编码，但**每个改动都必须通过真实验证**：

- 认证集成测试（`go test ./...`）
- 真实机器上起服务，用 mihomo 客户端实际拨号验证节点可用
- 证书类改动必须真实签发并由客户端**严格校验**连通
- 前端改动在 headless 浏览器里做 DOM/布局断言，不只看「页面能打开」

不追求「看起来能跑」，只承认「跑通了」。

---

## 目录结构

```
s-matrix-backend/       Go 后端 + 内嵌前端
  api/                  路由与 HTTP handler
  core/singbox/         配置编译、端口选择、进程管理、自签证书
  core/acme/            HTTP-01 签发、证书续期、内置 TLS 监听
  api/web/              原生 HTML/CSS/JS（embed 进二进制）
install.sh              一键安装脚本
scripts/build-release.sh 构建 amd64/arm64 发布件
docs/                   API、部署、产品说明
```

---

## 许可

MIT
