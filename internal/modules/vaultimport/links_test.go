package vaultimport

import (
	"reflect"
	"testing"
)

func TestRewriteWikilinksDropsAliasAndAnchor(t *testing.T) {
	cases := []struct {
		in, want string
		kinds    []string
	}{
		{"see [[Go]]", "see [[Go]]", nil},
		{"see [[Go|the language]]", "see [[Go]]", []string{WarnAlias}},
		{"see [[Go#Contexts]]", "see [[Go]]", []string{WarnAnchor}},
		{"see [[Go^abc123]]", "see [[Go]]", []string{WarnAnchor}},
		{"see [[Go#Contexts|ctx]]", "see [[Go]]", []string{WarnAnchor, WarnAlias}},
		{"see ![[diagram.png]]", "see [[diagram.png]]", []string{WarnEmbed}},
		{"see [[#Local]]", "see [[#Local]]", []string{WarnAnchor}},
		{`see \[[Go|x]]`, `see \[[Go|x]]`, nil},
		{"see ((01ARZ3NDEKTSV4RRFFQ69G5FAV|a [[Go|x]] label))", "see ((01ARZ3NDEKTSV4RRFFQ69G5FAV|a [[Go|x]] label))", nil},
		{"[[A|x]] and [[B#y]]\n[[C|z]]", "[[A]] and [[B]]\n[[C]]", []string{WarnAlias, WarnAnchor, WarnAlias}},
	}
	for _, c := range cases {
		got, warns := RewriteWikilinks(c.in)
		if got != c.want {
			t.Errorf("RewriteWikilinks(%q) = %q, want %q", c.in, got, c.want)
		}
		var kinds []string
		for _, w := range warns {
			kinds = append(kinds, w.Kind)
		}
		if !reflect.DeepEqual(kinds, c.kinds) {
			t.Errorf("RewriteWikilinks(%q) warnings = %v, want %v", c.in, kinds, c.kinds)
		}
	}
}

func TestRewriteWikilinksKeepsTheOriginalInDetail(t *testing.T) {
	_, warns := RewriteWikilinks("x [[Go#Contexts|ctx]] y")
	if len(warns) == 0 || warns[0].Detail != "[[Go#Contexts|ctx]]" {
		t.Fatalf("warnings = %+v, want the original link as detail", warns)
	}
}

func TestRewriteTags(t *testing.T) {
	cases := []struct{ in, want string }{
		{"a #tag here", "a [[tag]] here"},
		{"#first word", "[[first]] word"},
		{"tags: #a, #b", "tags: [[a]], [[b]]"},
		{"a #[[multi word]] tag", "a [[multi word]] tag"},
		{"# Heading", "# Heading"},
		{"## Heading", "## Heading"},
		{"http://x.y/#frag", "http://x.y/#frag"},
		{"C# is not a tag", "C# is not a tag"},
		{"see [[Go #1]]", "see [[Go #1]]"},
		{"a,#b and c.#d", "a,[[b]] and c.[[d]]"},
		{"end #tag. next", "end [[tag]]. next"},
		{"end #tag, next", "end [[tag]], next"},
		{"paren (#tag)", "paren (#tag)"},
		{"#tag)", "[[tag)]]"},
		{"trail #tag:", "trail [[tag]]:"},
		{"#a#b", "[[a#b]]"},
		{"((01ARZ3NDEKTSV4RRFFQ69G5FAV|#label))", "((01ARZ3NDEKTSV4RRFFQ69G5FAV|#label))"},
		{`\#escaped`, `\#escaped`},
		{"#тег", "[[тег]]"},
	}
	for _, c := range cases {
		if got := RewriteTags(c.in); got != c.want {
			t.Errorf("RewriteTags(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestRewriteBlockRefs(t *testing.T) {
	upper := func(target, label string) (string, string, bool) {
		if target == "skip" {
			return "", "", false
		}
		if label == "" {
			label = "L"
		}
		return "X" + target, label, true
	}
	cases := []struct{ in, want string }{
		{"see ((abc))", "see ((Xabc|L))"},
		{"see ((abc|label))", "see ((Xabc|label))"},
		{"see (( abc ))", "see ((Xabc|L))"},
		{"see ((skip))", "see ((skip))"},
		{`see \((abc))`, `see \((abc))`},
		{"see [[T ((abc))]]", "see [[T ((abc))]]"},
		{"((a)) and ((b|l))", "((Xa|L)) and ((Xb|l))"},
		{"unterminated ((abc", "unterminated ((abc"},
	}
	for _, c := range cases {
		if got := RewriteBlockRefs(c.in, upper); got != c.want {
			t.Errorf("RewriteBlockRefs(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}
