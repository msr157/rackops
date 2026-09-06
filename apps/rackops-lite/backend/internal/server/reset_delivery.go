package server

import "time"

// ResetEmail is persisted only inside encrypted control-plane state. Never
// return this structure through an API, event or diagnostic export.
type ResetEmail struct {
	UserID      string    `json:"userId"`
	Email       string    `json:"email"`
	Link        string    `json:"link"`
	TokenHash   string    `json:"tokenHash"`
	ExpiresAt   time.Time `json:"expiresAt"`
	NextAttempt time.Time `json:"nextAttempt"`
	Attempts    int       `json:"attempts"`
}

func (a *API) deliverResetEmail(userID string, now time.Time) {
	a.resetDeliveryMu.Lock()
	defer a.resetDeliveryMu.Unlock()
	var item ResetEmail
	ready := false
	a.Store.Mutate(func() {
		pending := a.Store.ResetEmails[userID]
		if pending == nil {
			return
		}
		user := a.Store.Users[userID]
		if user == nil || user.Email != pending.Email || user.ResetTokenHash != pending.TokenHash || !now.Before(user.ResetExpires) || !now.Before(pending.ExpiresAt) || pending.Attempts >= 6 {
			delete(a.Store.ResetEmails, userID)
			return
		}
		if now.Before(pending.NextAttempt) {
			return
		}
		pending.Attempts++
		pending.NextAttempt = now.Add(time.Minute * time.Duration(1<<min(pending.Attempts-1, 4)))
		item = *pending
		ready = true
	})
	// Persist the retry schedule before the external request. A lost response
	// can produce a duplicate email with the same link, never a new credential.
	if err := a.Store.persistOnce(); err != nil || !ready {
		return
	}
	if err := a.relayResetEmail(item.Email, item.Link); err == nil {
		a.Store.Mutate(func() {
			if pending := a.Store.ResetEmails[userID]; pending != nil && pending.TokenHash == item.TokenHash {
				delete(a.Store.ResetEmails, userID)
			}
		})
		_ = a.Store.persistOnce()
	}
}

func (a *API) retryResetEmails(now time.Time) {
	var users []string
	a.Store.View(func() {
		for id := range a.Store.ResetEmails {
			users = append(users, id)
		}
	})
	for _, id := range users {
		a.deliverResetEmail(id, now)
	}
}
