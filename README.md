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

> **Status:** Core REST API, WebSocket hub, Redis integrations, MySQL repositories, migrations, worker infrastructure, and Docker configuration are included. Unit/contract tests are maintained locally and are not versioned. CI is not yet configured.

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
go install github.com/sqlc-dev/sqlc/cmd/sqlc@v1.29.0
```

`make gen` and `make gen_check` run sqlc v1.29.0 through Go directly, so a
standalone sqlc installation is optional. The first run may download the tool.

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
go run ./cmd/schema
make migrate_up
make run

# Windows (PowerShell)
$env:APP_ENV = "local"
go run ./cmd/schema
$dsn = go run ./cmd/dsn/main.go
goose -dir ./internal/infrastructure/mysql/migrations mysql $dsn up
go run ./cmd/api/main.go
```

On a new database, run the GORM schema command before Goose: chat-table foreign
keys reference `users`. Docker Compose runs these steps in the same order.

**5. Verify**

```bash
curl http://localhost:8080/health
# {"status":"ok"}
```

### Run with Docker Compose

The application image is shared by the API, worker, and migration services.
MySQL and Redis use their official images and do not need separate Dockerfiles.

```bash
# Build the application image and start the full stack
docker compose up --build -d

# Check container and health status
docker compose ps

# Follow API and worker logs
docker compose logs -f api worker

# Verify the API
curl http://localhost:8080/health

# Stop containers while keeping database data
docker compose down

# Stop containers and delete MySQL/Redis data
docker compose down -v
```

Compose reads overrides such as `MYSQL_PASSWORD`, `REDIS_PASSWORD`,
`JWT_SECRET`, `MAIL_FROM`, `MAIL_PASSWORD`, and `API_PORT` from the shell or
the root `.env` file. Replace the development defaults before deployment.

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

The project uses GORM for user data and Goose migrations for chat data:

| Purpose | Source to edit | How it is applied |
|---|---|---|
| `users`, `oauth_accounts`, `user_tokens`, `user_contacts` | Models in `internal/infrastructure/mysql/gorm/model/`; registration in `gorm/db.go` | GORM AutoMigrate during API/worker initialization or `go run ./cmd/schema` |
| `conversations`, `conversation_members`, `messages`, `attachments`, `message_reactions`, `message_status`, and chat indexes | New SQL migrations in `internal/infrastructure/mysql/migrations/` | Goose via `make migrate_up` |
| Typed DB queries | SQL in `internal/infrastructure/mysql/query/` and generation settings in `sqlc.yaml` | `make gen_check` and `make gen` |
| The `users` schema used by sqlc | `internal/infrastructure/mysql/schema/user_stub.sql` | Code-generation input only; update alongside relevant GORM user field changes |

sqlc reads both migrations and schema stubs. The user stub is not a runtime
migration. Existing migrations, including the historical creation and removal of
`user_contacts`, remain part of the migration history; current contact models are
managed by GORM. For schema changes, follow the owning model/migration and review
the resulting SQL; AutoMigrate does not replace explicit data migrations.

For the #22 read-watermark migration, follow the [deployment procedure](internal/infrastructure/mysql/migrations/README.md):
pause writers, apply the migration, and rebuild Redis unread counts before restarting.

Files in `internal/infrastructure/mysql/sqlc/` with a `Code generated ... DO NOT EDIT`
header are generated output. Edit their SQL/config inputs and regenerate them.
`batch_helpers.go` and `to_domain.go` are handwritten extensions in that package;
batch helpers stay there to access `Queries.db`. They can be edited directly.
The ignored `docs/database/db2.sql`, when present, is a reference snapshot.

After changing user fields referenced by queries, update the GORM model and user
stub together, then validate and regenerate queries with the pinned sqlc version.
Review generated diffs before committing. Old migration comments may mention
Kafka; the current job transport is Redis Streams.

