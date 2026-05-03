# Sing-Matrix (S-Matrix)

基于 sing-box 的下一代可视化代理编排面板。S-Matrix 不是传统 x-ui 风格的表单列表，而是把 sing-box 的入站、规则集、出站抽象成可拖拽、可连线、可编译的流量拓扑图。

## 核心理念

- **Topology Orchestration**：用节点图表达流量走向，降低复杂 JSON 路由的维护成本。
- **Rule-Set First**：优先支持 sing-box SRS 规则集订阅、编译、分发，面向 sing-box 新路线设计。
- **Protocol Forge**：围绕 REALITY、Hysteria2 等高性能组合提供预设模板和端口冲突巡检。
- **Operational Dashboard**：通过 WebSocket 展示 sing-box 状态、系统资源、流量与活跃连接。

## 技术栈

- 后端：Go 1.21+、Gin、SQLite、WebSocket
- 前端：Vue 3、Vite、TypeScript、Naive UI、Tailwind CSS、Vue Flow、ECharts
- 目标运行：Linux 单节点面板，直接编排本机 sing-box 进程与配置

## 当前实现状态

这是项目第一版骨架，已经包含：

- Go 后端 API 服务
- SQLite 数据模型与初始化
- sing-box 配置生成器雏形
- REALITY 密钥与 short_id 生成接口
- 系统状态接口
- 拓扑图编译接口
- sing-box reload/restart 接口骨架
- WebSocket 流量推送接口
- Vue 3 前端工作台
- Vue Flow 拓扑画布
- Naive UI 暗黑风仪表盘
- API 客户端封装
- Dockerfile、docker-compose、systemd unit 示例

## 目录结构

- `backend/`：Go API 服务与 sing-box 编排逻辑
- `frontend/`：Vue 3 + Vite 前端
- `deploy/`：systemd、Nginx、Docker Compose 示例
- `docs/`：产品设计、API 与部署说明

## 快速开始

### 后端

进入 `backend` 后执行：

- `go mod tidy`
- `go run ./cmd/smatrix`

默认监听：`127.0.0.1:18088`

### 前端

进入 `frontend` 后执行：

- `npm install`
- `npm run dev`

默认开发入口：`http://127.0.0.1:5173`

## API 摘要

- `GET /api/v1/system/status`
- `GET /api/v1/singbox/config`
- `POST /api/v1/singbox/compile`
- `POST /api/v1/singbox/reload`
- `POST /api/v1/inbounds/reality`
- `GET /api/v1/traffic/ws`

详细文档见：`docs/API.md`

## 安全说明

S-Matrix 会读写 sing-box 配置并可重启 sing-box 进程，默认只建议监听 `127.0.0.1`，对公网开放前必须加 HTTPS、登录鉴权、防爆破、操作审计与最小权限运行。

## License

MIT
