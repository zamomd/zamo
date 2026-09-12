package logseq

import (
	"net/url"
	"regexp"
	"strings"
)

// nameFormat is how a graph encodes a page name into a filename.
type nameFormat int

const (
	// nameLegacy is the format of graphs created before Logseq 0.8.9 and
	// of any graph whose config does not say otherwise: a dot stands for
	// the namespace separator and the rest is percent-encoded.
	nameLegacy nameFormat = iota
	// nameTripleLowbar is the format newer graphs declare: three low bars
	// stand for the namespace separator and the rest is percent-encoded.
	nameTripleLowbar
)

// graphConfig is what the import needs from logseq/config.edn.
type graphConfig struct {
	titleFormat string // :journal/page-title-format
	fileFormat  string // :journal/file-name-format
	nameFormat  nameFormat
}

var (
	ednComment  = regexp.MustCompile(`;.*`)
	ednKeyword  = regexp.MustCompile(`:file/name-format\s+:triple-lowbar`)
	ednTitleFmt = regexp.MustCompile(`:journal/page-title-format\s+"([^"]*)"`)
	ednFileFmt  = regexp.MustCompile(`:journal/file-name-format\s+"([^"]*)"`)
)

// parseConfig reads the three settings out of a config.edn, after
// dropping comments, and fills in Logseq's defaults for the rest. A full
// EDN reader is not needed for three top-level keys.
func parseConfig(edn string) graphConfig {
	edn = ednComment.ReplaceAllString(edn, "")
	c := graphConfig{titleFormat: "MMM do, yyyy", fileFormat: "yyyy_MM_dd", nameFormat: nameLegacy}
	if ednKeyword.MatchString(edn) {
		c.nameFormat = nameTripleLowbar
	}
	if m := ednTitleFmt.FindStringSubmatch(edn); m != nil && m[1] != "" {
		c.titleFormat = m[1]
	}
	if m := ednFileFmt.FindStringSubmatch(edn); m != nil && m[1] != "" {
		c.fileFormat = m[1]
	}
	return c
}

// decodeName turns a filename body back into the page name it encodes, as
// Logseq's title-parsing does for each format.
func decodeName(base string, format nameFormat) string {
	if format == nameLegacy {
		name := strings.ReplaceAll(base, ".", "/")
		if decoded, err := url.PathUnescape(name); err == nil {
			return decoded
		}
		return name
	}
	name := strings.ReplaceAll(base, "___", "/")
	if decoded, err := url.PathUnescape(name); err == nil {
		name = decoded
	}
	var parts []string
	for _, p := range strings.Split(name, "/") {
		if p != "" {
			parts = append(parts, p)
		}
	}
	return strings.Join(parts, "/")
}
