package migration

import (
	"context"
	"errors"
	"io"
	"os/exec"
	"regexp"
)

type DatabaseEngine string

const (
	PostgreSQL DatabaseEngine = "postgresql"
	MySQL      DatabaseEngine = "mysql"
)

var containerID = regexp.MustCompile(`^[a-f0-9]{12,64}$`)
var databaseName = regexp.MustCompile(`^[a-z][a-z0-9_]{0,62}$`)

// Database operates on an already-owned container ID resolved by the agent.
// Credentials come from the database container's existing environment; they
// never enter command arguments, migration receipts or captured diagnostics.
type Database struct {
	Engine      DatabaseEngine
	ContainerID string
}

func (d Database) script() (string, error) {
	if !containerID.MatchString(d.ContainerID) {
		return "", errors.New("invalid database container identity")
	}
	switch d.Engine {
	case PostgreSQL:
		return `set -eu
: "${POSTGRES_USER:?database user missing}"
: "${POSTGRES_DB:?database name missing}"
case "$POSTGRES_DB" in *[!a-zA-Z0-9_]*) exit 1;; esac
export PGPASSWORD="${POSTGRES_PASSWORD:-}"
`, nil
	case MySQL:
		return `set -eu
: "${MYSQL_DATABASE:?database name missing}"
: "${MYSQL_ROOT_PASSWORD:?database root password missing}"
case "$MYSQL_DATABASE" in *[!a-zA-Z0-9_]*) exit 1;; esac
export MYSQL_PWD="$MYSQL_ROOT_PASSWORD"
`, nil
	default:
		return "", errors.New("database engine has no consistency adapter")
	}
}

// Dump requires the migration executor to have stopped all source application
// writers. PostgreSQL uses a custom-format snapshot; MySQL includes routines,
// events and triggers. This adapter requires official-image environment names.
// Filesystem-only copies must never be used as a substitute for these dumps.
func (d Database) Dump(ctx context.Context, destination io.Writer, writersStopped bool) error {
	if !writersStopped {
		return errors.New("source writers must be stopped before the final database dump")
	}
	script, err := d.script()
	if err != nil {
		return err
	}
	switch d.Engine {
	case PostgreSQL:
		script += `exec pg_dump --host=/var/run/postgresql --format=custom --no-owner --no-acl --username="$POSTGRES_USER" --dbname="$POSTGRES_DB"`
	case MySQL:
		// Disable dump statements which could alter destination replication state.
		script += `exec mysqldump --protocol=socket --user=root --single-transaction --routines --events --triggers --hex-blob --set-gtid-purged=OFF -- "$MYSQL_DATABASE"`
	}
	return d.run(ctx, script, nil, destination)
}

// RestoreNew creates a separate database. It deliberately fails if that name
// exists: an interrupted import requires inspection, never a blind overwrite.
// The executor must keep destination application writers disabled until health
// checks and routing cutover succeed. Source database ownership stays unchanged.
func (d Database) RestoreNew(ctx context.Context, source io.Reader, name string, destinationWritersDisabled bool) error {
	if !destinationWritersDisabled {
		return errors.New("destination application writers must remain disabled")
	}
	if !databaseName.MatchString(name) {
		return errors.New("invalid destination database name")
	}
	script, err := d.script()
	if err != nil {
		return err
	}
	switch d.Engine {
	case PostgreSQL:
		script += `createdb --host=/var/run/postgresql --username="$POSTGRES_USER" -- "$1"
exec pg_restore --host=/var/run/postgresql --exit-on-error --single-transaction --no-owner --no-acl --username="$POSTGRES_USER" --dbname="$1"`
	case MySQL:
		// The SQL identifier is validated above, not supplied as arbitrary SQL.
		script += "mysql --protocol=socket --user=root --execute='CREATE DATABASE `" + name + "`'\n"
		// Event execution stays disabled in the dedicated destination database
		// container during validation. Import failure leaves this container gated.
		script += "mysql --protocol=socket --user=root --execute='SET GLOBAL event_scheduler=OFF'\n"
		script += `exec mysql --protocol=socket --user=root --binary-mode -- "$1"`
	}
	return d.run(ctx, script, source, io.Discard, name)
}

// Validate performs a read-only query check against the restored database.
func (d Database) Validate(ctx context.Context, name string) error {
	if !databaseName.MatchString(name) {
		return errors.New("invalid destination database name")
	}
	script, err := d.script()
	if err != nil {
		return err
	}
	switch d.Engine {
	case PostgreSQL:
		script += `exec psql --host=/var/run/postgresql --username="$POSTGRES_USER" --dbname="$1" -c "SELECT 1;"`
	case MySQL:
		script += `exec mysql --protocol=socket --user=root --execute="SELECT 1;" "$1"`
	}
	return d.run(ctx, script, nil, io.Discard, name)
}

func (d Database) run(ctx context.Context, script string, input io.Reader, output io.Writer, args ...string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	argv := []string{"exec", "-i", d.ContainerID, "sh", "-c", script, "rackops-migration"}
	argv = append(argv, args...)
	cmd := exec.CommandContext(ctx, "docker", argv...)
	cmd.Stdin = input
	cmd.Stdout = output
	cmd.Stderr = io.Discard // database errors may include SQL or customer data
	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return errors.New("database migration command failed; inspect the private database container and retain source and backup")
	}
	return nil
}
