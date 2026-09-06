package server

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// Binary framing deliberately fails JSON parsing in older RackOps versions;
// an older binary must not interpret an encrypted envelope as an empty store.
var stateMagic = []byte("RACKOPS_STATE_V1\n")

func stateKeyPath(dataDir string) string {
	if path := os.Getenv("RACKOPS_STATE_KEY_FILE"); path != "" {
		return path
	}
	return filepath.Join(dataDir, "state.key")
}

func loadStateKey(dataDir string, encrypted bool) ([]byte, error) {
	path := stateKeyPath(dataDir)
	key, err := os.ReadFile(path)
	if err == nil {
		if len(key) != 32 {
			return nil, errors.New("state encryption key must be exactly 32 bytes")
		}
		return key, nil
	}
	if !os.IsNotExist(err) {
		return nil, fmt.Errorf("read state encryption key: %w", err)
	}
	if encrypted {
		return nil, errors.New("encrypted state requires its original state encryption key; refusing to initialize a new store")
	}
	key = make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return nil, fmt.Errorf("create state encryption key: %w", err)
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
	return key, nil
}

func stateCipher(key []byte) (cipher.AEAD, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}

func encryptState(key, data []byte) ([]byte, error) {
	gcm, err := stateCipher(key)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	out := append(append([]byte(nil), stateMagic...), nonce...)
	return gcm.Seal(out, nonce, data, stateMagic), nil
}

func decryptState(key, data []byte) ([]byte, error) {
	if !bytes.HasPrefix(data, stateMagic) {
		return nil, errors.New("unsupported encrypted state format")
	}
	gcm, err := stateCipher(key)
	if err != nil {
		return nil, err
	}
	data = data[len(stateMagic):]
	if len(data) < gcm.NonceSize()+gcm.Overhead() {
		return nil, errors.New("encrypted state is truncated")
	}
	plain, err := gcm.Open(nil, data[:gcm.NonceSize()], data[gcm.NonceSize():], stateMagic)
	if err != nil {
		return nil, errors.New("state integrity check failed: wrong key or damaged snapshot")
	}
	return plain, nil
}

// atomicStateWrite publishes only a completely written, synced snapshot.
// Failed writes leave the previously committed snapshot intact.
func atomicStateWrite(path string, data []byte) error {
	f, err := os.CreateTemp(filepath.Dir(path), ".rackops-state-*")
	if err != nil {
		return err
	}
	tmp := f.Name()
	defer os.Remove(tmp)
	if err = f.Chmod(0o600); err == nil {
		_, err = f.Write(data)
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
	return os.Rename(tmp, path)
}
