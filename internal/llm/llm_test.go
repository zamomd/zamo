package llm

import (
	"fmt"
	"strings"
	"testing"

	"github.com/cominddev/comind/internal/knowledge"
	"github.com/stretchr/testify/require"
)

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
				require.ErrorIs(t, err, tt.wantErr)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tt.want, got)
		})
	}
}

func TestParseOpsCapsDepth(t *testing.T) {
	raw := `{"content":"d1","children":[{"content":"d2","children":[{"content":"d3","children":[` +
		`{"content":"d4","children":[{"content":"d5","children":[{"content":"d6","children":[` +
		`{"content":"d7"}]}]}]}]}]}]}`
	got, err := ParseOps([]byte(raw))
	require.NoError(t, err)
	require.Len(t, got, 5, "depth cap")
	require.Equal(t, "d5", got[4].Content)
}

func TestParseOpsCapsCount(t *testing.T) {
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
	require.NoError(t, err)
	require.Len(t, got, 60, "count cap")
}

func TestParseOpsRejectsOversizedReply(t *testing.T) {
	raw := `{"content":"` + strings.Repeat("x", 2<<20) + `"}`
	_, err := ParseOps([]byte(raw))
	require.ErrorIs(t, err, knowledge.ErrLLMBadResponse)
}

func TestParseOpsAcceptsReplyAtSizeLimit(t *testing.T) {
	overhead := len(`{"content":""}`)
	raw := `{"content":"` + strings.Repeat("x", maxReplyBytes-overhead) + `"}`
	require.Len(t, raw, maxReplyBytes, "test setup")
	got, err := ParseOps([]byte(raw))
	require.NoError(t, err, "reply at the size limit was rejected")
	require.Len(t, got, 1)
}

func TestParseOpsIgnoresBracesInsideContent(t *testing.T) {
	content := `a \" b \\ c ` + strings.Repeat("{", 4*maxNestDepth)
	raw := `{"content":"` + content + `"}`
	got, err := ParseOps([]byte(raw))
	require.NoError(t, err, "in-content braces were miscounted as nesting")
	require.Len(t, got, 1)
}

func TestParseOpsRejectsDeeplyNestedReply(t *testing.T) {
	const depth = 500
	raw := strings.Repeat(`{"content":"x","children":[`, depth) +
		`{"content":"leaf"}` + strings.Repeat(`]}`, depth)
	_, err := ParseOps([]byte(raw))
	require.ErrorIs(t, err, knowledge.ErrLLMBadResponse)
}
