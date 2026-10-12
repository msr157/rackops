package server

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"io"
	"net/url"
	"os"
	"path/filepath"
)

// ExportAgentReply is a local operator escape hatch for oversized historical
// replies. It opens an existing database read-only and never updates delivery.
func ExportAgentReply(ctx context.Context, dataDir, installID, ticketID, messageID, output string) (string, error) {
	if installID == "" || ticketID == "" || messageID == "" || output == "" {
		return "", errors.New("install, ticket, message and output are required")
	}
	path, err := filepath.Abs(filepath.Join(dataDir, "crm.db"))
	if err != nil {
		return "", err
	}
	uri := url.URL{Scheme: "file", Path: filepath.ToSlash(path)}
	if len(uri.Path) > 1 && uri.Path[1] == ':' {
		uri.Path = "/" + uri.Path
	}
	uri.RawQuery = "mode=ro&_pragma=busy_timeout(5000)"
	db, err := sql.Open("sqlite", uri.String())
	if err != nil {
		return "", err
	}
	defer db.Close()
	tx, err := db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return "", err
	}
	defer tx.Rollback()
	var length int64
	err = tx.QueryRowContext(ctx, `SELECT length(CAST(body AS BLOB)) FROM ticket_messages WHERE install_id=? AND ticket_id=? AND id=? AND author='agent'`, installID, ticketID, messageID).Scan(&length)
	if err != nil {
		return "", errors.New("agent reply unavailable for the exact install, ticket and message")
	}
	f, err := os.OpenFile(output, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return "", err
	}
	complete := false
	defer func() {
		f.Close()
		if !complete {
			os.Remove(output)
		}
	}()
	sum := sha256.New()
	writer := io.MultiWriter(f, sum)
	for offset := int64(0); offset < length; {
		n := int64(256 << 10)
		if length-offset < n {
			n = length - offset
		}
		var chunk []byte
		if err = tx.QueryRowContext(ctx, `SELECT substr(CAST(body AS BLOB),?,?) FROM ticket_messages WHERE install_id=? AND ticket_id=? AND id=? AND author='agent'`, offset+1, n, installID, ticketID, messageID).Scan(&chunk); err != nil {
			return "", err
		}
		if int64(len(chunk)) != n {
			return "", errors.New("incomplete reply export")
		}
		if _, err = writer.Write(chunk); err != nil {
			return "", err
		}
		offset += n
	}
	if err = ctx.Err(); err != nil {
		return "", err
	}
	if err = f.Sync(); err != nil {
		return "", err
	}
	if err = f.Close(); err != nil {
		return "", err
	}
	complete = true
	return hex.EncodeToString(sum.Sum(nil)), nil
}
