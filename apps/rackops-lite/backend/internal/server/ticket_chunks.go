package server

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/rackops/rackops/internal/types"
)

func (a *API) pollReplyChunk(id, secret string, cursor int64) {
	offset := 0
	a.Store.View(func() {
		if t := a.Store.Tickets[id]; t != nil && t.ReplyAssembly != nil {
			offset = len(t.ReplyAssembly.Data)
		}
	})
	url := fmt.Sprintf("%s/ingest/v1/tickets/%s/replies?installId=%s&cursor=%d&chunk=1&offset=%d", crmURL(), id, a.InstallID, cursor, offset)
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return
	}
	req.Header.Set("X-Poll-Secret", secret)
	resp, err := crmClient.Do(req)
	if err != nil {
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return
	}
	var out struct {
		Chunk *struct {
			Sequence int64  `json:"sequence"`
			Offset   int    `json:"offset"`
			Total    int    `json:"total"`
			SHA256   string `json:"sha256"`
			Data     []byte `json:"data"`
		} `json:"chunk"`
	}
	if json.NewDecoder(io.LimitReader(resp.Body, 512<<10)).Decode(&out) != nil || out.Chunk == nil {
		return
	}
	c := out.Chunk
	if c.Sequence <= cursor || c.Offset != offset || c.Total <= 0 || c.Total > 64<<20 || len(c.SHA256) != 64 || len(c.Data) == 0 || len(c.Data) > 256<<10 || offset > c.Total-len(c.Data) {
		return
	}
	var updated *types.SupportTicket
	a.Store.Mutate(func() {
		t := a.Store.Tickets[id]
		if t == nil || t.PollSecret != secret || t.ReplyCursor != cursor {
			return
		}
		if t.ReplyAssembly == nil {
			if offset != 0 {
				return
			}
			t.ReplyAssembly = &types.ReplyAssembly{Sequence: c.Sequence, Total: c.Total, SHA256: c.SHA256}
		}
		part := t.ReplyAssembly
		if len(part.Data) != offset {
			return
		}
		if part.Sequence != c.Sequence || part.Total != c.Total || part.SHA256 != c.SHA256 {
			t.ReplyAssembly = nil
			return
		}
		part.Data = append(part.Data, c.Data...)
		t.CRMRevision = nil
		t.ReplySyncError = "Recovering a large historical support reply; progress is saved automatically."
		if len(part.Data) == part.Total {
			sum := sha256.Sum256(part.Data)
			var m types.TicketMessage
			if hex.EncodeToString(sum[:]) != part.SHA256 || json.Unmarshal(part.Data, &m) != nil || m.Author != "agent" || m.ID == "" {
				t.ReplyAssembly = nil
				t.ReplySyncError = "Support reply integrity check failed; recovery will retry."
				return
			}
			exists := false
			for _, old := range t.Messages {
				if old.ID == m.ID {
					exists = true
					break
				}
			}
			if !exists {
				t.Messages = append(t.Messages, m)
			}
			t.ReplyCursor = part.Sequence
			t.ReplyAssembly = nil
			t.ReplySyncError = ""
			t.UpdatedAt = time.Now()
			updated = t.Sanitized()
		}
	})
	if a.Store.persistOnce() != nil {
		return
	}
	if updated != nil {
		a.Hub.Broadcast("ticket_update", updated)
	}
}
