// A stand-in for a probe, for proof p11: it writes numbered JSON lines at a
// set rate, each with one write and no buffering, and can end with a last
// piece that has no line end. SIGTERM ends it at once with exit code 0.
package main

import (
	"flag"
	"fmt"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// sameLine is the line that -same writes every time.
const sameLine = `{"ok":false,"class":"refused"}` + "\n"

func main() {
	n := flag.Int("n", 10, "the number of lines")
	rate := flag.Float64("rate", 0, "lines a second; 0 writes as fast as it can")
	pad := flag.Int("pad", 0, "the number of x bytes in pad")
	tail := flag.Bool("tail", false, "after the lines, write a last piece with no line end, then exit")
	same := flag.Bool("same", false, `write the identical line {"ok":false,"class":"refused"} instead`)
	flag.Parse()

	term := make(chan os.Signal, 1)
	signal.Notify(term, syscall.SIGTERM)
	go func() {
		<-term
		os.Exit(0)
	}()

	padding := strings.Repeat("x", *pad)
	start := time.Now()
	for i := 1; i <= *n; i++ {
		if *rate > 0 {
			due := start.Add(time.Duration(float64(i-1) / *rate * float64(time.Second)))
			if wait := time.Until(due); wait > 0 {
				time.Sleep(wait)
			}
		}
		line := sameLine
		if !*same {
			line = `{"ok":true,"detail":{"i":` + strconv.Itoa(i) + `,"pad":"` + padding + `"}}` + "\n"
		}
		if _, err := os.Stdout.Write([]byte(line)); err != nil {
			fmt.Fprintf(os.Stderr, "liner: %v\n", err)
			os.Exit(1)
		}
	}
	if *tail {
		piece := `{"ok":true,"detail":{"i":` + strconv.Itoa(*n+1) + `}}`
		if _, err := os.Stdout.Write([]byte(piece)); err != nil {
			fmt.Fprintf(os.Stderr, "liner: %v\n", err)
			os.Exit(1)
		}
	}
}
