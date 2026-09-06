# Control-plane state encryption and recovery

This release stores `state.json` as an authenticated AES-256-GCM snapshot.
Despite the historical filename it is no longer plaintext JSON. Existing JSON
state is migrated before the server accepts requests. Older binaries fail to
parse the new binary framing rather than silently replacing it with an empty
installation. Do not downgrade without a compatible recovery procedure.

The separate 32-byte key is `state.key` in the data directory, or an explicit
path in `RACKOPS_STATE_KEY_FILE`. Protect that key with OS access controls and
keep a secure separate recovery copy. Never upload it with the snapshot.
Changing the key file does not rotate encryption: it makes old state unreadable.
Managed key rotation and external key-store integration are not implemented.

The owner can download current state in **Settings → State backup** or through
`GET /api/backups/state` with owner authentication. The response is encrypted
and non-cacheable. Other roles cannot export this installation-wide snapshot.

## Offline restore

Use an independent destination with an existing parent and no existing target
directory. Keep the original installation available until recovery is verified.

```sh
rackops restore-state --snapshot rackops-state.enc --key /secure/recovery/state.key --data-dir /srv/rackops-restored
```

The command verifies integrity and schema before writing. It refuses an existing
destination. Start with `rackops server --data-dir /srv/rackops-restored` only
after restoring required loose identity files and application data. Unset an
unrelated `RACKOPS_STATE_KEY_FILE`, or point it to the matching original key.

This snapshot includes control-plane configuration, users and queued tasks.
It does **not** include application volumes/database contents, agent identities,
the loose join-token/mesh keys, CRM SQLite, or the vendor Front Door registry.
Restore those independently. Review queued operations before reconnecting agents.
Raft logs/snapshots use their own existing persistence and are not encrypted by
this change; protect HA storage separately. Complete scheduled disaster recovery
and database/volume restore remain release gates.
