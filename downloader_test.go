package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/monomadic/beetroot-dl/internal/beatport"
)

func TestUniqueFilePathSkipsExistingFiles(t *testing.T) {
	dir := t.TempDir()

	// The unsuffixed name is not considered: uniqueFilePath is only reached
	// when that one is already taken.
	got := uniqueFilePath(dir, "Strobe", ".flac")
	want := filepath.Join(dir, "Strobe (1).flac")
	if got != want {
		t.Fatalf("uniqueFilePath() = %q, want %q", got, want)
	}

	// Occupy (1) and (2); the next free slot is (3).
	for _, n := range []int{1, 2} {
		p := filepath.Join(dir, fmt.Sprintf("Strobe (%d).flac", n))
		if err := os.WriteFile(p, []byte("x"), 0600); err != nil {
			t.Fatalf("seed %s: %v", p, err)
		}
	}

	got = uniqueFilePath(dir, "Strobe", ".flac")
	want = filepath.Join(dir, "Strobe (3).flac")
	if got != want {
		t.Fatalf("uniqueFilePath() = %q, want %q", got, want)
	}
}

// Different extensions must not collide with each other.
func TestUniqueFilePathIsPerExtension(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "Strobe (1).flac"), []byte("x"), 0600); err != nil {
		t.Fatalf("seed: %v", err)
	}

	got := uniqueFilePath(dir, "Strobe", ".m4a")
	want := filepath.Join(dir, "Strobe (1).m4a")
	if got != want {
		t.Fatalf("uniqueFilePath() = %q, want %q", got, want)
	}
}

// pagedFetcher serves a fixed number of pages of ids.
func pagedFetcher(pages [][]int) func(id int64, page int, params string) (*beatport.Paginated[int], error) {
	return func(id int64, page int, params string) (*beatport.Paginated[int], error) {
		if page < 1 || page > len(pages) {
			return nil, fmt.Errorf("page %d out of range", page)
		}
		p := &beatport.Paginated[int]{Results: pages[page-1]}
		if page < len(pages) {
			next := "more"
			p.Next = &next
		}
		return p, nil
	}
}

func TestForPaginatedVisitsEveryPage(t *testing.T) {
	pages := [][]int{{1, 2}, {3, 4}, {5}}

	var seen []int
	err := ForPaginated(int64(7), "", pagedFetcher(pages), func(item int, i int) error {
		seen = append(seen, item)
		return nil
	})
	if err != nil {
		t.Fatalf("ForPaginated: %v", err)
	}

	want := []int{1, 2, 3, 4, 5}
	if len(seen) != len(want) {
		t.Fatalf("visited %v, want %v", seen, want)
	}
	for i := range want {
		if seen[i] != want[i] {
			t.Fatalf("visited %v, want %v", seen, want)
		}
	}
}

// A single page with no Next must not be requested twice.
func TestForPaginatedStopsWithoutNext(t *testing.T) {
	calls := 0
	fetch := func(id int64, page int, params string) (*beatport.Paginated[int], error) {
		calls++
		return &beatport.Paginated[int]{Results: []int{1}}, nil
	}

	if err := ForPaginated(int64(1), "", fetch, func(item int, i int) error { return nil }); err != nil {
		t.Fatalf("ForPaginated: %v", err)
	}
	if calls != 1 {
		t.Errorf("fetched %d pages, want 1", calls)
	}
}

func TestForPaginatedPropagatesFetchError(t *testing.T) {
	sentinel := errors.New("boom")
	fetch := func(id int64, page int, params string) (*beatport.Paginated[int], error) {
		return nil, sentinel
	}

	err := ForPaginated(int64(1), "", fetch, func(item int, i int) error { return nil })
	if !errors.Is(err, sentinel) {
		t.Fatalf("err = %v, want it to wrap %v", err, sentinel)
	}
}

// An item error stops the walk rather than silently continuing to the next page.
func TestForPaginatedPropagatesItemError(t *testing.T) {
	sentinel := errors.New("bad item")
	pages := [][]int{{1, 2}, {3}}

	visited := 0
	err := ForPaginated(int64(1), "", pagedFetcher(pages), func(item int, i int) error {
		visited++
		if item == 2 {
			return sentinel
		}
		return nil
	})

	if !errors.Is(err, sentinel) {
		t.Fatalf("err = %v, want it to wrap %v", err, sentinel)
	}
	if visited != 2 {
		t.Errorf("visited %d items, want 2 (stopped at the failing one)", visited)
	}
}

// The index passed to processItem is the position within its page.
func TestForPaginatedItemIndexIsPerPage(t *testing.T) {
	pages := [][]int{{10, 11}, {12}}

	var indices []int
	if err := ForPaginated(int64(1), "", pagedFetcher(pages), func(item int, i int) error {
		indices = append(indices, i)
		return nil
	}); err != nil {
		t.Fatalf("ForPaginated: %v", err)
	}

	want := []int{0, 1, 0}
	for i := range want {
		if indices[i] != want[i] {
			t.Fatalf("indices = %v, want %v", indices, want)
		}
	}
}
