package run

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"rollproof/internal/verdict"
	"rollproof/internal/watch"
)

func TestWriteTimeline(t *testing.T) {
	from := time.Date(2026, 11, 19, 10, 0, 0, 0, time.UTC)
	obs := []verdict.Observation{
		{T: from.Add(200 * time.Millisecond), OK: true, N: 200},
		{T: from.Add(1500 * time.Millisecond), OK: false, N: 1, Class: "refused"},
		{T: from.Add(1700 * time.Millisecond), OK: true, N: 150},
		{T: from.Add(-time.Second), OK: true, N: 5},    // before the first row
		{T: from.Add(3 * time.Second), OK: true, N: 9}, // at to
	}
	pods := []watch.PodEvent{
		{At: from.Add(1100 * time.Millisecond), Pod: "orders-7f8e9d-k3m2p", Workload: "deployment/orders", Kind: "created"},
		{At: from.Add(1900 * time.Millisecond), Pod: "orders-6d5f9c-x2k4q", Workload: "deployment/orders", Kind: "deleted"},
		{At: from.Add(2500 * time.Millisecond), Pod: "orders-7f8e9d-k3m2p", Workload: "deployment/orders", Kind: "ready"},
	}
	want := "second\tchecks\tfailed\tpod events\n" +
		"2026-11-19T10:00:00Z\t200\t0\t\n" +
		"2026-11-19T10:00:01Z\t151\t1\tcreated orders-7f8e9d-k3m2p (deployment/orders), deleted orders-6d5f9c-x2k4q (deployment/orders)\n" +
		"2026-11-19T10:00:02Z\t0\t0\tready orders-7f8e9d-k3m2p (deployment/orders)\n"
	var b bytes.Buffer
	if err := writeTimeline(&b, obs, pods, from.Add(400*time.Millisecond), from.Add(3*time.Second)); err != nil {
		t.Fatal(err)
	}
	if got := b.String(); got != want {
		t.Errorf("timeline\n%s\nwant\n%s", got, want)
	}
}

// lastBytes is a replace that keeps the last bytes written under each name, safe from any
// goroutine.
type lastBytes struct {
	mu    sync.Mutex
	files map[string][]byte
}

func (l *lastBytes) replace(name string, b []byte) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.files == nil {
		l.files = map[string][]byte{}
	}
	l.files[name] = append([]byte(nil), b...)
	return nil
}

func (l *lastBytes) get(name string) []byte {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.files[name]
}

// decodeMarks decodes windows.json's bytes, and fails the test when it cannot.
func decodeMarks(t *testing.T, b []byte) []mark {
	t.Helper()
	var ms []mark
	if err := json.Unmarshal(b, &ms); err != nil {
		t.Fatalf("decode windows.json %q: %v", b, err)
	}
	return ms
}

