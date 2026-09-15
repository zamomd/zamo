package vaultimport

import (
	"reflect"
	"testing"
)

func TestParseOutlineBuildsATreeFromIndentation(t *testing.T) {
	cases := []struct {
		name, raw string
		preamble  string
		want      []Node
	}{
		{
			name: "two spaces per level, the export's own shape",
			raw:  "- a\n  - b\n    - c\n  - d\n- e\n",
			want: []Node{n("a", n("b", n("c")), n("d")), n("e")},
		},
		{
			name: "tabs",
			raw:  "- a\n\t- b\n\t\t- c\n",
			want: []Node{n("a", n("b", n("c")))},
		},
		{
			name: "four spaces once seen become the unit",
			raw:  "- a\n    - b\n        - c\n",
			want: []Node{n("a", n("b", n("c")))},
		},
		{
			name: "continuation lines join the block with the bullet's indent removed",
			raw:  "- a\n  second line\n\n  after a blank\n  - b\n    more of b\n",
			want: []Node{n("a\nsecond line\n\nafter a blank", n("b\nmore of b"))},
		},
		{
			name: "a bullet indented too deep is a child all the same",
			raw:  "- a\n      - b\n",
			want: []Node{n("a", n("b"))},
		},
		{
			name: "asterisk bullets and empty bullets",
			raw:  "* a\n* \n",
			want: []Node{n("a"), n("")},
		},
		{
			name:     "text before the first bullet is the preamble",
			raw:      "prose\n\nmore\n\n- a\n",
			preamble: "prose\n\nmore",
			want:     []Node{n("a")},
		},
		{
			name:     "no bullets at all",
			raw:      "just text\n",
			preamble: "just text",
			want:     nil,
		},
	}
	for _, c := range cases {
		preamble, got := ParseOutline(c.raw)
		if preamble != c.preamble {
			t.Errorf("%s: preamble = %q, want %q", c.name, preamble, c.preamble)
		}
		if !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s: nodes = %#v\nwant %#v", c.name, got, c.want)
		}
	}
}
