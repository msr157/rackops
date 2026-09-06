package agent

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/rackops/rackops/internal/types"
)

// taskJournal records intent before any external side effect. An interrupted
// execution is reported for operator reconciliation, never replayed blindly.
// Records are retained as tombstones, including after result acknowledgement.
type taskJournal struct {
	dir    string
	cipher cipher.AEAD
}
type taskReceipt struct {
	Fingerprint string            `json:"fingerprint"`
	Result      *types.TaskResult `json:"result,omitempty"`
}

func openTaskJournal(dataDir string) (*taskJournal, error) {
	dir := filepath.Join(dataDir, "task-journal")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	keyPath := filepath.Join(dir, "key")
	key, err := os.ReadFile(keyPath)
	if os.IsNotExist(err) {
		entries, readErr := os.ReadDir(dir)
		if readErr != nil {
			return nil, readErr
		}
		if len(entries) != 0 {
			return nil, fmt.Errorf("task journal key missing; restore its original key")
		}
		key = make([]byte, 32)
		if _, err = rand.Read(key); err != nil {
			return nil, err
		}
		var f *os.File
		f, err = os.OpenFile(keyPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if err != nil {
			return nil, err
		}
		if _, err = f.Write(key); err == nil {
			err = f.Sync()
		}
		closeErr := f.Close()
		if err != nil {
			return nil, err
		}
		if closeErr != nil {
			return nil, closeErr
		}
	} else if err != nil {
		return nil, err
	}
	if len(key) != 32 {
		return nil, fmt.Errorf("task journal key must be 32 bytes")
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCMWithRandomNonce(block)
	if err != nil {
		return nil, err
	}
	return &taskJournal{dir: dir, cipher: aead}, nil
}

func (j *taskJournal) save(path string, receipt taskReceipt) error {
	plain, err := json.Marshal(receipt)
	if err != nil {
		return err
	}
	sealed := j.cipher.Seal(nil, nil, plain, []byte(filepath.Base(path)))
	f, err := os.CreateTemp(j.dir, ".receipt-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if err = f.Chmod(0o600); err == nil {
		_, err = f.Write(sealed)
	}
	if err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	return os.Rename(f.Name(), path)
}

func (j *taskJournal) execute(ctx context.Context, task types.AgentTask, run func(context.Context, types.AgentTask) *types.TaskResult) *types.TaskResult {
	fail := func(message string) *types.TaskResult {
		return &types.TaskResult{TaskID: task.ID, ServiceID: task.ServiceID, Log: message}
	}
	uncertain := func(message string) *types.TaskResult {
		r := fail(message)
		r.Uncertain = true
		return r
	}
	name := sha256.Sum256([]byte(task.ID))
	path := filepath.Join(j.dir, hex.EncodeToString(name[:])+".receipt")
	payload, _ := json.Marshal(task)
	digest := sha256.Sum256(payload)
	fingerprint := hex.EncodeToString(digest[:])
	data, err := os.ReadFile(path)
	if err == nil {
		plain, err := j.cipher.Open(nil, nil, data, []byte(filepath.Base(path)))
		var receipt taskReceipt
		if err != nil || json.Unmarshal(plain, &receipt) != nil {
			return uncertain("task journal integrity failure; inspect the machine and restore the journal before retrying")
		}
		if receipt.Fingerprint != fingerprint {
			return uncertain("task ID reused with different content; execution refused")
		}
		if receipt.Result != nil {
			return receipt.Result
		}
		return uncertain("task execution was interrupted; outcome is uncertain. Inspect the machine before starting a new operation")
	}
	if !os.IsNotExist(err) {
		return fail("task journal unavailable; execution refused")
	}
	receipt := taskReceipt{Fingerprint: fingerprint}
	if err := j.save(path, receipt); err != nil {
		return fail("could not persist task intent; execution refused")
	}
	result := run(ctx, task)
	if result == nil {
		result = &types.TaskResult{TaskID: task.ID, ServiceID: task.ServiceID, OK: true}
	}
	if ctx.Err() != nil {
		result = uncertain("task deadline or shutdown interrupted execution; inspect the machine before starting a new operation")
	}
	receipt.Result = result
	if err := j.save(path, receipt); err != nil {
		return uncertain("task ran but its result could not be saved; outcome is uncertain. Inspect the machine before starting a new operation")
	}
	return result
}
