# API Draft

Base URL: `/api/v1`

## GET /system/status

返回系统负载、内存、运行时长和 sing-box 进程状态。

## GET /singbox/config

读取当前运行配置。默认路径由 `SMATRIX_SINGBOX_CONFIG` 指定。

## POST /singbox/compile

接收拓扑图 JSON，生成 sing-box config，并写入 `SMATRIX_GENERATED_CONFIG`。

请求体：

- `nodes[]`: `{ id, kind, label, position, data }`
- `edges[]`: `{ id, source, target }`

## POST /singbox/reload

当前第一版会执行 `sing-box check -c <generated-config>` 做配置校验。后续可接入 sing-box 热重载 API 或 systemd reload。

## POST /inbounds/reality

生成 REALITY short_id；如果本机已安装 sing-box，则调用 `sing-box generate reality-keypair` 获取真实 x25519 信息。

## GET /traffic/ws

WebSocket 流量推送。第一版为模拟数据，后续接 sing-box metrics/clash API 或日志解析。