```bash
make migrate_up           # Apply all pending migrations
make migrate_status       # Show migration state
make migrate_down         # Roll back last migration
make migrate_create name=add_something  # New migration file
make gen_check            # Validate queries without connecting to the database
make gen                  # Regenerate query code with sqlc v1.29.0
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

Redis Stream workers run as a standalone process from `cmd/worker/main.go`. The API process should enqueue jobs and return quickly; it does not start workers inline.

```bash
make run-worker
```

Or directly:

```bash
APP_ENV=local go run ./cmd/worker/main.go
```

| Job type | Stream | Consumer group |
|---|---|---|
| `email.send_otp` | `stream:email` | `email-workers` |
| `conversation.last_activity.update` | `stream:conversation` | `conversation-workers` |
| `conversation.system_message.create` | `stream:conversation` | `conversation-workers` |
| `auth.token_last_used.update` | `stream:auth` | `auth-workers` |
| `user.last_seen.update` | `stream:user` | `user-workers` |

`AuthService.SendOTP` enqueues `email.send_otp`; only the worker process initializes SMTP and sends email.

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
make run-worker     # Run background worker
make build          # Compile binary
make tidy           # go mod tidy
make lint           # golangci-lint
make fmt            # Format handwritten Go source
make fmt-check      # Check formatting without writing files
make check          # Format/vet/test; includes local tests when available
make test           # Requires the untracked local test/ directory
go run ./test       # Local test suite without Make (requires test/)
```


> The Makefile uses POSIX shell syntax. On Windows, use **Git Bash** or **WSL**.

`make check`, `make fmt`, and `make fmt-check` invoke Go commands and also work
from PowerShell with Go and GNU Make on PATH. Formatting covers handwritten Go
files in `cmd`, `config`, `global`, `internal`, `pkg`, and `scripts`; generated
files are left to their generators. Format checks report file paths and fail
without modifying source. Local test sources can be formatted separately with
`go run ./scripts/format -w test`.

`.gitattributes` keeps Go and shell files on LF line endings across platforms.

After checking format, when `test/main.go` exists, `make check` runs `go vet ./...` and
`go test ./... -count=1` through the local overlay runner, including the centralized
unit/contract tests. Otherwise it runs the standard Go commands directly against
the checkout. A fresh clone contains no unit/contract tests, so this fallback
checks compilation and vet without providing the local suite's behavioral coverage.
Existing `make lint` and `make gen_check` remain available for lint and SQL validation.

Test sources, fixtures, and the overlay runner are maintained locally under `test/`.
Both `test/` and `audit/` are intentionally ignored by Git and are absent from a
fresh clone. With the local suite available, use `make check`, `make test`, or
`go run ./test`; plain `go test ./...` does not load those test sources. `make test`
reports an error when the local suite is unavailable. Local instructions are in
`test/README.md`. Keep audit reports, probes, and saved results in `audit/`.

Services receive named `AuthDependencies`, `MessageDependencies`,
`RoomDependencies`, and `UserDependencies`. Each service file groups its dependency
interfaces, dependency struct, service struct, constructor, and business methods.
HTTP handlers declare their consumer interfaces in the corresponding handler file.
Runtime adapters are assembled in `internal/wire/container.go`. Unit tests supply
fakes without assigning globals.
`Now` is optional and defaults to `time.Now`; a nil logger defaults to a no-op
logger. Required stores/sequence/event adapters must be supplied; optional queue
and cache behavior is documented on the dependency structs.

`internal/presenter` contains pure REST/realtime mapping. Presence is fetched by
the service and passed to the mapper as data. Local JSON contract fixtures live in
`test/testdata/internal/handler/testdata` and `test/testdata/internal/service/testdata`; only regenerate them
with `STELLO_UPDATE_GOLDEN=1` when an intentional contract change has been reviewed.
Both message-send wrappers delegate to `Send(ctx, SendMessageCommand)` while
retaining their existing HTTP response shapes and messages.

---

## 🤝 Contributing

Before opening a PR:

1. Keep changes focused and scoped
2. Run `make fmt`; run `go mod tidy` when dependencies change
3. Run `make check` — no regressions
4. Run `make gen_check` and `make gen` if SQL queries or generation inputs changed
5. Follow the [database ownership table](#-database) for schema changes; add a new Goose migration for chat tables and keep relevant GORM models/user stubs in sync
6. **Never commit** `.env`, local YAML configs, logs, or local test/audit files; keep example configs free of secrets

---

## 📝 Open Source Checklist

- [ ] `LICENSE`
- [ ] `CONTRIBUTING.md`
- [ ] `SECURITY.md`
- [x] `.env.example`
- [x] Docker / `compose.yaml`
- [ ] CI pipeline (GitHub Actions)

---

<div align="center">

Made with ❤️ by [dinhdev-nu](https://github.com/dinhdev-nu)

</div>
