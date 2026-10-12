package server

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"strconv"
)

// Called only after ticket credential validation. Chunk the complete serialized
// message, including metadata, so neither UTF-8 boundaries nor names are lost.
func (a *API) writeReplyChunk(w http.ResponseWriter, r *http.Request, ticketID, installID string) {
	cursor, err := strconv.ParseInt(r.URL.Query().Get("cursor"), 10, 64)
	if err != nil || cursor < 0 {
		writeError(w, 400, "invalid cursor")
		return
	}
	offset, err := strconv.Atoi(r.URL.Query().Get("offset"))
	if err != nil || offset < 0 {
		writeError(w, 400, "invalid chunk offset")
		return
	}
	var sequence int64
	var m TicketMessage
	err = a.Store.db.QueryRow(`SELECT d.sequence,m.id,m.author,m.name,m.body,m.at FROM ticket_message_delivery d JOIN ticket_messages m ON m.id=d.message_id AND m.ticket_id=d.ticket_id AND m.install_id=d.install_id WHERE d.ticket_id=? AND d.install_id=? AND d.sequence>? AND m.author='agent' ORDER BY d.sequence LIMIT 1`, ticketID, installID, cursor).Scan(&sequence, &m.ID, &m.Author, &m.Name, &m.Body, &m.At)
	if err != nil {
		serverError(w, "load reply chunk", err)
		return
	}
	raw, err := json.Marshal(m)
	if err != nil {
		serverError(w, "encode reply chunk", err)
		return
	}
	if len(raw) > 64<<20 {
		writeError(w, 422, "historical reply exceeds supported recovery size")
		return
	}
	if offset >= len(raw) {
		writeError(w, 400, "chunk offset outside message")
		return
	}
	sum := sha256.Sum256(raw)
	end := offset + (256 << 10)
	if end > len(raw) {
		end = len(raw)
	}
	writeJSON(w, map[string]any{"chunk": map[string]any{"sequence": sequence, "offset": offset, "total": len(raw), "sha256": hex.EncodeToString(sum[:]), "data": raw[offset:end]}})
}
