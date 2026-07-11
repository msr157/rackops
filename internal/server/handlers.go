package server

import (
	"encoding/json"
	"net/http"
	"sort"
	"strings"

	"github.com/msr157/rackops/internal/api"
)

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

func (s *Server) handleReport(w http.ResponseWriter, r *http.Request) {
	var report api.ReportRequest
	if err := json.NewDecoder(r.Body).Decode(&report); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body: "+err.Error())
		return
	}
	if report.NodeID == "" {
		writeError(w, http.StatusBadRequest, "nodeId is required")
		return
	}
	s.store.Upsert(report)
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleListNodes(w http.ResponseWriter, _ *http.Request) {
	nodes := s.store.List()
	sort.Slice(nodes, func(i, j int) bool { return nodes[i].Hostname < nodes[j].Hostname })
	writeJSON(w, http.StatusOK, nodes)
}

func (s *Server) handleGetNode(w http.ResponseWriter, r *http.Request) {
	node, ok := s.store.Get(r.PathValue("id"))
	if !ok {
		writeError(w, http.StatusNotFound, "unknown node")
		return
	}
	writeJSON(w, http.StatusOK, node)
}

func (s *Server) handleMeta(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, api.Meta{Edition: s.opts.Edition, Version: Version})
}

func handleHealthz(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok", "service": "rackops-control-plane"})
}

func (s *Server) handleEarlyAccess(w http.ResponseWriter, r *http.Request) {
	var req api.EarlyAccessRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body: "+err.Error())
		return
	}

	req.Email = strings.TrimSpace(req.Email)
	req.Name = strings.TrimSpace(req.Name)
	req.Industry = strings.TrimSpace(req.Industry)

	if req.Email == "" || req.Name == "" || req.Industry == "" {
		writeError(w, http.StatusBadRequest, "email, name, and industry are required")
		return
	}

	if err := s.store.AddEarlyAccessRequest(req); err != nil {
		if strings.Contains(err.Error(), "unique constraint") || strings.Contains(err.Error(), "duplicate key") {
			writeError(w, http.StatusConflict, "this email has already requested early access")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to record request")
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "recorded"})
}
