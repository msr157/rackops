package server

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestExportReplyPreservesBytesAndOriginal(t *testing.T) {
	dir := t.TempDir()
	store, err := OpenStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	body := strings.Repeat("界\x00reply\n", 100000)
	for _, author := range []string{"agent", "note"} {
		_, err = store.db.Exec(`INSERT INTO ticket_messages(id,ticket_id,install_id,author,body,at) VALUES(?,?,?,?,?,?)`, author, "ticket", "install", author, body, time.Now())
		if err != nil {
			t.Fatal(err)
		}
	}
	path := filepath.Join(dir, "export.txt")
	digest, err := ExportAgentReply(context.Background(), dir, "install", "ticket", "agent", path)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	sum := sha256.Sum256([]byte(body))
	if err != nil || string(raw) != body || digest != hex.EncodeToString(sum[:]) {
		t.Fatal("export bytes changed")
	}
	if _, err = ExportAgentReply(context.Background(), dir, "install", "ticket", "agent", path); err == nil {
		t.Fatal("overwrote output")
	}
	for _, ids := range [][]string{{"other", "ticket", "agent"}, {"install", "other", "agent"}, {"install", "ticket", "note"}} {
		missing := filepath.Join(dir, "missing.txt")
		if _, err = ExportAgentReply(context.Background(), dir, ids[0], ids[1], ids[2], missing); err == nil {
			t.Fatal("wrong scope or note exported")
		}
		if _, err = os.Stat(missing); !os.IsNotExist(err) {
			t.Fatal("failure created output")
		}
	}
	var original string
	if err = store.db.QueryRow(`SELECT body FROM ticket_messages WHERE id='agent'`).Scan(&original); err != nil || original != body {
		t.Fatal("original modified")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	cancelled := filepath.Join(dir, "cancelled.txt")
	if _, err = ExportAgentReply(ctx, dir, "install", "ticket", "agent", cancelled); err == nil {
		t.Fatal("cancel ignored")
	}
	if _, err = os.Stat(cancelled); !os.IsNotExist(err) {
		t.Fatal("cancel created output")
	}
}

func TestExportReplyDoesNotCreateDatabase(t *testing.T) {
	dir := t.TempDir()
	if _, err := ExportAgentReply(context.Background(), dir, "install", "ticket", "agent", filepath.Join(dir, "out")); err == nil {
		t.Fatal("missing database accepted")
	}
	if _, err := os.Stat(filepath.Join(dir, "crm.db")); !os.IsNotExist(err) {
		t.Fatal("created database")
	}
}
