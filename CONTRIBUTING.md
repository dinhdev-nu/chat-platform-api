# Contributing

Follow the [Quick Start](README.md#-quick-start) to configure and run the project.
For sample data and cleanup commands, see the [seed guide](cmd/seed/README.md).

## Before opening a PR

1. Keep the change focused and describe its effect on API or worker behavior.
2. Run `make fmt`; run `make tidy` when dependencies change.
3. Run `make check` and `make lint`; report any pre-existing failures.
4. For SQL changes, run `make gen_check` and `make gen`, then review generated diffs.
5. Follow the schema ownership rules below and document any required data migration.
6. Keep secrets out of commits: `.env`, local YAML configs, logs, and local test/audit
   files are ignored. Example configs must remain free of secrets.

## Database changes

| Data | Source to edit | Application |
|---|---|---|
| `users`, `oauth_accounts`, `user_tokens`, `user_contacts` | [GORM models](internal/infrastructure/mysql/gorm/model/), registered in [gorm/db.go](internal/infrastructure/mysql/gorm/db.go) | AutoMigrate during API/worker initialization or `go run ./cmd/schema` |
| Chat tables and indexes | New files in [migrations](internal/infrastructure/mysql/migrations/) | `make migrate_up` |
| Typed queries | [query](internal/infrastructure/mysql/query/) and [sqlc.yaml](sqlc.yaml) | `make gen_check`, then `make gen` |
| User schema for query generation | [user_stub.sql](internal/infrastructure/mysql/schema/user_stub.sql) | sqlc input only |

Add new Goose migrations for chat schema changes. Review the resulting SQL;
GORM AutoMigrate does not replace explicit data migrations. For migration #22,
follow the [deployment guide](internal/infrastructure/mysql/migrations/README.md).

sqlc reads migrations and schema stubs. When queries depend on changed user fields,
update the GORM model and `user_stub.sql` together. The stub is not a runtime
migration. Make pins sqlc to v1.29.0.

Files under `internal/infrastructure/mysql/sqlc/` marked
`Code generated ... DO NOT EDIT` must be regenerated from SQL/config inputs.
`batch_helpers.go` and `to_domain.go` are handwritten extensions and can be
edited directly; batch helpers stay in that package to access `Queries.db`.

## Architecture conventions

Services receive named `AuthDependencies`, `MessageDependencies`,
`RoomDependencies`, and `UserDependencies`. Each service file groups its
dependency interfaces, dependency struct, service struct, constructor and
business methods. HTTP handlers declare their consumer interfaces in the
corresponding handler file.

Assemble runtime adapters in [internal/wire/container.go](internal/wire/container.go).
Tests supply fakes through constructors without assigning globals. Where provided,
`Now` defaults to `time.Now`; a nil logger defaults to a no-op logger. Supply
required stores, sequence and event adapters; dependency structs document optional
queue and cache behavior.

[internal/presenter](internal/presenter/) contains pure REST/realtime mapping.
Services fetch presence and pass it to mappers as data. Both message-send wrappers
delegate to `Send(ctx, SendMessageCommand)`; preserve their HTTP response shapes
and messages when changing the shared implementation.

## Formatting and tests

`make fmt` formats handwritten Go files; `make fmt-check` reports formatting
problems without modifying files. Generated files are left to their generators.
`.gitattributes` keeps Go and shell files on LF line endings.

`make check` runs formatting checks, `go vet` and `go test`. When `test/main.go`
is available, it uses the local overlay runner to include unit/contract tests;
otherwise it runs standard Go commands against the checkout. `make test` requires
the local suite. The `test/` suite and `audit/` results are ignored and absent
from a fresh clone. Plain `go test ./...` does not load the local overlay tests.

When that suite is available:

```bash
go run ./test                      # Run the local suite without Make
go run ./scripts/format -w test    # Format local test sources
```

Local test instructions are in `test/README.md`. JSON contract fixtures live in
`test/testdata/internal/handler/testdata` and
`test/testdata/internal/service/testdata`. Regenerate them with
`STELLO_UPDATE_GOLDEN=1` only for an intentional, reviewed contract change.
Keep audit reports, probes and saved results in `audit/`.
