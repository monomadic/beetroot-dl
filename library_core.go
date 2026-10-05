//go:build beetroot_library

package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"

	"github.com/monomadic/beetroot-dl/config"
	"github.com/monomadic/beetroot-dl/internal/beatport"
)

// One persistent client and connection pool. Operations are serialized per
// client because naming/existing-file handling must not race with itself.
type libraryClient struct {
	config config.AppConfig
	bp     *beatport.Beatport
	gate   chan struct{}
}

type libraryRequest struct {
	client      *libraryClient
	ctx         context.Context
	cancel      context.CancelFunc
	started     atomic.Bool
	mu          sync.Mutex
	state       string
	done, total int64
}

func openLibrary(configPath, downloadsDir string) (*libraryClient, error) {
	if configPath == "" {
		var exists bool
		var err error
		configPath, exists, err = FindConfigFile()
		if err != nil {
			return nil, err
		}
		if !exists {
			return nil, fmt.Errorf("config missing: %s (configure beetroot-dl first)", configPath)
		}
	}
	cfg, err := config.Parse(configPath)
	if err != nil {
		return nil, err
	}
	if downloadsDir != "" {
		cfg.DownloadsDirectory, err = filepath.Abs(downloadsDir)
		if err != nil {
			return nil, err
		}
	}
	cfg.ShowProgress = false
	cachePath, _, err := FindCacheFile()
	if err != nil {
		return nil, err
	}
	auth := beatport.NewAuth(cfg.Username, cfg.Password, cachePath)
	// No network during construction. Missing/expired tokens are handled by
	// Auth.Check on the first request, under that request's cancellation context.
	_ = auth.LoadCache()
	bp, err := beatport.New(cfg.Proxy, auth)
	if err != nil {
		return nil, err
	}
	return &libraryClient{config: *cfg, bp: bp.WithDiagnostics(io.Discard), gate: make(chan struct{}, 1)}, nil
}

func newLibraryRequest(client *libraryClient) *libraryRequest {
	ctx, cancel := context.WithCancel(context.Background())
	return &libraryRequest{client: client, ctx: ctx, cancel: cancel, state: "ready", total: -1}
}

func (r *libraryRequest) setState(state string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.state = state
}

func (r *libraryRequest) progress(done, total int64) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.done, r.total = done, total
}

func (r *libraryRequest) snapshot() any {
	r.mu.Lock()
	defer r.mu.Unlock()
	return struct {
		State      string `json:"state"`
		BytesDone  int64  `json:"bytes_done"`
		BytesTotal int64  `json:"bytes_total"`
	}{r.state, r.done, r.total}
}

func (r *libraryRequest) run(operation func(*beatport.Beatport) (any, error)) (value any, err error) {
	if !r.started.CompareAndSwap(false, true) {
		return nil, errors.New("request already used")
	}
	r.setState("waiting")
	defer func() {
		if recovered := recover(); recovered != nil {
			value, err = nil, errors.New("internal library panic")
		}
		if r.ctx.Err() != nil {
			value, err = nil, r.ctx.Err()
			r.setState("cancelled")
		} else if err != nil {
			r.setState("failed")
		} else {
			r.setState("complete")
		}
	}()
	select {
	case r.client.gate <- struct{}{}:
		defer func() { <-r.client.gate }()
	case <-r.ctx.Done():
		return nil, r.ctx.Err()
	}
	if err := r.ctx.Err(); err != nil {
		return nil, err
	}
	r.setState("running")
	return operation(r.client.bp.WithContext(r.ctx))
}

func (r *libraryRequest) search(query string) (any, error) {
	return r.run(func(bp *beatport.Beatport) (any, error) {
		result, err := bp.Search(query)
		if err != nil {
			return nil, err
		}
		hits := make([]searchHit, 0, len(result.Tracks))
		for _, track := range result.Tracks {
			hits = append(hits, searchTrack(track, &r.client.config))
		}
		return hits, nil
	})
}

func (r *libraryRequest) download(url string) (any, error) {
	return r.run(func(bp *beatport.Beatport) (any, error) {
		link, err := bp.ParseUrl(url)
		if err != nil {
			return nil, err
		}
		if link.Type != beatport.TrackLink {
			return nil, errors.New("prototype accepts single track URLs only")
		}
		cfg := r.client.config
		// This prototype supports direct FLAC/AAC. HLS also invokes ffmpeg, whose
		// cancellation and progress need a separate embedding pass.
		if cfg.Quality == "medium-hls" {
			return nil, errors.New("HLS not supported by the static library prototype")
		}
		// Returning an existing file must still supply its path. The existing CLI
		// skip branch returns no path, so reject it until that contract is refactored.
		if cfg.TrackExists == "skip" {
			return nil, errors.New("prototype requires track_exists=update, overwrite or error")
		}
		app := &application{config: &cfg, ctx: r.ctx, bp: bp, logWriter: io.Discard,
			activeFiles: make(map[string]struct{}), progress: r.progress}
		track, err := bp.GetTrack(link.ID)
		if err != nil {
			return nil, err
		}
		release, err := bp.GetRelease(track.Release.ID)
		if err != nil {
			return nil, err
		}
		track.Release = *release
		dir, err := app.setupDownloadsDirectory(cfg.DownloadsDirectory, release)
		if err != nil {
			return nil, err
		}
		var cover string
		if app.requireCover(true, true) {
			cover, err = app.downloadCover(track.Release.Image, dir)
			if err != nil {
				return nil, err
			}
		}
		if err = app.handleTrack(track, dir, cover); err != nil {
			if cover != "" {
				_ = os.Remove(cover)
			}
			return nil, err
		}
		if err = app.handleCoverFile(cover); err != nil {
			return nil, err
		}
		if len(app.downloaded) != 1 {
			return nil, errors.New("download completed without exactly one path")
		}
		return app.downloaded[0], nil
	})
}

func libraryJSON(value any, err error) string {
	response := struct {
		OK    bool   `json:"ok"`
		Value any    `json:"value,omitempty"`
		Error string `json:"error,omitempty"`
	}{OK: err == nil, Value: value}
	if err != nil {
		response.Error = err.Error()
	}
	data, marshalErr := json.Marshal(response)
	if marshalErr != nil {
		return `{"ok":false,"error":"encode library result"}`
	}
	return string(data)
}
