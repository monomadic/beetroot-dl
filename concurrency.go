package main

import (
	"fmt"
	"sync"
)

func (app *application) globalWorker(fn func()) {
	app.wg.Add(1)

	go func() {
		app.semAcquire(app.globalSem)
		defer app.wg.Done()
		defer app.semRelease(app.globalSem)
		defer func() {
			// A panic in a worker must not vanish into stdout: report it on
			// stderr and count it, so the process still exits non-zero.
			if err := recover(); err != nil {
				app.LogError("worker panic", fmt.Errorf("%s", err))
			}
		}()
		fn()
	}()
}

func (app *application) downloadWorker(wg *sync.WaitGroup, fn func()) {
	wg.Add(1)

	go func() {
		defer wg.Done()
		select {
		case <-app.ctx.Done():
			return
		default:
		}

		app.semAcquire(app.downloadSem)
		defer app.semRelease(app.downloadSem)

		defer func() {
			// A panic in a worker must not vanish into stdout: report it on
			// stderr and count it, so the process still exits non-zero.
			if err := recover(); err != nil {
				app.LogError("worker panic", fmt.Errorf("%s", err))
			}
		}()
		fn()
	}()
}

func (app *application) semAcquire(s chan struct{}) {
	s <- struct{}{}
}

func (app *application) semRelease(s chan struct{}) {
	<-s
}
