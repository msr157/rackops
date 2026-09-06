package server

import (
	"crypto/rand"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"
)

// RecoverAccountSnapshot writes recovered state into a NEW directory. It never
// opens a running Store or modifies the source snapshot. Possession of both the
// encrypted snapshot and its separate key is the offline recovery authority.
func RecoverAccountSnapshot(target string, sealed, key []byte, email, password string) error {
	if len(password) < 8 || len(password) > 72 {
		return fmt.Errorf("password must contain 8 to 72 bytes")
	}
	plain, err := decryptState(key, sealed)
	if err != nil {
		return err
	}
	var snapshot Store
	if err = json.Unmarshal(plain, &snapshot); err != nil {
		return err
	}
	if snapshot.SchemaVersion != 1 || snapshot.Users == nil || snapshot.Projects == nil || snapshot.Services == nil {
		return fmt.Errorf("unsupported or incomplete recovery snapshot")
	}
	email = strings.ToLower(strings.TrimSpace(email))
	userID := ""
	for id, user := range snapshot.Users {
		if user != nil && strings.EqualFold(user.Email, email) {
			if userID != "" {
				return fmt.Errorf("ambiguous account in snapshot")
			}
			userID = id
		}
	}
	if userID == "" {
		return fmt.Errorf("account not found in snapshot")
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	user := snapshot.Users[userID]
	user.PasswordHash = string(hash)
	user.SessionVersion++
	user.ResetTokenHash = ""
	user.ResetExpires = time.Time{}
	delete(snapshot.ResetEmails, userID)
	snapshot.BootstrapToken = ""
	// Snapshot restoration may rewind session counters; a fresh signing key
	// invalidates all old sessions rather than resurrecting any old JWT.
	snapshot.JWTSecret = make([]byte, 32)
	if _, err = rand.Read(snapshot.JWTSecret); err != nil {
		return err
	}
	for id, token := range snapshot.Tokens {
		if token != nil && token.UserID == userID {
			delete(snapshot.Tokens, id)
		}
	}
	plain, err = json.Marshal(&snapshot)
	if err != nil {
		return err
	}
	recovered, err := encryptState(key, plain)
	if err != nil {
		return err
	}
	return RestoreStateSnapshot(target, recovered, key)
}
