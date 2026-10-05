//go:build beetroot_library

package main

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/monomadic/beetroot-dl/internal/beatport"
)

func TestLibraryQueuedCancellationAndReuse(t *testing.T) {
	client := &libraryClient{bp: &beatport.Beatport{}, gate: make(chan struct{}, 1)}
	client.gate <- struct{}{}
	request := newLibraryRequest(client)
	defer request.cancel()
	complete := make(chan error, 1)
	go func() {
		_, err := request.run(func(*beatport.Beatport) (any, error) {
			return nil, errors.New("cancelled queued request executed")
		})
		complete <- err
	}()
	request.cancel()
	select {
	case err := <-complete:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("cancel: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("cancelled request remained blocked behind another request")
	}
	<-client.gate
	if _, err := request.search("must not run"); err == nil {
		t.Fatal("request was reused")
	}
}

func TestLibraryPanicIsReturnedAndClientRemainsUsable(t *testing.T) {
	client := &libraryClient{bp: &beatport.Beatport{}, gate: make(chan struct{}, 1)}
	request := newLibraryRequest(client)
	defer request.cancel()
	if _, err := request.run(func(*beatport.Beatport) (any, error) { panic("fixture") }); err == nil {
		t.Fatal("panic escaped error boundary")
	}
	next := newLibraryRequest(client)
	defer next.cancel()
	if _, err := next.run(func(*beatport.Beatport) (any, error) { return "ok", nil }); err != nil {
		t.Fatal(err)
	}
}
