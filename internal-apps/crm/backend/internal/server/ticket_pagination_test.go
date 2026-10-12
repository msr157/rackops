package server

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

func TestReplySequenceSurvivesTiesRestartAndBackdating(t *testing.T) {
	dir := t.TempDir()
	s, err := OpenStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	_, _, err = s.UpsertTicket(&Ticket{ID: "thread", InstallID: testInstallID}, now, strings.Repeat("a", 64))
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 105; i++ {
		if err := s.AddMessage("thread", testInstallID, TicketMessage{ID: fmt.Sprint(i), Author: "agent", Body: "reply", At: now}); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.AddMessage("thread", testInstallID, TicketMessage{ID: "private", Author: "note", Body: "private", At: now}); err != nil {
		t.Fatal(err)
	}
	_, page, cursor, err := s.AgentRepliesPage("thread", testInstallID, 0)
	if err != nil || len(page) != 100 {
		t.Fatalf("first page: %d %v", len(page), err)
	}
	s.Close()
	s, err = OpenStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err := s.AddMessage("thread", testInstallID, TicketMessage{ID: "late", Author: "agent", Body: "backdated", At: now.Add(-time.Hour)}); err != nil {
		t.Fatal(err)
	}
	_, page, next, err := s.AgentRepliesPage("thread", testInstallID, cursor)
	if err != nil || len(page) != 6 || page[5].ID != "late" || next <= cursor {
		t.Fatalf("second page: %+v %v", page, err)
	}
	_, empty, same, err := s.AgentRepliesPage("thread", testInstallID, next)
	if err != nil || len(empty) != 0 || same != next {
		t.Fatal("empty page changed cursor")
	}
	_, foreign, _, _ := s.AgentRepliesPage("thread", "another-install", 0)
	if len(foreign) != 0 {
		t.Fatal("cross-install disclosure")
	}
}

func TestReplySequenceBackfillsOlderDatabase(t *testing.T) {
	s, err := OpenStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if _, err := s.db.Exec(`DROP TRIGGER ticket_message_delivery_insert; DROP TABLE ticket_message_delivery;`); err != nil {
		t.Fatal(err)
	}
	_, _, err = s.UpsertTicket(&Ticket{ID: "legacy", InstallID: testInstallID}, time.Now(), strings.Repeat("a", 64))
	if err != nil {
		t.Fatal(err)
	}
	if err := s.AddMessage("legacy", testInstallID, TicketMessage{ID: "old", Author: "agent", Body: "existing reply", At: time.Now()}); err != nil {
		t.Fatal(err)
	}
	if err := migrate(s.db); err != nil {
		t.Fatal(err)
	}
	_, page, cursor, err := s.AgentRepliesPage("legacy", testInstallID, 0)
	if err != nil || len(page) != 1 || cursor == 0 {
		t.Fatal("legacy reply was not backfilled", err)
	}
	if err := migrate(s.db); err != nil {
		t.Fatal(err)
	}
	_, again, same, err := s.AgentRepliesPage("legacy", testInstallID, 0)
	if err != nil || len(again) != 1 || same != cursor {
		t.Fatal("migration changed existing cursor", err)
	}
}
