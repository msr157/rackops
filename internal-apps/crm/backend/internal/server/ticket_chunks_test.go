package server

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestReplyChunkEndpointPreservesFullMessageAndScope(t *testing.T) {
	e := newTestEnv(t)
	secret := strings.Repeat("a", 64)
	e.forwardTicket(t, "chunks", secret, "initial")
	original := TicketMessage{ID: "old", Author: "agent", Body: strings.Repeat("界<", 100000), At: time.Now().UTC()}
	if err := e.api.Store.AddMessage("chunks", testInstallID, original); err != nil {
		t.Fatal(err)
	}
	var assembled []byte
	var digest string
	for i := 0; i < 10; i++ {
		req, _ := http.NewRequest("GET", fmt.Sprintf("%s/ingest/v1/tickets/chunks/replies?installId=%s&cursor=0&chunk=1&offset=%d", e.srv.URL, testInstallID, len(assembled)), nil)
		req.Header.Set("X-Poll-Secret", secret)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		var out struct {
			Chunk struct {
				Data   []byte `json:"data"`
				Total  int    `json:"total"`
				SHA256 string `json:"sha256"`
			} `json:"chunk"`
		}
		err = json.NewDecoder(resp.Body).Decode(&out)
		resp.Body.Close()
		if err != nil || resp.StatusCode != 200 || len(out.Chunk.Data) > 256<<10 {
			t.Fatal("chunk request failed", err)
		}
		assembled = append(assembled, out.Chunk.Data...)
		digest = out.Chunk.SHA256
		if len(assembled) == out.Chunk.Total {
			break
		}
	}
	sum := sha256.Sum256(assembled)
	var restored TicketMessage
	if hex.EncodeToString(sum[:]) != digest || json.Unmarshal(assembled, &restored) != nil || restored.Body != original.Body {
		t.Fatal("message integrity failed")
	}
	req, _ := http.NewRequest("GET", e.srv.URL+"/ingest/v1/tickets/chunks/replies?installId="+testInstallID+"&cursor=0&chunk=1&offset=0", nil)
	req.Header.Set("X-Poll-Secret", "wrong")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 403 {
		t.Fatal("chunk authentication bypass")
	}
}
