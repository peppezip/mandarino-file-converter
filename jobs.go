package main

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"sync"
	"time"

	"mandarino/engine"
)

// A job is one action running on a set of files, in its own goroutine.
// The user interface polls its progress and is told when it ends.

type job struct {
	id      int
	action  engine.Action
	files   []string
	started time.Time
	cancel  context.CancelFunc
	cx, cy  int // where the wheel was on screen (the progress wheel appears there)

	mu     sync.Mutex // guards the fields below
	index  int        // file being processed
	frac   float64    // progress of that file (0..1)
	result *engine.Result
}

// startJob launches the action in the background. done is called from the
// worker goroutine when it finishes (platforms forward it to their UI thread).
func startJob(id int, a engine.Action, files []string, cx, cy int, done func(*job)) *job {
	ctx, cancel := context.WithCancel(context.Background())
	j := &job{id: id, action: a, files: files, started: time.Now(), cancel: cancel, cx: cx, cy: cy}
	go func() {
		defer cancel()
		res := engine.Run(ctx, a.ID, files, func(i, n int, f float64) {
			j.mu.Lock()
			j.index, j.frac = i, f
			j.mu.Unlock()
		})
		j.mu.Lock()
		j.result = &res
		j.mu.Unlock()
		done(j)
	}()
	return j
}

// state returns a consistent snapshot of the job's progress.
func (j *job) state() (index int, frac float64, res *engine.Result) {
	j.mu.Lock()
	defer j.mu.Unlock()
	return j.index, j.frac, j.result
}

// percent is the overall progress 0..100, for tooltips.
func (j *job) percent() int {
	i, f, _ := j.state()
	return int((float64(i) + f) / float64(max(len(j.files), 1)) * 100)
}

// summary turns a finished job into a notification: title, text, and whether it is an error.
func (j *job) summary() (title, text string, isError bool) {
	_, _, r := j.state()
	if r == nil {
		return "", "", false
	}
	switch {
	case len(r.Failed) == 0 && len(r.Outputs) == 1:
		return "Done!", "Created " + filepath.Base(r.Outputs[0]), false
	case len(r.Failed) == 0:
		return "Done!", fmt.Sprintf("Created %d files (%s).", len(r.Outputs), j.action.Desc), false
	}
	if errors.Is(r.Failed[0].Err, context.Canceled) {
		return "Cancelled", j.action.Desc, true
	}
	title = "Conversion failed"
	if len(r.Outputs) > 0 {
		title = fmt.Sprintf("Finished with errors (%d done, %d failed)", len(r.Outputs), len(r.Failed))
	} else if len(r.Failed) > 1 {
		title = fmt.Sprintf("Conversion failed (%d files)", len(r.Failed))
	}
	return title, errorText(r.Failed[0]), true
}

// errorText formats a failure as "file.ext: reason", shortened for notifications.
func errorText(f engine.Failure) string {
	msg := f.Err.Error()
	if f.File != "" {
		msg = filepath.Base(f.File) + ": " + msg
	}
	if r := []rune(msg); len(r) > 220 {
		msg = string(r[:217]) + "…"
	}
	return msg
}

// lastOutput returns the last file created by the job ("" if none).
func (j *job) lastOutput() string {
	_, _, r := j.state()
	if r == nil || len(r.Outputs) == 0 {
		return ""
	}
	return r.Outputs[len(r.Outputs)-1]
}

// succeeded reports whether every file was converted.
func (j *job) succeeded() bool {
	_, _, r := j.state()
	return r != nil && len(r.Failed) == 0
}
