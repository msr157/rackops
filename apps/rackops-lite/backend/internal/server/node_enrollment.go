package server

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"github.com/rackops/rackops/internal/types"
	"net/http"
	"regexp"
	"strings"
	"time"
)

type NodeEnrollment struct {
	OrgID     string    `json:"orgId"`
	ExpiresAt time.Time `json:"expiresAt"`
	NodeID    string    `json:"nodeId,omitempty"`
}
type NodeCredential struct {
	OrgID string `json:"orgId"`
	Hash  string `json:"hash"`
}

var enrolledNodeID = regexp.MustCompile(`^[a-zA-Z0-9_-]{6,64}$`)
var nodeSecret = regexp.MustCompile(`^rknode_[0-9a-f]{64}$`)

func (a *API) handleCreateNodeEnrollment(w http.ResponseWriter, r *http.Request) {
	org := a.cloudOrg(w, r)
	if org == nil {
		return
	}
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		http.Error(w, "could not generate enrollment", 500)
		return
	}
	proof := "rkenroll_" + hex.EncodeToString(raw)
	expires := time.Now().UTC().Add(time.Hour)
	a.Store.Mutate(func() {
		if a.Store.NodeEnrollments == nil {
			a.Store.NodeEnrollments = map[string]*NodeEnrollment{}
		}
		a.Store.NodeEnrollments[sha256Hex(proof)] = &NodeEnrollment{OrgID: org.ID, ExpiresAt: expires}
	})
	if err := a.Store.persistOnce(); err != nil {
		http.Error(w, "could not persist enrollment", 503)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, map[string]any{"token": proof, "orgId": org.ID, "expiresAt": expires})
}

func (a *API) handleNodeEnroll(w http.ResponseWriter, r *http.Request) {
	var req struct {
		NodeID     string `json:"nodeId"`
		Credential string `json:"credential"`
	}
	if json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&req) != nil || !enrolledNodeID.MatchString(req.NodeID) || !nodeSecret.MatchString(req.Credential) {
		http.Error(w, "invalid enrollment request", 400)
		return
	}
	a.nodeEnrollmentMu.Lock()
	defer a.nodeEnrollmentMu.Unlock()
	hash := sha256Hex(req.Credential)
	orgID := ""
	denied := true
	a.Store.Mutate(func() {
		// Persisting the client-chosen node credential before exchange permits a
		// retry of that same identity after a lost response, even after expiry.
		if credential := a.Store.NodeCredentials[req.NodeID]; credential != nil {
			node := a.Store.Nodes[req.NodeID]
			if node != nil && node.OrgID == credential.OrgID && a.Store.Orgs[credential.OrgID] != nil && subtle.ConstantTimeCompare([]byte(credential.Hash), []byte(hash)) == 1 {
				orgID = credential.OrgID
				denied = false
			}
			return
		}
		grant := a.Store.NodeEnrollments[sha256Hex(r.Header.Get("X-Rackops-Token"))]
		if grant == nil || grant.NodeID != "" || !time.Now().Before(grant.ExpiresAt) || a.Store.Orgs[grant.OrgID] == nil || a.Store.Nodes[req.NodeID] != nil {
			return
		}
		if a.Store.NodeCredentials == nil {
			a.Store.NodeCredentials = map[string]*NodeCredential{}
		}
		a.Store.NodeCredentials[req.NodeID] = &NodeCredential{OrgID: grant.OrgID, Hash: hash}
		a.Store.Nodes[req.NodeID] = &types.Node{ID: req.NodeID, OrgID: grant.OrgID, Role: types.RoleWorker, Status: types.NodeJoining, JoinedAt: time.Now().UTC()}
		grant.NodeID = req.NodeID
		orgID = grant.OrgID
		denied = false
	})
	if denied {
		http.Error(w, "enrollment expired, consumed, or identity unavailable", 403)
		return
	}
	if err := a.Store.persistOnce(); err != nil {
		http.Error(w, "could not persist node identity; retry the same enrollment", 503)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, map[string]string{"nodeId": req.NodeID, "orgId": orgID})
}

func (a *API) agentAuthorized(nodeID, token string) bool {
	valid := false
	a.Store.View(func() { valid = a.agentAuthorizedLocked(nodeID, token) })
	return valid
}

func (a *API) agentAuthorizedLocked(nodeID, token string) bool {
	valid, scoped := false, false
	func() {
		if c := a.Store.NodeCredentials[nodeID]; c != nil {
			scoped = true
			node := a.Store.Nodes[nodeID]
			valid = node != nil && node.OrgID == c.OrgID && a.Store.Orgs[c.OrgID] != nil && subtle.ConstantTimeCompare([]byte(c.Hash), []byte(sha256Hex(token))) == 1
		}
	}()
	if scoped {
		return valid
	}
	if strings.HasPrefix(token, "rknode_") || strings.HasPrefix(token, "rkenroll_") {
		return false
	}
	return a.JoinToken == "" || subtle.ConstantTimeCompare([]byte(token), []byte(a.JoinToken)) == 1
}
