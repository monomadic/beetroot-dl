package beatport

import (
	"sync"
	"testing"
	"time"
)

// Concurrent readers hitting a valid token must not race. Run with -race.
func TestAccessTokenConcurrentReads(t *testing.T) {
	a := NewAuth("user", "pass", t.TempDir()+"/cache.json")
	a.tokenPair = &tokenPair{
		AccessToken: "token",
		IssuedAt:    time.Now().Unix(),
		ExpiresIn:   3600,
	}

	var wg sync.WaitGroup
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if got := a.AccessToken(); got != "token" {
				t.Errorf("AccessToken() = %q, want %q", got, "token")
			}
			a.expiringSoon()
		}()
	}
	wg.Wait()
}

func TestAccessTokenWithoutTokenPair(t *testing.T) {
	a := NewAuth("user", "pass", t.TempDir()+"/cache.json")
	if got := a.AccessToken(); got != "" {
		t.Errorf("AccessToken() = %q, want empty string", got)
	}
	// A missing token counts as expiring, so Check will fetch one.
	if !a.expiringSoon() {
		t.Error("expiringSoon() = false with no token pair, want true")
	}
}

// Invalidate must be safe before any token has been issued — it used to
// dereference a nil tokenPair.
func TestInvalidateWithoutTokenPair(t *testing.T) {
	a := NewAuth("user", "pass", t.TempDir()+"/cache.json")
	a.Invalidate()
}

func TestExpiringSoon(t *testing.T) {
	now := time.Now().Unix()
	cases := map[string]struct {
		issuedAt, expiresIn int64
		want                bool
	}{
		"fresh":            {now, 3600, false},
		"expired":          {now - 7200, 3600, true},
		"within 5m window": {now - 3500, 3600, true},
		"invalidated":      {0, 3600, true},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			a := NewAuth("user", "pass", t.TempDir()+"/cache.json")
			a.tokenPair = &tokenPair{IssuedAt: tc.issuedAt, ExpiresIn: tc.expiresIn}
			if got := a.expiringSoon(); got != tc.want {
				t.Errorf("expiringSoon() = %v, want %v", got, tc.want)
			}
		})
	}
}
