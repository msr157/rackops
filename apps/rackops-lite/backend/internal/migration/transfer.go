package migration

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
)

// TransferManifest identifies encrypted bytes, not plaintext application data.
// It must come from the authorized source through the control plane.
type TransferManifest struct {
	Bytes  int64  `json:"bytes"`
	SHA256 string `json:"sha256"`
}

// Receiver owns one private transfer directory. The agent must keep one receiver
// per job and prevent concurrent processes from owning the same directory.
// Network handlers must enforce organization/job authorization before using it.
type Receiver struct {
	mu       sync.Mutex
	dir      string
	manifest TransferManifest
}

// OpenReceiver resumes the same immutable encrypted artifact after a restart.
// An existing job directory cannot be repurposed for different content.
func OpenReceiver(dir string, manifest TransferManifest) (*Receiver, error) {
	digest, err := hex.DecodeString(manifest.SHA256)
	if err != nil || len(digest) != sha256.Size || manifest.Bytes < 1 || manifest.Bytes > 1<<50 || hex.EncodeToString(digest) != manifest.SHA256 {
		return nil, errors.New("invalid transfer manifest")
	}
	if err := os.Mkdir(dir, 0700); err != nil && !os.IsExist(err) {
		return nil, err
	}
	info, err := os.Lstat(dir)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return nil, errors.New("transfer directory must be private local storage")
	}
	raw, err := json.Marshal(manifest)
	if err != nil {
		return nil, err
	}
	path := filepath.Join(dir, "manifest.json")
	if err := publishFile(path, func(f *os.File) error { return writeFull(f, raw) }); err != nil {
		if !os.IsExist(err) {
			return nil, err
		}
		existing, err := os.ReadFile(path)
		if err != nil || !bytes.Equal(existing, raw) {
			return nil, errors.New("transfer job already belongs to another artifact")
		}
	}
	return &Receiver{dir: dir, manifest: manifest}, nil
}

// Offset is the durable prefix length. The sender may retry any previously
// acknowledged chunk; Append compares the existing bytes before acknowledging.
func (r *Receiver) Offset() (int64, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, name := range []string{"artifact.enc", "partial.enc"} {
		info, err := os.Lstat(filepath.Join(r.dir, name))
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return 0, err
		}
		if !info.Mode().IsRegular() || info.Size() > r.manifest.Bytes {
			return 0, errors.New("invalid transfer file")
		}
		return info.Size(), nil
	}
	return 0, nil
}

func (r *Receiver) Append(ctx context.Context, offset int64, chunk []byte, digest string) (int64, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	sum := sha256.Sum256(chunk)
	if len(chunk) == 0 || len(chunk) > chunkSize || offset < 0 || offset > r.manifest.Bytes-int64(len(chunk)) || hex.EncodeToString(sum[:]) != digest {
		return 0, errors.New("invalid transfer chunk")
	}
	path := filepath.Join(r.dir, "partial.enc")
	if _, err := os.Lstat(filepath.Join(r.dir, "artifact.enc")); err == nil {
		return 0, errors.New("transfer is already finalized")
	} else if !os.IsNotExist(err) {
		return 0, err
	}
	if info, err := os.Lstat(path); err == nil && !info.Mode().IsRegular() {
		return 0, errors.New("invalid transfer file")
	} else if err != nil && !os.IsNotExist(err) {
		return 0, err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return 0, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return 0, err
	}
	size := info.Size()
	if size > r.manifest.Bytes || offset > size {
		return size, errors.New("transfer offset does not match durable prefix")
	}
	// A lost response or partially written chunk is safe to retry. Compare the
	// existing prefix and append only bytes not already present.
	existing := min(size-offset, int64(len(chunk)))
	if existing > 0 {
		buf := make([]byte, existing)
		if _, err := f.ReadAt(buf, offset); err != nil {
			return size, err
		}
		if !bytes.Equal(buf, chunk[:existing]) {
			return size, errors.New("retried transfer chunk conflicts with stored bytes")
		}
	}
	if existing < int64(len(chunk)) {
		if _, err := f.WriteAt(chunk[existing:], offset+existing); err != nil {
			return size, err
		}
		size = offset + int64(len(chunk))
	}
	if err := f.Sync(); err != nil {
		return size, err
	}
	return size, nil
}

// Finalize re-reads the entire encrypted artifact before publishing it. A
// finalized artifact is still encrypted; RestoreBackup separately authenticates
// and checks the expected plaintext digest before creating destination data.
func (r *Receiver) Finalize(ctx context.Context) (string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	complete := filepath.Join(r.dir, "artifact.enc")
	source := filepath.Join(r.dir, "partial.enc")
	if _, err := os.Lstat(complete); err == nil {
		source = complete
	} else if !os.IsNotExist(err) {
		return "", err
	}
	info, err := os.Lstat(source)
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() || info.Size() != r.manifest.Bytes {
		return "", errors.New("transfer is incomplete")
	}
	f, err := os.Open(source)
	if err != nil {
		return "", err
	}
	defer f.Close()
	hash := sha256.New()
	buf := make([]byte, chunkSize)
	var count int64
	for {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		n, err := f.Read(buf)
		count += int64(n)
		if count > r.manifest.Bytes {
			return "", errors.New("transfer changed during verification")
		}
		_, _ = hash.Write(buf[:n])
		if err == io.EOF {
			break
		}
		if err != nil {
			return "", err
		}
	}
	if count != r.manifest.Bytes || hex.EncodeToString(hash.Sum(nil)) != r.manifest.SHA256 {
		return "", errors.New("transferred artifact failed integrity verification")
	}
	if err := f.Close(); err != nil {
		return "", err
	}
	if source != complete {
		if err := os.Link(source, complete); err != nil {
			return "", fmt.Errorf("publish verified transfer: %w", err)
		}
	}
	return complete, nil
}
