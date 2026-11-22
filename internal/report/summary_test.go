package report

import (
	"fmt"
	"reflect"
	"strings"
	"testing"
)

func TestSummaryGolden(t *testing.T) {
	for _, c := range []struct {
		name string
		run  Run
	}{
		{"pass", passRun()},
		{"fail", failRun()},
		{"not-verified", notVerifiedRun()},
		{"before-first-observation", beforeFirstRun()},
		{"in-install-window", inInstallRun()},
		{"install-decided", installDecidedRun()},
		{"custom-no-ms-or-lag", customNoMsOrLagRun()},
		{"stall-mirror", stallMirrorRun()},
	} {
		t.Run(c.name, func(t *testing.T) {
			golden(t, "testdata/summary-"+c.name+".golden.md", []byte(Summary(c.run)))
		})
	}
}

func TestSummaryKeepsTextFromOutsideTheToolInCode(t *testing.T) {
	const planted = "MARK<b>x</b>|*y*_z_[l](http://e.example) www.e.example ```\r\n`` end"
	// fence is a text that, on a reason's detail line, would be a line that closes a fence of
	// three backticks, as GitHub reads one: up to three spaces, then a run of backticks.
	const fence = "````"
	for _, c := range []struct {
		name string
		run  func() Run
	}{
		{"pass", passRun},
		{"fail", failRun},
		{"stall-mirror", stallMirrorRun},
		{"custom", customRun},
		{"before-first-observation", beforeFirstRun},
	} {
		for _, text := range []string{planted, fence} {
			t.Run(fmt.Sprintf("%s/%q", c.name, text), func(t *testing.T) {
				md := Summary(plantFreeText(c.run(), text))
				outside, err := outsideCode(md)
				if err != nil {
					t.Fatalf("outsideCode: %v\n%s", err, md)
				}
				if strings.Contains(outside, "MARK") {
					t.Errorf("text outside code holds MARK:\n%s\nin\n%s", outside, md)
				}
				if strings.Contains(outside, "`") {
					t.Errorf("text outside code holds a backtick:\n%s\nin\n%s", outside, md)
				}
				if text == planted && !strings.Contains(md, "MARK<b>x</b>") {
					t.Errorf("the summary does not hold the planted text:\n%s", md)
				}
				lines := strings.Split(md, "\n")
				inFence, err := fencedLines(lines)
				if err != nil {
					t.Fatal(err)
				}
				header := -1
				for i, line := range lines {
					if inFence[i] || !strings.HasPrefix(line, "| ") {
						header = -1
						continue
					}
					if header < 0 {
						header = pipes(line)
						continue
					}
					if got := pipes(line); got != header {
						t.Errorf("line %q has %d cell borders, its table's header %d", line, got, header)
					}
				}
			})
		}
	}
}

// pipes counts the | of line that no \ precedes: the borders of a table row's cells.
func pipes(line string) int {
	n := 0
	for i := range len(line) {
		if line[i] == '|' && (i == 0 || line[i-1] != '\\') {
			n++
		}
	}
	return n
}

func TestFence(t *testing.T) {
	for _, c := range []struct {
		lines []string
		want  string
	}{
		{[]string{"a"}, "```\na\n```\n"},
		{[]string{"x ``` y"}, "````\nx ``` y\n````\n"},
		{[]string{"````", "b"}, "`````\n````\nb\n`````\n"},
	} {
		if got := fenced(c.lines); got != c.want {
			t.Errorf("fenced(%q) = %q, want %q", c.lines, got, c.want)
		}
	}
}

