//go:build beetroot_library && cgo

package main

/*
#include <stdint.h>
#include <stdlib.h>
*/
import "C"

import (
	"errors"
	"sync"
	"unsafe"
)

// No Go pointers cross the C boundary. The registry owns clients/requests;
// result strings are C allocations released exclusively through BrFree.
var libraryHandles = struct {
	sync.Mutex
	next     uint64
	clients  map[uint64]*libraryClient
	requests map[uint64]*libraryRequest
}{clients: make(map[uint64]*libraryClient), requests: make(map[uint64]*libraryRequest)}

//export BrVersion
func BrVersion() C.uint32_t { return 1 }

//export BrOpen
func BrOpen(configPath, downloadsDir *C.char, errorOut **C.char) (result C.uint64_t) {
	if errorOut != nil {
		*errorOut = nil
	}
	defer func() {
		if recover() != nil {
			result = 0
			if errorOut != nil {
				*errorOut = C.CString("internal library panic")
			}
		}
	}()
	client, err := openLibrary(C.GoString(configPath), C.GoString(downloadsDir))
	if err != nil {
		if errorOut != nil {
			*errorOut = C.CString(err.Error())
		}
		return 0
	}
	libraryHandles.Lock()
	defer libraryHandles.Unlock()
	libraryHandles.next++
	libraryHandles.clients[libraryHandles.next] = client
	return C.uint64_t(libraryHandles.next)
}

//export BrRequest
func BrRequest(client C.uint64_t) C.uint64_t {
	libraryHandles.Lock()
	defer libraryHandles.Unlock()
	c := libraryHandles.clients[uint64(client)]
	if c == nil {
		return 0
	}
	libraryHandles.next++
	libraryHandles.requests[libraryHandles.next] = newLibraryRequest(c)
	return C.uint64_t(libraryHandles.next)
}

func requestFor(handle C.uint64_t) *libraryRequest {
	libraryHandles.Lock()
	defer libraryHandles.Unlock()
	return libraryHandles.requests[uint64(handle)]
}

func ffiResult(fn func() (any, error)) (result *C.char) {
	defer func() {
		if recover() != nil {
			result = C.CString(`{"ok":false,"error":"internal library panic"}`)
		}
	}()
	value, err := fn()
	return C.CString(libraryJSON(value, err))
}

//export BrSearch
func BrSearch(handle C.uint64_t, query *C.char) *C.char {
	return ffiResult(func() (any, error) {
		r := requestFor(handle)
		if r == nil {
			return nil, errors.New("invalid request handle")
		}
		if query == nil {
			return nil, errors.New("null query")
		}
		return r.search(C.GoString(query))
	})
}

//export BrDownload
func BrDownload(handle C.uint64_t, url *C.char) *C.char {
	return ffiResult(func() (any, error) {
		r := requestFor(handle)
		if r == nil {
			return nil, errors.New("invalid request handle")
		}
		if url == nil {
			return nil, errors.New("null track URL")
		}
		return r.download(C.GoString(url))
	})
}

//export BrProgress
func BrProgress(handle C.uint64_t) *C.char {
	return ffiResult(func() (any, error) {
		r := requestFor(handle)
		if r == nil {
			return nil, errors.New("invalid request handle")
		}
		return r.snapshot(), nil
	})
}

//export BrCancel
func BrCancel(handle C.uint64_t) C.int {
	if r := requestFor(handle); r != nil {
		r.cancel()
		return 1
	}
	return 0
}

//export BrCloseRequest
func BrCloseRequest(handle C.uint64_t) C.int {
	libraryHandles.Lock()
	defer libraryHandles.Unlock()
	if r := libraryHandles.requests[uint64(handle)]; r != nil {
		r.cancel()
		delete(libraryHandles.requests, uint64(handle))
		return 1
	}
	return 0
}

//export BrClose
func BrClose(handle C.uint64_t) C.int {
	libraryHandles.Lock()
	defer libraryHandles.Unlock()
	if libraryHandles.clients[uint64(handle)] == nil {
		return 0
	}
	// Existing requests retain their client. No use-after-free if close races
	// with an operation; release each request to cancel and reclaim its state.
	delete(libraryHandles.clients, uint64(handle))
	return 1
}

//export BrFree
func BrFree(result *C.char) { C.free(unsafe.Pointer(result)) }
