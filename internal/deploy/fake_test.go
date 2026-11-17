//go:build linux

package deploy

import (
	"context"
	"os"
	"path/filepath"
	"sync"
)

// fakeRunner records every Command it is given and answers with the Result
// scripted for the Command's Name. A Name with no script succeeds with exit
// code 0. An effect scripted for a Name runs before the answer. Like the
// real runner, it sets the Result's Name to the Command's.
type fakeRunner struct {
	mu      sync.Mutex
	calls   []Command
	results map[string]Result
	effects map[string]func(c Command) error
}

func newFake() *fakeRunner {
	return &fakeRunner{results: map[string]Result{}, effects: map[string]func(Command) error{}}
}

func (f *fakeRunner) Run(ctx context.Context, c Command) Result {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, c)
	if effect := f.effects[c.Name]; effect != nil {
		if err := effect(c); err != nil {
			panic(err)
		}
	}
	r, ok := f.results[c.Name]
	if !ok {
		r = Result{Ending: Succeeded}
	}
	r.Name = c.Name
	return r
}

// names are the Names of the recorded Commands, in order.
func (f *fakeRunner) names() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []string
	for _, c := range f.calls {
		out = append(out, c.Name)
	}
	return out
}

// argAfter is the argument that follows flag in args, or "" when there is none.
func argAfter(args []string, flag string) string {
	for i := 0; i+1 < len(args); i++ {
		if args[i] == flag {
			return args[i+1]
		}
	}
	return ""
}

// writeArchive is what a pull does: it writes NAME-VERSION.tgz into the
// folder that the command's --destination names.
func writeArchive(name, version string) func(c Command) error {
	return func(c Command) error {
		dest := argAfter(c.Args, "--destination")
		return os.WriteFile(filepath.Join(dest, name+"-"+version+".tgz"), []byte("archive"), 0o644)
	}
}
