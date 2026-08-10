# Komari Scheduler

自定义事件定时推送 Telegram 的小服务：在网页界面配置事件（推送内容 / 目标 chat_id / 各自的 Bot Token / 触发天数），服务自动计时推送，**无需 cron**。

## 功能

- 📅 支持自定义天数自动通知，无需 cron
- 🎯 多事件/多 chat_id 隔离
- 🖥️ Komari 风格前端界面
- 🐘 SQLite 本地持久化
- 🐳 支持 Docker Compose 一键部署
- ✏️ 支持编辑、启用/停用、删除事件
- 🖼️ 支持自定义背景图

## 快速开始

```bash
git clone https://github.com/nekomini88/komari-scheduler.git
cd komari-scheduler
docker compose up --build
```

访问 `http://localhost:9006` 即可配置事件。

> 无需 `.env` 文件：Telegram Bot Token 在界面按事件单独填写，每个事件可使用不同的 bot。

## 目录结构

```
komari-scheduler/
├── backend/
│   ├── main.go              # Go 服务：事件 CRUD + 定时调度 + TG 推送
│   └── frontend/
│       ├── dist/            # 前端预构建产物（COPY 进镜像）
│       └── frontend.go      # 静态文件服务（/ 路由）
├── data/                    # SQLite 数据（scheduler.db，volume 挂载持久化）
├── migrations/
├── docker-compose.yml       # 端口 9006:9006，挂载 ./data
├── Dockerfile.backend
└── TODO.md
```

## 环境变量

| 变量 | 说明 |
|------|------|
| `PORT` | 服务端口（main.go 默认 8080；docker-compose 中设为 `9006` 并映射 `9006:9006`） |

## 事件配置

在界面填写：

- 事件名称
- 推送内容
- TG chat_id
- TG bot token
- `trigger_days`：自定义天数
  - `0`：立即执行
  - `>0`：首次延后 N 天触发，之后每 N 天继续推送

## 操作说明

- 新建：填写表单后点“保存”
- 编辑：点击事件列表里的“编辑”，表单回填后修改并保存
- 启用/停用：点击对应按钮切换状态
- 删除：点击“删除”并确认，事件将从数据库中移除
- 背景：右上角“⚙️ 设置”输入图片 URL，留空恢复默认

## 技术栈

- Go + mattn/go-sqlite3 + SQLite（`data/scheduler.db`）
- HTML/CSS/JS 静态前端（预构建后嵌入 Go 服务）
- Docker Compose
