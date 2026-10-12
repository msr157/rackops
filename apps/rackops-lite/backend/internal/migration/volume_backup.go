package migration

import (
	"context"
	"io"
	"os"
	"path/filepath"
)

func BackupVolume(ctx context.Context, source, destination string, key []byte, writersStopped bool) (ArtifactDigest, error) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	reader, writer := io.Pipe()
	done := make(chan error, 1)
	go func() {
		err := ArchiveVolume(ctx, source, writer, writersStopped)
		_ = writer.CloseWithError(err)
		done <- err
	}()
	digest, err := CreateBackup(ctx, reader, destination, key)
	_ = reader.CloseWithError(err)
	if err != nil {
		cancel()
	}
	archiveErr := <-done
	if err != nil {
		return ArtifactDigest{}, err
	}
	if archiveErr != nil {
		return ArtifactDigest{}, archiveErr
	}
	return digest, nil
}

// RestoreVolume verifies the encrypted archive before extracting into a new
// destination directory. On extraction failure that directory remains isolated;
// callers must never attach it to a workload or remove the recoverable source.
func RestoreVolume(ctx context.Context, encrypted io.Reader, key []byte, expected ArtifactDigest, stagingParent, destination string, maxExpandedBytes int64) error {
	stage, err := os.MkdirTemp(stagingParent, ".rackops-volume-restore-*")
	if err != nil {
		return err
	}
	defer os.Remove(stage)
	archive := filepath.Join(stage, "verified.tar")
	defer os.Remove(archive)
	if err := RestoreBackup(ctx, encrypted, archive, key, expected); err != nil {
		return err
	}
	file, err := os.Open(archive)
	if err != nil {
		return err
	}
	defer file.Close()
	return ExtractVolume(ctx, file, destination, maxExpandedBytes)
}
