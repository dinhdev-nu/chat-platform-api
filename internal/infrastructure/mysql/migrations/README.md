# Precise read watermarks (#22)

Migration `20260910155000` upgrades `last_read_at` and `last_activity_at` to
`DATETIME(3)` and adds `last_read_seq`. Apply it before running code that uses
the new read cursor. HTTP/WebSocket requests still take a message ID.

Old second-precision read timestamps have lost their milliseconds and seq.
The migration conservatively rewinds non-null read timestamps by one second,
with seq zero. Some recently read messages may appear unread once (a window
of up to two seconds, depending on the old rounding mode). Null cursors stay
null. Activity and previews are rebuilt from the latest stored message using
`(created_at, seq)`; empty conversations keep their existing activity time.

## Deployment

1. Back up the database and pause API/worker instances that write messages,
   read cursors, conversation summaries or unread counters. MySQL DDL commits
   implicitly; Goose cannot roll back a partially applied migration atomically.
2. Apply Goose migrations to the intended application database. On a partial
   failure, inspect the schema and restore the backup before retrying.
3. Rebuild Redis unread counters **before resuming writers**, using the query
   below. For each row, `SET unread:<user_id>:<conversation_id> <unread>` on the
   application's Redis database. IDs are lowercase hex. Do not merely delete
   the keys: an incoming `INCR` on a missing key cannot recover older unread
   messages. Do not flush Redis during an ordinary migration; it also contains
   sessions, OTPs and sequence counters.
4. Start the updated API/worker instances together and verify reading messages
   and listing conversations. Do not mix old and new writers during deployment.

```sql
SELECT LOWER(HEX(cm.user_id)) AS user_id,
       LOWER(HEX(cm.conversation_id)) AS conversation_id,
       COUNT(m.id) AS unread
FROM conversation_members cm
LEFT JOIN messages m ON m.conversation_id = cm.conversation_id
  AND m.sender_id != cm.user_id AND m.is_deleted = 0
  AND (cm.last_read_at IS NULL OR m.created_at > cm.last_read_at
       OR (m.created_at = cm.last_read_at AND m.seq > cm.last_read_seq))
GROUP BY cm.user_id, cm.conversation_id;
```

Export the result without headers as a tab-separated file. With `redis-cli`
configured for the correct host/database/authentication, PowerShell can restore
each counter and stop on an error:

```powershell
Get-Content -LiteralPath unread-counts.tsv | ForEach-Object {
    $userId, $conversationId, $unread = $_ -split "`t"
    $reply = redis-cli --raw SET "unread:${userId}:${conversationId}" $unread
    if ($LASTEXITCODE -ne 0 -or $reply -ne 'OK') { throw 'Unread rebuild failed' }
}
```

For an explicitly approved reset of disposable stage data, recreate the app
database and clear its Redis database while writers are stopped, then apply
the normal GORM/Goose schema setup. An empty database has no unread counters
to rebuild. Never use the reset procedure for data that must be retained.

`Down` restores the old column definitions but loses fractional precision and
seq positions. Restoring the backup is preferable for a full rollback; either
way, pause writers and rebuild Redis counts using the matching application
version before restarting. Reapplying `Up` rewinds old read timestamps again.

This change uses the existing per-conversation seq allocation. Duplicate seq
recovery (#18), general unread-cache races and durable event delivery remain
separate concerns.
