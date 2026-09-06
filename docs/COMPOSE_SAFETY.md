# Compose deployment safety

Local implementation; live Docker/Podman deployment acceptance is outstanding.

New stacks use a project name based on the RackOps service ID. Identical app
names therefore do not share the implicit Compose volume/network namespace.
Existing stacks keep their recorded legacy project name to avoid silently
creating empty replacement volumes. Before deployment, lifecycle operations
or log access, RackOps inspects project containers and requires their Compose
working-directory labels to match the app's managed directory. Missing or
conflicting ownership evidence causes an error requiring inspection.

Start, stop and restart use stack commands. Deployment validates configuration
before running `up`, waits for Compose's running/healthy result, and does not
automatically remove orphan services. Failed deployment after `up` begins is
an uncertain operation requiring inspection.

Each configuration update writes a private revision directory containing YAML
and an environment file, including an empty file when all variables are removed.
After validation a single active-revision pointer selects the complete pair.
Previous revisions remain on disk. Invalid new configuration does not replace
the active pointer. Files use restrictive permissions and are synced before
the pointer is published; power-loss durability of directory metadata remains
part of platform acceptance testing.

Environment values use literal single-quoted syntax, tested with the installed
Compose parser. Control characters, trailing backslashes and a backslash
immediately before a single quote currently return explicit validation errors.
Duplicate/invalid keys and `COMPOSE_` control variables are rejected. See
[Docker's environment-file syntax](https://docs.docker.com/compose/how-tos/environment-variables/variable-interpolation/).

## Remaining limits

- Configuration revisions do not snapshot volumes or databases. Reusing an
  older configuration after data writes requires application-specific recovery.
- A legacy project without surviving containers cannot be fully verified from
  container labels alone. Volume/network ownership migration remains work.
- Explicit external resources and fixed container names in customer YAML need
  a separate compatibility review; project names do not isolate those resources.
- Shell environment precedence, supported Compose/Podman versions, full
  inventory tracking, port discovery, cleanup and live interruption fixtures
  remain release gates. A successful parser test is not a deployment test.
- Revisions have no automated retention cleanup. Include them in protected
  host backups; they can contain application secrets.
