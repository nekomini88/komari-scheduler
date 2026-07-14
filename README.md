# Komari Scheduler

自定义事件定时推送到 Telegram。

## 功能

- 📅 支持自定义天数自动通知，无需 cron
- 🎯 多事件/多 chat_id 隔离
- 🖥️ Komari 风格前端界面
- 🐘 SQLite 本地持久化
- 🐳 支持 Docker Compose 一键部署

## 快速开始

```bash
git clone https://github.com/nekomini88/komari-scheduler.git
cd komari-scheduler
cp .env.example .env   # 填入 TG_BOT_TOKEN
docker-compose up --build
```

访问 `http://localhost:8080` 即可配置事件。

## 环境变量

- `PORT`：服务端口
- `TG_BOT_TOKEN`：默认 Telegram Bot Token

## 事件配置

在界面填写：
- 事件名称
- cron 表达式，如 `0 8,20 * * * `
- 推送内容
- TG chat_id
- TG bot token
- `trigger_days`：首次发送前延后 N 天；首次发送后仍按 cron + N 天继续延迟触发

## 技术栈

- Go + robfig/cron + SQLite
- HTML/CSS/JS 静态前端
- Docker Compose
