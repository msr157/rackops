package server

import (
	"fmt"
	"strings"
	"sync"
	"testing"
)

func TestBootstrapRequiresProofAndCreatesOneOwner(t *testing.T) {
	a, _ := testAPI(t)
	proof := a.Store.BootstrapToken
	for _, path := range []string{"/api/nodes", "/api/services", "/api/settings", "/api/backups/state", "/relay/private/"} {
		r := do(a, "GET", path, "", nil)
		if r.Code != 403 {
			t.Fatalf("setup exposes %s: %d", path, r.Code)
		}
	}
	r := do(a, "GET", "/api/auth/status", "", nil)
	if r.Code != 200 || strings.Contains(r.Body.String(), proof) {
		t.Fatal("status missing or leaked setup credential")
	}
	r = do(a, "POST", "/api/auth/signup", `{"email":"o@test.local","password":"correct-horse"}`, nil)
	if r.Code != 403 {
		t.Fatalf("signup without proof: %d", r.Code)
	}
	var wg sync.WaitGroup
	codes := make(chan int, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			body := fmt.Sprintf(`{"email":"owner%d@test.local","password":"correct-horse","setupToken":%q}`, i, proof)
			codes <- do(a, "POST", "/api/auth/signup", body, nil).Code
		}(i)
	}
	wg.Wait()
	close(codes)
	success := 0
	for code := range codes {
		if code == 200 {
			success++
		} else if code != 403 {
			t.Fatalf("signup: %d", code)
		}
	}
	if success != 1 || len(a.Store.Users) != 1 || a.Store.BootstrapToken != "" {
		t.Fatal("owner bootstrap was not single-use")
	}
}
