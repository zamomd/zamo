package logseq

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestJournalDate(t *testing.T) {
	cases := []struct {
		file, format string
		want         string
		ok           bool
	}{
		{"2024_01_15", "yyyy_MM_dd", "2024-01-15", true},
		{"2024-01-15", "yyyy_MM_dd", "2024-01-15", true}, // built-in formats stay accepted
		{"20240115", "yyyy_MM_dd", "2024-01-15", true},
		{"15-01-2024", "dd-MM-yyyy", "2024-01-15", true},
		{"Go", "yyyy_MM_dd", "", false},
		{"2024_13_40", "yyyy_MM_dd", "", false},
	}
	for _, c := range cases {
		d, ok := journalDate(c.file, c.format)
		if !assert.Equal(t, c.ok, ok, "journalDate(%q, %q) ok", c.file, c.format) {
			continue
		}
		if ok {
			assert.Equal(t, c.want, d.Format("2006-01-02"), "journalDate(%q, %q)", c.file, c.format)
		}
	}
}

func TestFormatDate(t *testing.T) {
	d, _ := journalDate("2024_01_05", "yyyy_MM_dd")
	cases := []struct{ pattern, want string }{
		{"MMM do, yyyy", "Jan 5th, 2024"},
		{"MMMM do, yyyy", "January 5th, 2024"},
		{"yyyy-MM-dd", "2024-01-05"},
		{"E, dd-MM-yyyy", "Fri, 05-01-2024"},
		{"EEEE, d MMM yyyy", "Friday, 5 Jan 2024"},
		{"yyyy年MM月dd日", "2024年01月05日"},
		{"do MMM yy", "5th Jan 24"},
	}
	for _, c := range cases {
		assert.Equal(t, c.want, formatDate(d, c.pattern), "formatDate(%q)", c.pattern)
	}
	d, _ = journalDate("2024_03_22", "yyyy_MM_dd")
	assert.Equal(t, "22nd", formatDate(d, "do"))
	d, _ = journalDate("2024_03_13", "yyyy_MM_dd")
	assert.Equal(t, "13th", formatDate(d, "do"))
}

func TestReadConfig(t *testing.T) {
	cases := []struct {
		name, edn string
		want      graphConfig
	}{
		{"empty", "", graphConfig{titleFormat: "MMM do, yyyy", fileFormat: "yyyy_MM_dd", nameFormat: nameLegacy}},
		{
			"new graph", `{:file/name-format :triple-lowbar
 :journal/page-title-format "yyyy-MM-dd"
 :journal/file-name-format "yyyy-MM-dd"}`,
			graphConfig{titleFormat: "yyyy-MM-dd", fileFormat: "yyyy-MM-dd", nameFormat: nameTripleLowbar},
		},
		{
			"commented out", `{;; :file/name-format :triple-lowbar
 ;; :journal/page-title-format "yyyy-MM-dd"
 :preferred-format :markdown}`,
			graphConfig{titleFormat: "MMM do, yyyy", fileFormat: "yyyy_MM_dd", nameFormat: nameLegacy},
		},
	}
	for _, c := range cases {
		assert.Equal(t, c.want, parseConfig(c.edn), c.name)
	}
}

func TestPageNameByFormat(t *testing.T) {
	cases := []struct {
		base   string
		format nameFormat
		want   string
	}{
		{"lang___go", nameTripleLowbar, "lang/go"},
		{"a%2Fb", nameTripleLowbar, "a/b"},
		{"v1.2", nameTripleLowbar, "v1.2"},
		{"___leading", nameTripleLowbar, "leading"},
		{"what%3F", nameTripleLowbar, "what?"},
		{"lang.go", nameLegacy, "lang/go"},
		{"a%2Fb", nameLegacy, "a/b"},
		{"lang___go", nameLegacy, "lang___go"},
	}
	for _, c := range cases {
		assert.Equal(t, c.want, decodeName(c.base, c.format), "decodeName(%q, %v)", c.base, c.format)
	}
}
