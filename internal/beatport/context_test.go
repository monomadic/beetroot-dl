package beatport

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"
)

func TestSearchContextInterruptsInFlightHTTP(t *testing.T) {
	started := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(started)
		<-r.Context().Done()
	}))
	defer server.Close()
	auth := NewAuth("fixture", "fixture", filepath.Join(t.TempDir(), "auth.json"))
	auth.tokenPair = &tokenPair{AccessToken: "fixture", ExpiresIn: 3600, IssuedAt: time.Now().Unix()}
	bp, err := New("", auth)
	if err != nil {
		t.Fatal(err)
	}
	bp.baseUrl = server.URL
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	complete := make(chan error, 1)
	go func() { _, err := bp.WithContext(ctx).Search("fixture"); complete <- err }()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("request did not start")
	}
	cancel()
	select {
	case err := <-complete:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("got %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("HTTP cancellation did not return promptly")
	}
}
