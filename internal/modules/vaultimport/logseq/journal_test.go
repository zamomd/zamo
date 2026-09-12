package logseq

import "testing"

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
		if ok != c.ok {
			t.Errorf("journalDate(%q, %q) ok = %v, want %v", c.file, c.format, ok, c.ok)
			continue
		}
		if ok && d.Format("2006-01-02") != c.want {
			t.Errorf("journalDate(%q, %q) = %s, want %s", c.file, c.format, d.Format("2006-01-02"), c.want)
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
		if got := formatDate(d, c.pattern); got != c.want {
			t.Errorf("formatDate(%q) = %q, want %q", c.pattern, got, c.want)
		}
	}
	d, _ = journalDate("2024_03_22", "yyyy_MM_dd")
	if got := formatDate(d, "do"); got != "22nd" {
		t.Errorf("22 → %q", got)
	}
	d, _ = journalDate("2024_03_13", "yyyy_MM_dd")
	if got := formatDate(d, "do"); got != "13th" {
		t.Errorf("13 → %q", got)
	}
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
		if got := parseConfig(c.edn); got != c.want {
			t.Errorf("%s: parseConfig = %+v, want %+v", c.name, got, c.want)
		}
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
		if got := decodeName(c.base, c.format); got != c.want {
			t.Errorf("decodeName(%q, %v) = %q, want %q", c.base, c.format, got, c.want)
		}
	}
}