func TestMarks(t *testing.T) {
	first := t0.Add(-15 * time.Second)
	ws := []verdict.Window{{Name: verdict.Install, Start: first}, {Name: verdict.Steady}, {Name: verdict.Deploy}, {Name: verdict.Rollback}}

	t.Run("set writes every window, a missing time as null", func(t *testing.T) {
		var files lastBytes
		var warned []string
		m := newMarks(files.replace, func(s string) { warned = append(warned, s) })
		defer m.end()
		m.set(ws)
		var compact bytes.Buffer
		if err := json.Compact(&compact, files.get("windows.json")); err != nil {
			t.Fatal(err)
		}
		want := `[{"name":"install","start":"2026-11-13T09:59:45Z","finished":null,"end":null,"finishes":null},` +
			`{"name":"steady","start":null,"finished":null,"end":null,"finishes":null},` +
			`{"name":"deploy","start":null,"finished":null,"end":null,"finishes":null},` +
			`{"name":"rollback","start":null,"finished":null,"end":null,"finishes":null}]`
		if compact.String() != want {
			t.Errorf("windows.json is\n%s\nwant\n%s", compact.String(), want)
		}
		if len(warned) != 0 {
			t.Errorf("warnings %q, want none", warned)
		}
	})

	t.Run("each finish that follow takes is kept in order", func(t *testing.T) {
		var files lastBytes
		m := newMarks(files.replace, func(string) {})
		m.set(ws)
		finishes := make(chan watch.Finish, 2)
		m.follow(finishes)
		finishes <- watch.Finish{Name: verdict.Deploy, At: t0.Add(25 * time.Second)}
		finishes <- watch.Finish{Name: verdict.Deploy, At: t0.Add(32 * time.Second)}
		m.end()
		ms := decodeMarks(t, files.get("windows.json"))
		got := ms[2].Finishes
		if len(got) != 2 || !got[0].Equal(t0.Add(25*time.Second)) || !got[1].Equal(t0.Add(32*time.Second)) {
			t.Errorf("the deploy's finishes are %v, want [%v %v]", got, t0.Add(25*time.Second), t0.Add(32*time.Second))
		}
		m.end() // a second end does nothing
	})

	t.Run("two finishes at one moment are both kept", func(t *testing.T) {
		var files lastBytes
		m := newMarks(files.replace, func(string) {})
		m.set(ws)
		finishes := make(chan watch.Finish, 2)
		m.follow(finishes)
		at := t0.Add(25 * time.Second)
		finishes <- watch.Finish{Name: verdict.Deploy, At: at}
		finishes <- watch.Finish{Name: verdict.Deploy, At: at}
		m.end()
		got := decodeMarks(t, files.get("windows.json"))[2].Finishes
		if len(got) != 2 || !got[0].Equal(at) || !got[1].Equal(at) {
			t.Errorf("the deploy's finishes are %v, want [%v %v]", got, at, at)
		}
	})

	t.Run("a panic of the follower is one warning", func(t *testing.T) {
		var mu sync.Mutex
		var warned []string
		calls := 0
		panicked := make(chan struct{})
		replace := func(string, []byte) error {
			mu.Lock()
			calls++
			first := calls == 1
			mu.Unlock()
			if first {
				close(panicked)
				panic("the disk broke")
			}
			return nil
		}
		m := newMarks(replace, func(s string) { mu.Lock(); warned = append(warned, s); mu.Unlock() })
		finishes := make(chan watch.Finish, 1)
		m.follow(finishes)
		finishes <- watch.Finish{Name: verdict.Deploy, At: t0.Add(25 * time.Second)}
		<-panicked // the follower's write is the first, and it panics
		done := make(chan struct{})
		go func() { m.end(); close(done) }()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Fatal("end did not return after the follower panicked")
		}
		mu.Lock()
		defer mu.Unlock()
		if len(warned) != 1 || warned[0] != "the evidence windows.json was not written: the disk broke" {
			t.Errorf("warnings %q, want one for the follower's panic", warned)
		}
	})

	t.Run("a closed channel ends the following", func(t *testing.T) {
		var files lastBytes
		m := newMarks(files.replace, func(string) {})
		finishes := make(chan watch.Finish, 1)
		m.follow(finishes)
		finishes <- watch.Finish{Name: verdict.Deploy, At: t0.Add(25 * time.Second)}
		close(finishes)
		m.set(ws)
		done := make(chan struct{})
		go func() { m.end(); close(done) }()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Fatal("end did not return after the channel was closed")
		}
		if got := decodeMarks(t, files.get("windows.json"))[2].Finishes; len(got) != 1 {
			t.Errorf("the deploy's finishes are %v, want the one sent before the close", got)
		}
	})

	t.Run("end with no follow writes the file", func(t *testing.T) {
		var files lastBytes
		m := newMarks(files.replace, func(string) {})
		m.end()
		m.end()
		if got := string(files.get("windows.json")); strings.TrimSpace(got) != "[]" {
			t.Errorf("windows.json is %q, want []", got)
		}
	})

	t.Run("a write that fails warns once", func(t *testing.T) {
		var warned []string
		m := newMarks(func(string, []byte) error { return errors.New("no room") }, func(s string) { warned = append(warned, s) })
		for range 4 {
			m.set(ws)
		}
		m.end()
		if len(warned) != 1 || !strings.Contains(warned[0], "windows.json") {
			t.Errorf("warnings %q, want one that names windows.json", warned)
		}
	})
}
