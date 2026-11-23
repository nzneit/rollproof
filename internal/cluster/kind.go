//go:build linux

package cluster

import "context"

// untilDone runs call, a call into kind that takes no context, and returns its
// error; or, when ctx ends first, ctx's error at once. A ctx that has already
// ended returns its error, and call is not made. A call that ctx outran goes on in
// its goroutine until kind returns, and its result is dropped: what it made
// carries the cluster's name, and the next run's removal of leftovers deletes it
// (section 10, "On a signal"). A panic of call is raised again here when call
// returned first, and dropped when ctx won, so that it never ends the process
// from the goroutine.
func untilDone(ctx context.Context, call func() error) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	type result struct {
		err      error
		panicked any
	}
	done := make(chan result, 1) // room for one, so that the goroutine never waits for a reader
	go func() {
		var r result
		defer func() {
			r.panicked = recover()
			done <- r
		}()
		r.err = call()
	}()
	select {
	case r := <-done:
		if r.panicked != nil {
			panic(r.panicked)
		}
		return r.err
	case <-ctx.Done():
		return ctx.Err()
	}
}
