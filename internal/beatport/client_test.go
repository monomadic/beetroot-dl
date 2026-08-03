package beatport

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// testServer stands in for the Beatport API. It counts requests per path so a
// test can assert how many times an endpoint was hit.
type testServer struct {
	*httptest.Server
	mu     sync.Mutex
	counts map[string]int
	// catalogStatus is the status returned for the fake catalog endpoint.
	catalogStatus int
}

func newTestServer(t *testing.T) *testServer {
	t.Helper()
	ts := &testServer{counts: map[string]int{}, catalogStatus: http.StatusOK}

	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		ts.record(r.URL.Path)
		switch r.URL.Path {
		case tokenEndpoint:
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprintf(w, `{"access_token":"fresh-%d","refresh_token":"r","expires_in":36000,"token_type":"bearer"}`,
				ts.count(tokenEndpoint))
		default:
			if ts.catalogStatusCode() != http.StatusOK {
				w.WriteHeader(ts.catalogStatusCode())
				fmt.Fprint(w, `{"detail":"nope"}`)
				return
			}
			fmt.Fprint(w, `{"id":1,"name":"Strobe"}`)
		}
	})

	ts.Server = httptest.NewServer(mux)
	t.Cleanup(ts.Close)
	return ts
}

func (ts *testServer) record(path string) {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	ts.counts[path]++
}

func (ts *testServer) count(path string) int {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	return ts.counts[path]
}

func (ts *testServer) catalogStatusCode() int {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	return ts.catalogStatus
}

func (ts *testServer) setCatalogStatus(code int) {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	ts.catalogStatus = code
}

// newTestClient wires a client to the fake server with a token seeded directly,
// so tests control whether an auth refresh is due.
func newTestClient(t *testing.T, ts *testServer, expiresIn int64) (*Beatport, *Auth) {
	t.Helper()
	auth := NewAuth("user", "pass", filepath.Join(t.TempDir(), "creds.json"))
	auth.tokenPair = &tokenPair{
		AccessToken:  "seeded",
		RefreshToken: "seeded-refresh",
		ExpiresIn:    expiresIn,
		IssuedAt:     time.Now().Unix(),
	}
	bp, err := New("", auth)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	bp.baseUrl = ts.URL
	return bp, auth
}

// A persistent 401 must be retried exactly once and then surface as an error.
// Before this was capped, fetch recursed forever, re-authenticating each time.
func TestFetchRetriesUnauthorizedOnce(t *testing.T) {
	ts := newTestServer(t)
	ts.setCatalogStatus(http.StatusUnauthorized)
	bp, _ := newTestClient(t, ts, 36000)

	resp, err := bp.fetch("GET", "/catalog/tracks/1", nil, "")
	if err == nil {
		resp.Body.Close()
		t.Fatal("fetch() = nil error, want error after a persistent 401")
	}

	if got := ts.count("/catalog/tracks/1"); got != 2 {
		t.Errorf("catalog endpoint hit %d times, want 2 (initial + one retry)", got)
	}
}

// A 401 that clears after re-authentication should succeed on the retry.
func TestFetchSucceedsAfterReauth(t *testing.T) {
	ts := newTestServer(t)
	ts.setCatalogStatus(http.StatusUnauthorized)
	bp, _ := newTestClient(t, ts, 36000)

	// Let the first request 401, then let the endpoint recover.
	var once sync.Once
	original := ts.Server.Config.Handler
	ts.Server.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		original.ServeHTTP(w, r)
		// Recover only *after* the first catalog request has been answered,
		// so that request is the one that 401s.
		if r.URL.Path == "/catalog/tracks/1" {
			once.Do(func() { ts.setCatalogStatus(http.StatusOK) })
		}
	})

	resp, err := bp.fetch("GET", "/catalog/tracks/1", nil, "")
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
	defer resp.Body.Close()

	if got := ts.count("/catalog/tracks/1"); got != 2 {
		t.Errorf("catalog endpoint hit %d times, want 2", got)
	}
}

// Concurrent workers finding an expired token must produce exactly one refresh.
// Without the re-test under the write lock, every worker queued on the mutex
// refreshed in turn, each invalidating the token the previous one fetched.
func TestCheckRefreshesOnceUnderConcurrency(t *testing.T) {
	ts := newTestServer(t)
	// expiresIn 0 => already expired => every caller sees "expiring soon".
	bp, auth := newTestClient(t, ts, 0)

	const workers = 24
	var wg sync.WaitGroup
	var failures atomic.Int64
	start := make(chan struct{})

	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			if err := auth.Check(bp); err != nil {
				failures.Add(1)
			}
		}()
	}
	close(start)
	wg.Wait()

	if n := failures.Load(); n != 0 {
		t.Fatalf("%d workers failed Check()", n)
	}
	if got := ts.count(tokenEndpoint); got != 1 {
		t.Errorf("token endpoint hit %d times, want exactly 1", got)
	}
	if tok := auth.AccessToken(); tok != "fresh-1" {
		t.Errorf("AccessToken() = %q, want the refreshed token", tok)
	}
}

// The token, authorize and login endpoints must never recurse through the
// auth check — that is what keeps a failing login from looping.
func TestFetchDoesNotRetryAuthEndpoints(t *testing.T) {
	ts := newTestServer(t)
	ts.setCatalogStatus(http.StatusUnauthorized)
	bp, _ := newTestClient(t, ts, 36000)

	if _, err := bp.fetch("POST", loginEndpoint, map[string]string{"a": "b"}, "application/json"); err == nil {
		t.Fatal("fetch(login) = nil error, want error")
	}
	if got := ts.count(loginEndpoint); got != 1 {
		t.Errorf("login endpoint hit %d times, want 1 (no retry)", got)
	}
}
