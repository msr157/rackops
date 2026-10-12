// Package migration contains the data and execution primitives used by workload migration.
package migration

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
)

const chunkSize = 1 << 20
const artifactMagic = "RKBACK01"

// ArtifactDigest describes the complete plaintext stream. Store it with the
// encrypted control-plane record, never in public telemetry.
type ArtifactDigest struct {
	Bytes  int64  `json:"bytes"`
	SHA256 string `json:"sha256"`
}

func artifactCipher(key, header []byte) (cipher.AEAD, error) {
	if len(key) != 32 {
		return nil, errors.New("backup key must contain 32 bytes")
	}
	// A random 256-bit salt derives a fresh encryption key for each artifact,
	// even when an installation reuses its protected backup master key.
	h := hmac.New(sha256.New, key)
	_, _ = h.Write(header)
	block, err := aes.NewCipher(h.Sum(nil))
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}

func frameAuth(header []byte, index uint64, size uint32) ([]byte, []byte) {
	nonce := make([]byte, 12)
	binary.BigEndian.PutUint64(nonce[4:], index)
	aad := make([]byte, len(header)+12)
	copy(aad, header)
	binary.BigEndian.PutUint64(aad[len(header):], index)
	binary.BigEndian.PutUint32(aad[len(header)+8:], size)
	return nonce, aad
}

func writeFull(w io.Writer, data []byte) error {
	n, err := w.Write(data)
	if err == nil && n != len(data) {
		err = io.ErrShortWrite
	}
	return err
}

// EncryptArtifact streams bounded chunks and an authenticated end marker.
// Callers must quiesce writers or supply a database-consistent dump first.
func EncryptArtifact(ctx context.Context, dst io.Writer, src io.Reader, key []byte) (ArtifactDigest, error) {
	var result ArtifactDigest
	if err := ctx.Err(); err != nil {
		return result, err
	}
	header := make([]byte, len(artifactMagic)+32)
	copy(header, artifactMagic)
	if _, err := rand.Read(header[len(artifactMagic):]); err != nil {
		return result, err
	}
	aead, err := artifactCipher(key, header)
	if err != nil {
		return result, err
	}
	if err := writeFull(dst, header); err != nil {
		return result, err
	}
	hash := sha256.New()
	buf := make([]byte, chunkSize)
	for index := uint64(0); ; index++ {
		if err := ctx.Err(); err != nil {
			return result, err
		}
		n, readErr := io.ReadFull(src, buf)
		if readErr != nil && readErr != io.EOF && readErr != io.ErrUnexpectedEOF {
			return result, readErr
		}
		size := uint32(n)
		nonce, aad := frameAuth(header, index, size)
		sealed := aead.Seal(nil, nonce, buf[:n], aad)
		var length [4]byte
		binary.BigEndian.PutUint32(length[:], size)
		if err := writeFull(dst, length[:]); err != nil {
			return result, err
		}
		if err := writeFull(dst, sealed); err != nil {
			return result, err
		}
		if n == 0 {
			break
		}
		_, _ = hash.Write(buf[:n])
		result.Bytes += int64(n)
	}
	result.SHA256 = hex.EncodeToString(hash.Sum(nil))
	return result, nil
}

// DecryptArtifact writes verified chunks to private staging storage. Nothing
// may consume that storage until this function and the expected digest check
// succeed: a valid prefix alone is not a complete backup. maxBytes is mandatory.
func DecryptArtifact(ctx context.Context, dst io.Writer, src io.Reader, key []byte, maxBytes int64) (ArtifactDigest, error) {
	var result ArtifactDigest
	if maxBytes < 0 {
		return result, errors.New("invalid backup size limit")
	}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	header := make([]byte, len(artifactMagic)+32)
	if _, err := io.ReadFull(src, header); err != nil {
		return result, err
	}
	if !bytes.Equal(header[:len(artifactMagic)], []byte(artifactMagic)) {
		return result, errors.New("unsupported backup format")
	}
	aead, err := artifactCipher(key, header)
	if err != nil {
		return result, err
	}
	hash := sha256.New()
	for index := uint64(0); ; index++ {
		if err := ctx.Err(); err != nil {
			return result, err
		}
		var length [4]byte
		if _, err := io.ReadFull(src, length[:]); err != nil {
			return result, fmt.Errorf("incomplete backup: %w", err)
		}
		size := binary.BigEndian.Uint32(length[:])
		if size > chunkSize || int64(size) > maxBytes-result.Bytes {
			return result, errors.New("backup exceeds size limit")
		}
		sealed := make([]byte, int(size)+aead.Overhead())
		if _, err := io.ReadFull(src, sealed); err != nil {
			return result, err
		}
		nonce, aad := frameAuth(header, index, size)
		plain, err := aead.Open(nil, nonce, sealed, aad)
		if err != nil {
			return result, errors.New("backup integrity verification failed")
		}
		if size == 0 {
			var extra [1]byte
			if _, err := io.ReadFull(src, extra[:]); err != io.EOF {
				return result, errors.New("unexpected data after backup end")
			}
			break
		}
		if err := writeFull(dst, plain); err != nil {
			return result, err
		}
		_, _ = hash.Write(plain)
		result.Bytes += int64(len(plain))
	}
	result.SHA256 = hex.EncodeToString(hash.Sum(nil))
	return result, nil
}
