package knowledge

import (
	"encoding/json/v2"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type refCase struct {
	Name    string `json:"name"`
	Content string `json:"content"`
	Refs    []struct {
		Kind      string `json:"kind"`
		TargetKey string `json:"targetKey"`
	} `json:"refs"`
}

// refFixture loads the grammar cases the TypeScript parser is driven from too.
func refFixture(t *testing.T) []refCase {
	t.Helper()
	raw, err := os.ReadFile("testdata/refs.json")
	require.NoError(t, err)
	var cases []refCase
	require.NoError(t, json.Unmarshal(raw, &cases))
	require.NotEmpty(t, cases, "fixture is empty")
	return cases
}

func TestParseRefs(t *testing.T) {
	for _, tc := range refFixture(t) {
		t.Run(tc.Name, func(t *testing.T) {
			want := make([]Ref, 0, len(tc.Refs))
			for _, r := range tc.Refs {
				want = append(want, Ref{Kind: RefKind(r.Kind), TargetKey: r.TargetKey})
			}
			got := ParseRefs(tc.Content)
			if len(got) == 0 && len(want) == 0 {
				return
			}
			assert.Equal(t, want, got, "ParseRefs(%q)", tc.Content)
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
			assert.Equal(t, tt.want, NormalizeTitle(tt.in))
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
			assert.Equal(t, tt.want, got)
			// A reference written from the name must resolve to the key the
			// content itself normalizes to.
			assert.Equal(t, NormalizeTitle(tt.in), NormalizeTitle(got))
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
			assert.Equal(t, tt.want, FirstLine(tt.in))
		})
	}
}