func TestCodeSpan(t *testing.T) {
	for _, c := range []struct{ in, want string }{
		{"a b", "`a b`"},
		{"a`b", "``a`b``"},
		{"`a", "`` `a ``"},
		{"a ", "` a  `"},
		{"", ""},
	} {
		if got := codeSpan(c.in); got != c.want {
			t.Errorf("codeSpan(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestMarkdownTable(t *testing.T) {
	got := markdownTable([]string{"h1", "h2"}, []bool{false, true}, [][]string{{"a|b", "c|d"}, {"", "e"}}, []bool{false, true})
	want := "| h1 | h2 |\n| --- | ---: |\n| a\\|b | `c\\|d` |\n|  | `e` |\n"
	if got != want {
		t.Errorf("markdownTable = %q, want %q", got, want)
	}
}

func TestSummaryLine(t *testing.T) {
	for _, c := range []struct{ in, want string }{
		{"warning: the cluster was not deleted: a ``` b", "\n````\nwarning: the cluster was not deleted: a ``` b\n````\n"},
		{"warning: a\nb\tc", "\n```\nwarning: a b c\n```\n"},
	} {
		if got := SummaryLine(c.in); got != c.want {
			t.Errorf("SummaryLine(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// plantFreeText returns a copy of r in which every string of a field without the tag mask:"keep",
// those of lists, pointers and map values included, is s: the text from outside the tool, as Mask
// reads it. Map keys and the vocabulary stay.
func plantFreeText(r Run, s string) Run {
	return plantedValue(reflect.ValueOf(r), s).Interface().(Run)
}

// plantedValue returns a copy of v with each of its strings s, walked as masked walks a record: the
// structs of this package field by field, the fields tagged mask:"keep" copied as they are, a list
// and a pointer with their contents, and a map with its values, its keys kept.
func plantedValue(v reflect.Value, s string) reflect.Value {
	switch v.Kind() {
	case reflect.String:
		return reflect.ValueOf(s).Convert(v.Type())
	case reflect.Pointer:
		if v.IsNil() {
			return v
		}
		out := reflect.New(v.Type().Elem())
		out.Elem().Set(plantedValue(v.Elem(), s))
		return out
	case reflect.Slice:
		if v.IsNil() {
			return v
		}
		out := reflect.MakeSlice(v.Type(), v.Len(), v.Len())
		for i := range v.Len() {
			out.Index(i).Set(plantedValue(v.Index(i), s))
		}
		return out
	case reflect.Map:
		if v.IsNil() {
			return v
		}
		out := reflect.MakeMapWithSize(v.Type(), v.Len())
		iter := v.MapRange()
		for iter.Next() {
			out.SetMapIndex(iter.Key(), plantedValue(iter.Value(), s))
		}
		return out
	case reflect.Struct:
		if v.Type().PkgPath() != pkgPath {
			return v
		}
		out := reflect.New(v.Type()).Elem()
		for i := range v.NumField() {
			if v.Type().Field(i).Tag.Get("mask") == "keep" {
				out.Field(i).Set(v.Field(i))
				continue
			}
			out.Field(i).Set(plantedValue(v.Field(i), s))
		}
		return out
	}
	return v
}

// outsideCode returns the text of md that is outside its fenced code blocks and code spans, read by
// the rules that fenced and codeSpan write by, which are GitHub's: a fence opens at a line that
// begins, after at most three spaces, with three or more backticks, and closes at the next line
// that is, after at most three spaces, a run of at least as many backticks and nothing else but
// spaces; outside a fence, a run of n backticks opens a code span that the next run of exactly n
// backticks on its line closes. A fence or a span that is not closed is an error.
func outsideCode(md string) (string, error) {
	lines := strings.Split(md, "\n")
	inFence, err := fencedLines(lines)
	if err != nil {
		return "", err
	}
	var out strings.Builder
	for i, line := range lines {
		if inFence[i] {
			continue
		}
		span := 0
		for j := 0; j < len(line); {
			if line[j] != '`' {
				if span == 0 {
					out.WriteByte(line[j])
				}
				j++
				continue
			}
			n := backticks(line[j:])
			switch {
			case span == 0:
				span = n
			case n == span:
				span = 0
			}
			j += n
		}
		if span != 0 {
			return "", fmt.Errorf("line %d: a code span of %d backticks is not closed: %q", i+1, span, line)
		}
		out.WriteString("\n")
	}
	return out.String(), nil
}

// fencedLines reports, for each of lines, whether it belongs to a fenced code block, its fences
// included, by the rules of outsideCode. A fence that is not closed is an error.
func fencedLines(lines []string) ([]bool, error) {
	in := make([]bool, len(lines))
	fence, opened := 0, 0
	for i, line := range lines {
		rest := line
		for k := 0; k < 3 && strings.HasPrefix(rest, " "); k++ {
			rest = rest[1:]
		}
		n := backticks(rest)
		switch {
		case fence == 0 && n >= 3 && !strings.Contains(rest[n:], "`"):
			fence, opened = n, i
			in[i] = true
		case fence > 0:
			in[i] = true
			if n >= fence && strings.TrimRight(rest[n:], " ") == "" {
				fence = 0
			}
		}
	}
	if fence > 0 {
		return nil, fmt.Errorf("the fence of line %d is not closed", opened+1)
	}
	return in, nil
}

// backticks is the length of the run of backticks that s begins with.
func backticks(s string) int {
	return len(s) - len(strings.TrimLeft(s, "`"))
}
