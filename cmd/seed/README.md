# Local sample data

Run commands from the repository root. The seed tool creates sample users,
conversations and messages for local development. `plan` generates and validates
an offline preview in memory; it does not connect to MySQL/Redis or persist data.

## Preview and apply

With Go and GNU Make on PATH, these seed targets work in Git Bash, PowerShell
and WSL. Make exports `APP_ENV=local` and uses the API's
`environment/local.yaml` and `.env`.

1. Start MySQL and Redis with the configuration from the [Quick Start](../../README.md#-quick-start).
   On a fresh database, run `go run ./cmd/schema`, apply Goose migrations, then
   run `go run ./cmd/schema` again to ensure the current GORM tables are present.
   The seed command does not run application migrations.
2. Preview the dataset. Let pending worker jobs finish, then stop API and worker.
3. Apply and verify before restarting the application:

```bash
make seed-plan
make seed-apply WRITERS_STOPPED=1
make seed-verify-baseline
```

`WRITERS_STOPPED=1` confirms that you stopped the writers; it does not stop
processes or drain queues. Apply, sync and reset require this acknowledgement.
Keep writers stopped during verification for a consistent MySQL/Redis comparison.

`SEED_DATABASE` defaults to `chat_platform_api` and must match the configured
MySQL database. It confirms the target, **it does not change the connection**.
Redis uses the API's configured host, port and DB. To use an isolated pair,
configure both endpoints first; do not share its Redis DB with another MySQL dataset.

Database commands require explicit `APP_ENV=local`, a non-production server mode,
and loopback hosts or Compose service names `mysql`/`redis`. Run the CLI and API
with the same local timezone.

## Dataset and options

| Setting | Make default | Go CLI default | Supported values |
|---|---|---|---|
| Users | 50 | 50 | Exactly 50 |
| Conversations | 150 | 75 | 50–150 |
| Messages | 50,000 | 25,000 | 25,000–50,000 |
| Dataset name | `local-demo-v1` | `local-demo-v1` | `SEED_DATASET` / `--dataset` |

The dataset includes `23t1020100@husc.edu.vn` and `emailnhasai@gmail.com`, plus
48 synthetic users with `example.test` emails. Existing active test accounts
retain their IDs, profiles and sessions; inactive accounts are rejected. There
are 50 dataset identities, so only 48–50 users may need to be inserted.

Data includes direct/group conversations, empty rooms, short and long histories,
replies, edits, soft deletes, reactions and varied read states. Message totals
include system and soft-deleted messages. Attachments, contacts, sessions and OTPs
are not generated. No emails or historical notifications are sent; use the
application's normal OTP flow to log in after seeding.

Override the profile consistently for plan and apply:

```bash
make seed-plan SEED_CONVERSATIONS=75 SEED_MESSAGES=25000
make seed-apply WRITERS_STOPPED=1 SEED_CONVERSATIONS=75 SEED_MESSAGES=25000
```

Verify, sync and reset load the saved profile and need no size flags.
Reapplying a ready dataset with the same profile is a no-op; a changed profile
under the same name is rejected. No command automatically resets existing data.

## Verify, recover and reset

Run these as needed, with API and worker stopped:

```bash
make seed-verify                       # Verify current data after application use
make seed-sync WRITERS_STOPPED=1        # Rebuild scoped Redis state from MySQL
make seed-reset WRITERS_STOPPED=1       # Deliberately delete the seeded dataset
```

`seed-verify-baseline` checks original totals immediately after seeding.
`seed-verify` allows subsequent messages and changed read states/memberships.
Verification reports inconsistencies; it does not repair them. Let pending
summary jobs finish before stopping the worker and checking conversation previews.

Apply inserts batches of 500 rows within one MySQL data transaction and verifies
SQL relationships before committing. Redis synchronization follows the commit;
it rebuilds unread counters, preserves higher sequence allocations and invalidates
the dataset's member caches.

| Result | Next step |
|---|---|
| Failure before SQL commit | Seed rows and the dataset record roll back; fix the cause and retry apply. |
| Uncertain SQL commit result | Rerun apply to inspect the saved state before retrying any writes. |
| SQL committed but Redis failed (`sql-ready`) | Run `make seed-sync WRITERS_STOPPED=1`, then `make seed-verify`. |
| Reset committed in MySQL but Redis cleanup failed (`reset-db-done`) | Repeat the same reset command to finish cleanup. |

The local `_stello_local_seeds` registry stores ownership and recovery metadata.
Its table is created before the data transaction and may remain empty after a
failed apply. Keep it while a dataset exists. Sync uses current MySQL data;
it does not restore original messages, read cursors or conversation previews.

**Reset deletes seeded conversations, including messages added to those rooms
after seeding, and the 48 synthetic users. Both test accounts and their sessions
remain, even when seed created them.** It refuses deletion if synthetic users have
contacts, sessions, OAuth records or relationships outside the dataset, if outside
users joined seeded rooms, or if external messages reply to seeded messages.
Resolve those dependencies deliberately before retrying.

Reset removes only scoped Redis keys; it never flushes Redis or truncates tables.
After successful apply/sync verification, restart API and worker for normal use.

## Without Make

The CLI defaults to an offline plan when no subcommand is provided:

```bash
go run ./cmd/seed
go run ./cmd/seed plan --users 50 --conversations 150 --messages 50000
```

For commands that access the database, set `APP_ENV` in your shell first
(`export APP_ENV=local` in Bash, or `$env:APP_ENV = "local"` in PowerShell):

```bash
go run ./cmd/seed apply --database chat_platform_api --writers-stopped --users 50 --conversations 150 --messages 50000
go run ./cmd/seed verify --database chat_platform_api --baseline
```

Use `--dataset` to select a saved dataset. `--batch-size` accepts 1–1000 and
`--timeout` defaults to 10 minutes. For repeatable previews, pin a nonzero
`--random-seed`, an RFC3339 whole-second `--at`, the dataset name and generator
version. Run `go run ./cmd/seed plan --help` for all options.
