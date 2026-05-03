# Sing-Matrix 产品设计

Sing-Matrix 面向熟悉 sing-box 的进阶用户和运维人员，目标是把复杂的配置 JSON 抽象为可视化拓扑。

## 模块

1. 仪表盘：系统负载、sing-box 状态、实时流量、活跃连接。
2. Traffic Studio：节点图编排入站、规则、出站，导出 sing-box config。
3. Protocol Forge：REALITY/Hysteria2 模板、端口冲突巡检、证书路径检查。
4. Rule-Set Matrix：SRS 订阅、本地规则编译、DNS 分流沙盒。
5. Audit：后续加入登录、操作审计、配置 diff 与回滚。

## 拓扑编排模型

画布中的节点分三类：

- Inbound：例如 `inbound-reality`、`inbound-hy2`
- Rule：例如 `rule-srs`、`rule-domain`、`rule-ip-cidr`
- Outbound：例如 `outbound-direct`、`outbound-selector`、`outbound-wireguard`

边表示流量路径，后端会把图结构转换为 sing-box 的 `inbounds`、`route.rules` 与 `outbounds`。
