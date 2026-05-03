# 部署说明

## 后端 systemd

参考：`deploy/sing-matrix.service`

建议：

- 后端只监听 `127.0.0.1`
- Nginx/Caddy 负责 HTTPS 与鉴权
- sing-box 配置目录使用独立低权限用户可写路径
- 对外开放前必须启用登录鉴权和操作审计

## Nginx

参考：`deploy/nginx.example.conf`

## Docker Compose

参考：`deploy/docker-compose.yml`

第一版 Docker 仅用于开发/演示；生产环境建议直接 systemd 管理，便于控制 sing-box 进程权限。
