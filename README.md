<div align="center">

<img src="public/stello-banner.png" alt="Stello – Chat Platform API" width="100%" />

# Stello — Chat Platform API

A production-grade Go backend for real-time messaging — featuring email OTP auth, JWT sessions, direct/group conversations, WebSocket fan-out via Redis Pub/Sub & Streams, and MySQL persistence.

<br/>

[![Go](https://img.shields.io/badge/Go-1.26.2-00ADD8?style=for-the-badge&logo=go&logoColor=white)](https://go.dev/)
[![Gin](https://img.shields.io/badge/Gin-HTTP%20API-00B386?style=for-the-badge)](https://github.com/gin-gonic/gin)
[![MySQL](https://img.shields.io/badge/MySQL-8.x-4479A1?style=for-the-badge&logo=mysql&logoColor=white)](https://www.mysql.com/)
[![Redis](https://img.shields.io/badge/Redis-7.x-DC382D?style=for-the-badge&logo=redis&logoColor=white)](https://redis.io/)
[![WebSocket](https://img.shields.io/badge/WebSocket-Gorilla-5C2D91?style=for-the-badge)](https://github.com/gorilla/websocket)
[![License](https://img.shields.io/badge/license-TBD-lightgrey?style=for-the-badge)](#license)

> **Status:** Core REST API, WebSocket hub, Redis integrations, MySQL repositories, migrations, and worker infrastructure are complete. Docker, CI, and tests are not yet included.

</div>

---

## ✨ Features

<img src="public/features.png" alt="Features" width="100%" />

---

## 🖼 System Overview

<div align="center">
  <img src="public/system-overview.png" alt="System Overview" width="760" />
</div>

---

## 🛠 Tech Stack

| Layer | Technology |
|---|---|
| **Language** | Go `1.26.2` |
| **HTTP** | [Gin](https://github.com/gin-gonic/gin) |
| **WebSocket** | [Gorilla WebSocket](https://github.com/gorilla/websocket) |
| **Database** | MySQL 8.x, [GORM](https://gorm.io/), [sqlc](https://sqlc.dev/) |
| **Migrations** | [Goose](https://github.com/pressly/goose) |
| **Cache / Realtime** | Redis 7.x — Pub/Sub, Streams |
| **Auth** | JWT HMAC, Redis-backed sessions |
| **Config** | [Viper](https://github.com/spf13/viper), godotenv |
| **Logging** | [Zap](https://github.com/uber-go/zap), Lumberjack |
| **Mail** | SMTP via [go-mail](https://github.com/wneessen/go-mail) |

---

## 📋 Requirements

- Go `1.26.2`+
- MySQL `8.x`
- Redis `7.x`
- SMTP account for OTP emails
- *(Optional)* `make`, `goose`, `sqlc`, `golangci-lint`

```bash
go install github.com/pressly/goose/v3/cmd/goose@v3.24.1
go install github.com/sqlc-dev/sqlc/cmd/sqlc@v1.27.0
```

---

## 🚀 Quick Start

**1. Clone & install**

```bash
git clone https://github.com/dinhdev-nu/chat-platform-api.git
cd chat-platform-api
go mod download
```

**2. Configure environment**

```bash
cp .env.example .env
cp environment/local.example.yaml environment/local.yaml
```

**3. Create the database**

```sql
CREATE DATABASE chat_platform_api
  CHARACTER SET utf8mb4
  COLLATE utf8mb4_unicode_ci;
```

**4. Run migrations and start**

```bash
# Linux / macOS
make migrate_up
make run

# Windows (PowerShell)
$env:APP_ENV = "local"
$dsn = go run ./cmd/dsn/main.go
goose -dir ./internal/infrastructure/mysql/migrations mysql $dsn up
go run ./cmd/api/main.go
```

**5. Verify**

```bash
curl http://localhost:8080/health
# {"status":"ok"}
```

---

## ⚙️ Configuration

The app loads `.env` first, then the YAML file selected by `APP_ENV`:

```
APP_ENV=local       →  environment/local.yaml
APP_ENV=production  →  environment/production.yaml
```

**Required environment variables:**

| Variable | Purpose |
|---|---|
| `APP_ENV` | Runtime environment (default: `local`) |
| `MYSQL_PASSWORD` | MySQL password |
| `REDIS_PASSWORD` | Redis password |
| `JWT_SECRET` | JWT signing secret |
| `MAIL_PASSWORD` | SMTP password / app password |

YAML sections: `server`, `mysql`, `redis`, `logger`, `jwt`, `mail`, `cors`.

---

## 🗃 Database

The project uses a hybrid approach:

- **GORM AutoMigrate** — manages `users`, `oauth_accounts`, `user_tokens`, `user_contacts`
- **Goose** — manages chat tables and schema changes (`conversations`, `messages`, `attachments`, `reactions`, `message_status`, indexes, etc.)
- **sqlc** — generates typed query code from `internal/infrastructure/mysql/query`

```bash
make migrate_up           # Apply all pending migrations
make migrate_status       # Show migration state
make migrate_down         # Roll back last migration
make migrate_create name=add_something  # New migration file
make gen                  # Regenerate sqlc code
```

---

## 📡 API Reference

**Base URL:** `http://localhost:8080/api/v1`

Protected routes require: `Authorization: Bearer <access_token>`

> IDs are 32-character hex strings encoding `BINARY(16)` values.

### Auth

| Method | Path | Auth | Description |
|---|---|:---:|---|
| `POST` | `/auth/send-otp` | — | Send OTP to email |
| `POST` | `/auth/verify-otp` | — | Verify OTP, issue JWT |
| `POST` | `/auth/logout` | ✓ | Revoke session |

### Users & Contacts

| Method | Path | Auth | Description |
|---|---|:---:|---|
| `GET` | `/users/me` | ✓ | Current user profile |
| `PUT` | `/users/me` | ✓ | Update profile |
| `GET` | `/users/search` | ✓ | Search users |
| `POST` | `/contacts/requests` | ✓ | Send contact request |
| `GET` | `/contacts/requests/incoming` | ✓ | Incoming requests |
| `PUT` | `/contacts/requests/accept` | ✓ | Accept request |
| `GET` | `/contacts` | ✓ | List contacts |

### Conversations & Messages

| Method | Path | Auth | Description |
|---|---|:---:|---|
| `POST` | `/conversations/direct` | ✓ | Get or create DM |
| `POST` | `/conversations/group` | ✓ | Create group / channel |
| `GET` | `/conversations` | ✓ | List conversations |
| `POST` | `/conversations/:id/members` | ✓ | Add member |
| `DELETE` | `/conversations/:id/members/:user_id` | ✓ | Remove member |
| `POST` | `/conversations/:id/messages` | ✓ | Send message |
| `GET` | `/conversations/:id/messages` | ✓ | List messages |
| `POST` | `/conversations/:id/read` | ✓ | Mark as read |
| `PUT` | `/messages/:id` | ✓ | Edit message |
| `DELETE` | `/messages/:id` | ✓ | Soft delete message |
| `POST` | `/messages/:id/reactions` | ✓ | Toggle reaction |
| `GET` | `/ws` | ✓ | WebSocket upgrade |

---

## 🔌 WebSocket

**Endpoint:** `ws://localhost:8080/api/v1/ws?token=<access_token>`

**Client → Server** (inbound frame):

```json
{ "type": "typing", "payload": { "conv_id": "0190d6f2..." } }
```

Supported inbound types: `typing` · `viewing` · `left` · `read`

**Server → Client** (outbound events):

```json
{ "event": "message.new", "conv_id": "...", "msg_id": "...", "sender_id": "...", "seq": 42 }
```

Main outbound events: `typing` · `presence` · `message.new` · `message.read` · `message.edited` · `message.deleted` · `reaction.toggle` · `conversation.created` · `member.added` · `member.removed`

> ⚠️ WebSocket events are **real-time signals, not durable messages.** Clients should resync via REST after reconnecting.

---

## ⚙️ Worker

```bash
APP_ENV=local go run ./cmd/worker/main.go
```

| Job type | Stream | Consumer group |
|---|---|---|
| `email.send_otp` | `stream:email` | `email-workers` |

> **Note:** `AuthService.SendOTP` currently uses the direct SMTP fallback. The Redis Stream enqueue path is implemented in `auth_service.go` but commented out.

---

## 🗂 Project Structure

<div align="center">
  <img src="public/project-structure.png" alt="Project Structure" width="640" />
</div>

---

## 🛠 Development

```bash
make help           # List all make targets
make run            # Run the API server
make build          # Compile binary
make tidy           # go mod tidy
make lint           # golangci-lint
go test ./...       # Run tests
```

> The Makefile uses POSIX shell syntax. On Windows, use **Git Bash** or **WSL**.

---

## 🤝 Contributing

Before opening a PR:

1. Keep changes focused and scoped
2. Run `gofmt -w .` and `go mod tidy`
3. Run `go test ./...` — no regressions
4. Run `make gen` if SQL queries changed
5. Add a Goose migration for any schema change
6. **Never commit** `.env`, YAML configs, logs, or local files

---

## 📝 Open Source Checklist

- [ ] `LICENSE`
- [ ] `CONTRIBUTING.md`
- [ ] `SECURITY.md`
- [ ] `.env.example` *(confirm it exists)*
- [ ] Docker / `docker-compose.yml`
- [ ] CI pipeline (GitHub Actions)

---

<div align="center">

Made with ❤️ by [dinhdev-nu](https://github.com/dinhdev-nu)

</div>