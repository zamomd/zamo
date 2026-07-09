package llm

import (
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/cominddev/comind/internal/knowledge"
)

// add builds the add operation ParseOps is expected to emit for a new block.
func add(tempID, parent, content string) knowledge.Op {
	return knowledge.Op{Kind: knowledge.OpAdd, TempID: tempID, Parent: parent, Content: content}
}

func TestParseOps(t *testing.T) {
	tests := []struct {
		name    string
		raw     string
		want    []knowledge.Op
		wantErr error
	}{
		{
			name: "clean nested tree",
			raw: `{
				"content": "# Contexts in Go",
				"children": [
					{"content": "Why context.Context"},
					{"content": "Cancellation", "children": [
						{"content": "example: cancel a goroutine"}
					]}
				]
			}`,
			want: []knowledge.Op{
				add("t1", "", "# Contexts in Go"),
				add("t2", "t1", "Why context.Context"),
				add("t3", "t1", "Cancellation"),
				add("t4", "t3", "example: cancel a goroutine"),
			},
		},
		{
			name: "wrapped in markdown fences",
			raw:  "```json\n{\"content\": \"Root\", \"children\": [{\"content\": \"Child\"}]}\n```",
			want: []knowledge.Op{
				add("t1", "", "Root"),
				add("t2", "t1", "Child"),
			},
		},
		{
			name: "prose around json",
			raw:  "Sure! Here is the tree:\n{\"content\": \"Root\"}\nHope this helps.",
			want: []knowledge.Op{add("t1", "", "Root")},
		},
		{
			name: "empty node dropped and child re-parented",
			raw:  `{"content": "Root", "children": [{"content": "   ", "children": [{"content": "Deep but valid"}]}]}`,
			want: []knowledge.Op{
				add("t1", "", "Root"),
				add("t2", "t1", "Deep but valid"),
			},
		},
		{
			name: "blank node re-parents a sibling group",
			raw:  `{"content":"Root","children":[{"content":"  ","children":[{"content":"A"},{"content":"B"}]}]}`,
			want: []knowledge.Op{
				add("t1", "", "Root"),
				add("t2", "t1", "A"),
				add("t3", "t1", "B"),
			},
		},
		{
			name: "trailing prose containing a brace",
			raw:  "Here you go:\n{\"content\": \"Root\"}\nUse {} for an empty struct.",
			want: []knowledge.Op{add("t1", "", "Root")},
		},
		{
			name: "leading prose containing a brace",
			raw:  "As requested {see note}: {\"content\": \"Root\"}",
			want: []knowledge.Op{add("t1", "", "Root")},
		},
		{
			name: "fenced block then trailing note with a brace",
			raw:  "```json\n{\"content\": \"Root\"}\n```\nLet me know if you want {more}.",
			want: []knowledge.Op{add("t1", "", "Root")},
		},
		{
			name: "multiple top-level objects: first wins",
			raw:  "{\"content\": \"First\"}\n{\"content\": \"Second\"}",
			want: []knowledge.Op{add("t1", "", "First")},
		},
		{
			name: "leading empty object does not shadow the real tree",
			raw:  `{} {"content": "Root"}`,
			want: []knowledge.Op{add("t1", "", "Root")},
		},
		{
			name: "preamble brace pair does not shadow the real tree",
			raw:  "Use {} for an empty struct.\n{\"content\": \"Root\"}",
			want: []knowledge.Op{add("t1", "", "Root")},
		},
		{
			name: "leading object with only unknown fields is skipped",
			raw:  "{\"note\": \"thinking...\"}\n{\"content\": \"Root\", \"children\": [{\"content\": \"Child\"}]}",
			want: []knowledge.Op{
				add("t1", "", "Root"),
				add("t2", "t1", "Child"),
			},
		},
		{
			name:    "malformed json",
			raw:     `{"content": "Root", "children": [`,
			wantErr: knowledge.ErrLLMBadResponse,
		},
		{
			name:    "no json at all",
			raw:     "Sorry, I cannot help with that.",
			wantErr: knowledge.ErrLLMBadResponse,
		},
		{
			name:    "empty input",
			raw:     "",
			wantErr: knowledge.ErrLLMBadResponse,
		},
		{
			name:    "all nodes empty yields bad response",
			raw:     `{"content": "  ", "children": [{"content": ""}]}`,
			wantErr: knowledge.ErrLLMBadResponse,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseOps([]byte(tt.raw))
			if tt.wantErr != nil {
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("ParseOps error = %v, want errors.Is %v", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseOps unexpected error: %v", err)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("ParseOps ops =\n %#v\nwant\n %#v", got, tt.want)
			}
		})
	}
}

func TestParseOpsCapsDepth(t *testing.T) {
	// Root at depth 1 plus a single-child chain running 7 levels deep; the cap is 5.
	raw := `{"content":"d1","children":[{"content":"d2","children":[{"content":"d3","children":[` +
		`{"content":"d4","children":[{"content":"d5","children":[{"content":"d6","children":[` +
		`{"content":"d7"}]}]}]}]}]}]}`
	got, err := ParseOps([]byte(raw))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 5 {
		t.Fatalf("kept %d blocks, want 5 (depth cap)", len(got))
	}
	if got[4].Content != "d5" {
		t.Fatalf("deepest kept content = %q, want d5", got[4].Content)
	}
}

func TestParseOpsCapsCount(t *testing.T) {
	// Root plus 65 children; the cap is 60 blocks total.
	var b strings.Builder
	b.WriteString(`{"content":"Root","children":[`)
	for i := range 65 {
		if i > 0 {
			b.WriteByte(',')
		}
		fmt.Fprintf(&b, `{"content":"c%d"}`, i)
	}
	b.WriteString(`]}`)

	got, err := ParseOps([]byte(b.String()))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 60 {
		t.Fatalf("kept %d blocks, want 60 (count cap)", len(got))
	}
}

func TestParseOpsRejectsOversizedReply(t *testing.T) {
	// The reply crosses the untrusted boundary; a reviewable expansion is a few KB.
	// A multi-megabyte "block" is hostile, not content, and must be refused up front
	// rather than parsed and allocated.
	raw := `{"content":"` + strings.Repeat("x", 2<<20) + `"}`
	if _, err := ParseOps([]byte(raw)); !errors.Is(err, knowledge.ErrLLMBadResponse) {
		t.Fatalf("oversized reply error = %v, want errors.Is %v", err, knowledge.ErrLLMBadResponse)
	}
}

func TestParseOpsRejectsDeeplyNestedReply(t *testing.T) {
	// A chain of deeply nested nodes must not be decoded and walked: without a nesting
	// bound, degenerate input drives O(nesting) recursion in both the JSON decoder and
	// the walk, burning CPU and stack on garbage. Extreme nesting is refused, not capped.
	const depth = 500
	raw := strings.Repeat(`{"content":"x","children":[`, depth) +
		`{"content":"leaf"}` + strings.Repeat(`]}`, depth)
	if _, err := ParseOps([]byte(raw)); !errors.Is(err, knowledge.ErrLLMBadResponse) {
		t.Fatalf("deeply nested reply error = %v, want errors.Is %v", err, knowledge.ErrLLMBadResponse)
	}
}
