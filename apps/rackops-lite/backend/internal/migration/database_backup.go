package migration

import (
	"context"
	"io"
	"os"
)

// BackupDatabase streams a consistent database dump directly into encryption.
// A failed dump closes the pipe with an error, preventing publication of a
// valid-looking artifact containing only a partial dump. No plaintext dump is
// written to disk at the source.
func BackupDatabase(ctx context.Context, database Database, destination string, key []byte, writersStopped bool) (ArtifactDigest, error) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	reader, writer := io.Pipe()
	done := make(chan error, 1)
	go func() {
		err := database.Dump(ctx, writer, writersStopped)
		_ = writer.CloseWithError(err)
		done <- err
	}()
	digest, err := CreateBackup(ctx, reader, destination, key)
	_ = reader.CloseWithError(err)
	if err != nil {
		cancel()
	}
	dumpErr := <-done
	if err != nil {
		return ArtifactDigest{}, err
	}
	if dumpErr != nil {
		return ArtifactDigest{}, dumpErr
	}
	return digest, nil
}

// RestoreDatabase verifies the entire plaintext digest in private local staging
// before executing any SQL. The destination must be a dedicated migration DB
// container. Failed imports preserve the source and encrypted artifact; callers
// must not blindly retry against the partially created destination database.
func RestoreDatabase(ctx context.Context, database Database, encrypted io.Reader, key []byte, expected ArtifactDigest, stagingParent, name string, writersDisabled bool) error {
	stage, err := os.MkdirTemp(stagingParent, ".rackops-db-restore-*")
	if err != nil {
		return err
	}
	// Only agent-created files inside this exact private directory are removed.
	defer os.Remove(stage)
	dump := stage + string(os.PathSeparator) + "verified.dump"
	defer os.Remove(dump)
	if err := RestoreBackup(ctx, encrypted, dump, key, expected); err != nil {
		return err
	}
	f, err := os.Open(dump)
	if err != nil {
		return err
	}
	defer f.Close()
	return database.RestoreNew(ctx, f, name, writersDisabled)
}
