package server

import (
	"strings"
	"sync"
	"testing"
	"time"
)

func TestClientChosenTicketCredentialSurvivesRetry(t *testing.T) {
	e := newTestEnv(t)
	proof := strings.Repeat("a", 64)
	for i := 0; i < 2; i++ {
		response, secret := e.forwardTicket(t, "durable-ticket", proof, "message-one")
		if response.StatusCode != 200 || secret != proof {
			t.Fatal("client credential not preserved")
		}
	}
	ticket, err := e.api.Store.GetTicket("durable-ticket", testInstallID)
	if err != nil || len(ticket.Messages) != 1 {
		t.Fatal("retry duplicated ticket messages")
	}
	response, _ := e.forwardTicket(t, "durable-ticket", strings.Repeat("b", 64), "attacker-message")
	if response.StatusCode != 403 {
		t.Fatal("another credential changed the ticket")
	}
	stored, err := e.api.Store.ticketPollHash("durable-ticket", testInstallID)
	if err != nil || stored != hashSecret(proof) || stored == proof {
		t.Fatal("poll credential not hashed")
	}
}

func TestConcurrentTicketClaimsAreAuthorizedInsideTransaction(t *testing.T) {
	e := newTestEnv(t)
	if err := e.api.Store.UpsertInstall(testInstallID, "fixture", "fixture", nil, time.Now()); err != nil {
		t.Fatal(err)
	}
	start := make(chan struct{})
	var wg sync.WaitGroup
	results := make(chan error, 2)
	for _, proof := range []string{strings.Repeat("a", 64), strings.Repeat("b", 64)} {
		wg.Add(1)
		go func(proof string) {
			defer wg.Done()
			<-start
			_, _, err := e.api.Store.UpsertTicket(&Ticket{ID: "claim", InstallID: testInstallID, Subject: "fixture", Category: "general", Severity: "normal", Email: "fixture@example.test"}, time.Now(), proof)
			results <- err
		}(proof)
	}
	close(start)
	wg.Wait()
	close(results)
	winners, denied := 0, 0
	for err := range results {
		if err == nil {
			winners++
		} else if err == errTicketCredential {
			denied++
		} else {
			t.Fatal(err)
		}
	}
	if winners != 1 || denied != 1 {
		t.Fatal("concurrent claim bypassed credential validation")
	}
}
