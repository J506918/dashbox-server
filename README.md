# DashBox Server

Go 后端服务，部署在阿里云 ECS `8.136.28.140:8443`（HTTPS/WSS），systemd 托管。

## 架构

```
cmd/main.go              → 入口：初始化 DB → 路由 → 启动 HTTPS
internal/
  api/
    router.go            → Gin 路由注册（REST + WebSocket）
    handlers.go          → REST API handler（设备 CRUD、参数、备份、路线）
    ws_handler.go        → WebSocket handler（设备 WS + App WS + RPC dispatch）
    auth.go              → QQ OAuth 登录
    pair.go              → 配对码生成/验证/绑定
  db/
    db.go                → PostgreSQL 操作（GORM, 设备/参数/用户/备份/路线）
  models/
    models.go            → 数据模型定义
  ws/
    hub.go               → WebSocket Hub（设备连接管理、App 连接管理、消息转发）
```

## 文件说明

| 文件 | 行数 | 职责 |
|------|------|------|
| `cmd/main.go` | ~60 | 入口：连 PostgreSQL，初始化 Hub，注册路由，启动 HTTPS |
| `api/router.go` | ~60 | 路由表：REST API + `/ws`(设备) + `/ws/app`(App) |
| `api/ws_handler.go` | ~280 | WS 核心：设备 auto-register、register_device 下发、ping/pong 心跳、RPC 方法路由 |
| `api/handlers.go` | ~300 | REST handler：设备 CRUD、参数转发、RPC 代理 |
| `api/auth.go` | ~190 | QQ 登录：OAuth state → token → JWT |
| `api/pair.go` | ~60 | 配对码：生成(15min TTL)、验证、绑定设备到用户 |
| `db/db.go` | ~350 | 所有数据库操作：设备/参数/用户/备份/路线 |
| `models/models.go` | ~80 | GORM 模型定义：Device/User/ParamHistory/UploadURL/DriveRoute/DeviceBackup |
| `ws/hub.go` | ~130 | WebSocket Hub：Register/Unregister/SendRPC/NotifyApp |
| `go.mod` | - | 依赖：gin, gorm, gorilla/websocket, lib/pq |
| `Dockerfile` | - | 构建用（生产用 systemd 直跑，非 Docker） |

## 关键流程

### 设备注册
```
设备 WS 连接（serial + dongle_id）
  → serial 不存在 DB → auto-register（CreateDevice）
  → dongle_id 不匹配 → 下发 register_device 消息
  → 设备收到后清空旧 ID → 写新 ID → 关连接重连
  → 重连时 dongle_id 匹配 → 正常在线
```

### 心跳
- 服务端每 5s 发 ping，20s 无 pong 视为离线
- pong 到达时刷新 DB last_seen + online=true
- 设备断连时 online=false

### 参数同步
- 设备上线推送 params_sync → 服务器转发给 App（params_update）
- App 改参数 → HTTP POST → 服务器通过 WS RPC 发给设备 saveParams
- 设备写参数文件 → 通过 Unix socket 通知 UI 刷新

## 部署

```bash
# 构建
go build -o dashbox-server ./cmd/

# 部署到服务器
scp dashbox-server root@8.136.28.140:/opt/dashbox/
ssh root@8.136.28.140 "systemctl restart dashbox-server"

# 查看日志
ssh root@8.136.28.140 "journalctl -u dashbox-server -f"
```

## 数据库

PostgreSQL 14，直装（非 Docker）。凭证：`dashbox:dashbox@localhost:5432/dashbox`

```bash
# 查看设备
sudo -u postgres psql -d dashbox -c "SELECT id, serial, device_id, online FROM devices;"

# 删除设备（触发重新注册）
sudo -u postgres psql -d dashbox -c "DELETE FROM devices WHERE serial='xxx';"
```
