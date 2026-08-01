package knowledge

import (
	"encoding/json"
	"os"
	"reflect"
	"testing"
)

type refCase struct {
	Name    string `json:"name"`
	Content string `json:"content"`
	Refs    []struct {
		Kind      string `json:"kind"`
		TargetKey string `json:"targetKey"`
	} `json:"refs"`
}

func TestParseRefs(t *testing.T) {
	raw, err := os.ReadFile("testdata/refs.json")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	var cases []refCase
	if err := json.Unmarshal(raw, &cases); err != nil {
		t.Fatalf("decode fixture: %v", err)
	}
	if len(cases) == 0 {
		t.Fatal("fixture is empty")
	}
	for _, tc := range cases {
		t.Run(tc.Name, func(t *testing.T) {
			want := make([]Ref, 0, len(tc.Refs))
			for _, r := range tc.Refs {
				want = append(want, Ref{Kind: RefKind(r.Kind), TargetKey: r.TargetKey})
			}
			got := ParseRefs(tc.Content)
			if len(got) == 0 && len(want) == 0 {
				return
			}
			if !reflect.DeepEqual(got, want) {
				t.Errorf("ParseRefs(%q)\n got %v\nwant %v", tc.Content, got, want)
			}
		})
	}
}

func TestNormalizeTitle(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"plain", "Local-first", "local-first"},
		{"trims space", "  Local-first  ", "local-first"},
		{"first line only", "Local-first\n\nA body paragraph.", "local-first"},
		{"strips heading markers", "## Local-first", "local-first"},
		{"strips bullet marker", "- Local-first", "local-first"},
		{"collapses inner whitespace", "Local\tfirst   design", "local first design"},
		{"empty stays empty", "   \n\n", ""},
		{"markers only", "###", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := NormalizeTitle(tt.in); got != tt.want {
				t.Errorf("NormalizeTitle(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestTopicName(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"plain", "Local-first", "Local-first"},
		{"first line only", "Local-first\n\nA body paragraph.", "Local-first"},
		{"strips heading markers", "## Local-First: Why?", "Local-First: Why?"},
		{"strips bullet marker", "- Local-first", "Local-first"},
		{"keeps casing and inner spacing", "Local   First", "Local   First"},
		{"markers only", "###", ""},
		{"empty input", "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := TopicName(tt.in)
			if got != tt.want {
				t.Errorf("TopicName(%q) = %q, want %q", tt.in, got, tt.want)
			}
			// A reference written from the name must resolve to the key the
			// content itself normalizes to.
			if NormalizeTitle(got) != NormalizeTitle(tt.in) {
				t.Errorf("NormalizeTitle(%q) = %q, want %q", got, NormalizeTitle(got), NormalizeTitle(tt.in))
			}
		})
	}
}

func TestFirstLine(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"single line unchanged", "Local-first design", "Local-first design"},
		{"multi-line reduced to first", "Local-first design\n\nA body paragraph.", "Local-first design"},
		{"surrounding whitespace trimmed", "  Local-first design  \nmore", "Local-first design"},
		{"casing and punctuation preserved", "## Local-First: Why?", "## Local-First: Why?"},
		{"empty input", "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := FirstLine(tt.in); got != tt.want {
				t.Errorf("FirstLine(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}
