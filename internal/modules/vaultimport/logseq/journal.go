package logseq

import (
	"strconv"
	"strings"
	"time"
)

// Journal files are named by a date pattern and their pages by another: a
// graph's config sets both, and Logseq names the page by rendering the date
// the filename holds in the title pattern. Both patterns use the Joda-style
// letters of cljs-time (yyyy, MM, dd, MMM, do, E, ...).

// builtInFileFormats are the date patterns Logseq accepts for a journal
// filename whatever the graph's own setting says.
var builtInFileFormats = []string{
	"yyyy_MM_dd", "yyyy-MM-dd", "yyyyMMdd", "yyyy.MM.dd", "yyyy/MM/dd",
	"dd-MM-yyyy", "dd.MM.yyyy", "dd_MM_yyyy", "MM-dd-yyyy", "MM_dd_yyyy",
}

// journalDate reads the date a journal filename holds, trying the graph's
// own file pattern first and the built-in ones after.
func journalDate(base, fileFormat string) (time.Time, bool) {
	patterns := append([]string{fileFormat}, builtInFileFormats...)
	for _, p := range patterns {
		layout, ok := goLayout(p)
		if !ok {
			continue
		}
		if d, err := time.Parse(layout, base); err == nil {
			return d, true
		}
	}
	return time.Time{}, false
}

// goLayout converts a numeric Joda pattern to a Go time layout. Patterns
// with ordinals or day names cannot be parsed and report !ok.
func goLayout(pattern string) (string, bool) {
	var b strings.Builder
	for _, tok := range tokens(pattern) {
		switch tok {
		case "yyyy":
			b.WriteString("2006")
		case "yy":
			b.WriteString("06")
		case "MM":
			b.WriteString("01")
		case "M":
			b.WriteString("1")
		case "dd":
			b.WriteString("02")
		case "d":
			b.WriteString("2")
		case "MMM":
			b.WriteString("Jan")
		case "MMMM":
			b.WriteString("January")
		default:
			if isLetters(tok) {
				return "", false
			}
			b.WriteString(tok)
		}
	}
	return b.String(), true
}

// formatDate renders d in a Joda pattern: year, month, day, ordinal day and
// day-of-week letters, everything else verbatim.
func formatDate(d time.Time, pattern string) string {
	var b strings.Builder
	for _, tok := range tokens(pattern) {
		switch tok {
		case "yyyy":
			b.WriteString(d.Format("2006"))
		case "yy":
			b.WriteString(d.Format("06"))
		case "MMMM":
			b.WriteString(d.Format("January"))
		case "MMM":
			b.WriteString(d.Format("Jan"))
		case "MM":
			b.WriteString(d.Format("01"))
		case "M":
			b.WriteString(d.Format("1"))
		case "dd":
			b.WriteString(d.Format("02"))
		case "d":
			b.WriteString(d.Format("2"))
		case "do":
			b.WriteString(ordinal(d.Day()))
		case "EEEE":
			b.WriteString(d.Format("Monday"))
		case "E", "EE", "EEE":
			b.WriteString(d.Format("Mon"))
		default:
			b.WriteString(tok)
		}
	}
	return b.String()
}

// tokens splits a pattern into runs of one pattern letter, the ordinal
// marker "do", and literal text.
func tokens(pattern string) []string {
	var out []string
	rs := []rune(pattern)
	for i := 0; i < len(rs); {
		r := rs[i]
		if !isPatternLetter(r) {
			j := i
			for j < len(rs) && !isPatternLetter(rs[j]) {
				j++
			}
			out = append(out, string(rs[i:j]))
			i = j
			continue
		}
		if r == 'd' && i+1 < len(rs) && rs[i+1] == 'o' {
			out = append(out, "do")
			i += 2
			continue
		}
		j := i
		for j < len(rs) && rs[j] == r {
			j++
		}
		out = append(out, string(rs[i:j]))
		i = j
	}
	return out
}

func isPatternLetter(r rune) bool { return strings.ContainsRune("yMdE", r) }

func isLetters(s string) bool {
	for _, r := range s {
		if !isPatternLetter(r) {
			return false
		}
	}
	return s != ""
}

func ordinal(n int) string {
	suffix := "th"
	switch {
	case n%100 >= 11 && n%100 <= 13:
	case n%10 == 1:
		suffix = "st"
	case n%10 == 2:
		suffix = "nd"
	case n%10 == 3:
		suffix = "rd"
	}
	return strconv.Itoa(n) + suffix
}
