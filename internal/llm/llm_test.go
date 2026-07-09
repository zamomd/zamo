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
