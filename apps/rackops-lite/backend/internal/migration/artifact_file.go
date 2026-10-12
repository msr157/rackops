package migration

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
)

// CreateBackup creates a private encrypted artifact without replacing an
// existing backup. The caller persists the digest and key reference separately.
func CreateBackup(ctx context.Context, source io.Reader, destination string, key []byte) (ArtifactDigest, error) {
	var digest ArtifactDigest
	err := publishFile(destination, func(file *os.File) error {
		var err error
		digest, err = EncryptArtifact(ctx, file, source, key)
		return err
	})
	return digest, err
}

// RestoreBackup publishes a separate plaintext dump only after complete stream
// authentication and exact digest/size verification. It never overwrites data.
// Database import and enabling destination writers are separate later steps.
func RestoreBackup(ctx context.Context, source io.Reader, destination string, key []byte, expected ArtifactDigest) error {
	hash, err := hex.DecodeString(expected.SHA256)
	if err != nil || len(hash) != sha256.Size || expected.Bytes < 0 {
		return errors.New("invalid expected backup digest")
	}
	return publishFile(destination, func(file *os.File) error {
		actual, err := DecryptArtifact(ctx, file, source, key, expected.Bytes)
		if err != nil {
			return err
		}
		if actual != expected {
			return errors.New("backup does not match the selected artifact")
		}
		return nil
	})
}

// Stage in the same filesystem, then link atomically without replacement.
// Unsupported filesystems fail closed instead of exposing a partial restore.
// destination's parent must be a private, application-controlled directory.
func publishFile(destination string, write func(*os.File) error) error {
	file, err := os.CreateTemp(filepath.Dir(destination), ".rackops-artifact-*")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	defer file.Close()
	if err := file.Chmod(0600); err != nil {
		return err
	}
	if err := write(file); err != nil {
		return err
	}
	if err := file.Sync(); err != nil {
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	return os.Link(file.Name(), destination)
}
