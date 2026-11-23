package report

import "strings"

// Summary returns the job summary of r: the console's blocks, in the console's
// order, for every verdict and every ending (section 10). The first line is a
// heading; the table of windows and the table of transitions are Markdown tables;
// the lists of a pass are Markdown lists; every other block of lines is a fenced
// code block, line for line as the console prints it. Text from outside the tool,
// every string that Mask masks, stands only inside a code block or a code span, so
// that it cannot change how the page reads. r is cleaned as Console cleans it;
// Summary reads nothing else.
func Summary(r Run) string {
	r = cleanRun(r)
	var blocks []string

	heading := "### rollproof: " + verdictWord(r)
	if tail := strings.TrimSpace(verdictTail(r)); tail != "" {
		heading += " " + codeSpan(tail)
	}
	blocks = append(blocks, heading+"\n")

	if lines := setUp(r); len(lines) > 0 {
		blocks = append(blocks, fenced(lines))
	}
	if windowStarted(r) {
		header, rows := windowRows(r)
		right := make([]bool, len(header))
		for i := 1; i <= 6; i++ {
			right[i] = true
		}
		blocks = append(blocks, markdownTable(header, right, rows, make([]bool, len(header))))
		if lines := crossedLines(r); len(lines) > 0 {
			blocks = append(blocks, fenced(lines))
		}
	}
	if header, rows := transitionRows(r); rows != nil {
		outside := make([]bool, len(header))
		outside[1] = true
		blocks = append(blocks, markdownTable(header, make([]bool, len(header)), rows, outside))
	}
	if r.Verdict == "pass" {
		exercised, notExercised := exercisedItems(r)
		var items []string
		for _, item := range exercised {
			items = append(items, item[0]+": "+item[1])
		}
		blocks = append(blocks, markdownList("exercised", items), markdownList("not exercised", notExercised))
	}
	blocks = append(blocks, fenced(last(r)))
	return strings.Join(blocks, "\n")
}

// SummaryLine returns line as one more block of a summary: a blank line and a
// fenced code block of the line, cleaned as Console cleans text, so that a warning
// that arises after the summary was written is appended to it as a line
// (section 10).
func SummaryLine(line string) string {
	return "\n" + fenced([]string{clean(line)})
}

// markdownList writes a paragraph of its title and, after a blank line, a Markdown list of the
// items, one "- ITEM" line each; each line ends with a line end. A list with no items is the title
// line alone, so that the blank line that Summary puts between blocks stays one. The items are
// the tool's own words.
func markdownList(title string, items []string) string {
	var b strings.Builder
	b.WriteString(title + "\n")
	if len(items) > 0 {
		b.WriteString("\n")
	}
	for _, item := range items {
		b.WriteString("- " + item + "\n")
	}
	return b.String()
}

// longestBackticks is the length of the longest run of backticks in s, 0 when s holds none.
func longestBackticks(s string) int {
	longest, run := 0, 0
	for i := range len(s) {
		if s[i] == '`' {
			run++
			longest = max(longest, run)
		} else {
			run = 0
		}
	}
	return longest
}

// fenced writes lines as a fenced code block: a fence of backticks one longer than the longest run
// of backticks in any line, three at the least, so that no line can close it, then the lines, then
// the fence, each ending with a line end. In a fenced code block GitHub reads no Markdown and no
// HTML.
func fenced(lines []string) string {
	n := 3
	for _, line := range lines {
		n = max(n, longestBackticks(line)+1)
	}
	fence := strings.Repeat("`", n)
	return fence + "\n" + strings.Join(lines, "\n") + "\n" + fence + "\n"
}

// codeSpan writes s as a code span: delimited by a run of backticks one longer than the longest run
// in s, with one space inside each delimiter when s begins or ends with a backtick or a space,
// which the renderer takes off again. In a code span GitHub reads no Markdown, no link and no HTML.
// An empty s gives "": a span of nothing would show its backticks, and an empty table cell stays
// empty.
func codeSpan(s string) string {
	if s == "" {
		return ""
	}
	delimiter := strings.Repeat("`", longestBackticks(s)+1)
	if strings.HasPrefix(s, "`") || strings.HasPrefix(s, " ") || strings.HasSuffix(s, "`") || strings.HasSuffix(s, " ") {
		s = " " + s + " "
	}
	return delimiter + s + delimiter
}

// markdownTable writes a Markdown table: the header, a row of alignments, right-aligned for the
// columns that right marks, and the rows, each line "| CELL | CELL |". A cell of a column that
// outside marks is text from outside the tool and goes into a code span. Every | of a cell is
// written \|, inside a code span too, as GitHub's tables ask. An empty cell stays empty.
func markdownTable(header []string, right []bool, rows [][]string, outside []bool) string {
	var b strings.Builder
	line := func(cells []string) {
		b.WriteString("|")
		for _, cell := range cells {
			b.WriteString(" " + strings.ReplaceAll(cell, "|", `\|`) + " |")
		}
		b.WriteString("\n")
	}
	line(header)
	alignments := make([]string, len(header))
	for i := range header {
		alignments[i] = "---"
		if right[i] {
			alignments[i] = "---:"
		}
	}
	line(alignments)
	for _, row := range rows {
		cells := make([]string, len(row))
		for i, cell := range row {
			if outside[i] {
				cell = codeSpan(cell)
			}
			cells[i] = cell
		}
		line(cells)
	}
	return b.String()
}
