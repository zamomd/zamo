package agent_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/zamomd/zamo/internal/agent"
	"github.com/zamomd/zamo/internal/knowledge"
)

func TestWritePolicy(t *testing.T) {
	svc := openService(t)
	// scope: root → a → a1; root → b. Elsewhere: other → x.
	root := seedTopic(t, svc, "Scope", "a", "b")
	sub := seedUnder(t, svc, root[1], "a1")
	other := seedTopic(t, svc, "Other", "x")
	scope, a, b, a1, x := string(root[0]), string(root[1]), string(root[2]), string(sub), string(other[1])

	add := func(temp, parent string) knowledge.Op {
		return knowledge.Op{Kind: knowledge.OpAdd, TempID: temp, Parent: parent, Content: "new"}
	}
	edit := func(id string) knowledge.Op {
		return knowledge.Op{Kind: knowledge.OpEdit, BlockID: knowledge.ID(id), Content: "changed"}
	}
	move := func(id, parent string) knowledge.Op {
		return knowledge.Op{Kind: knowledge.OpMove, BlockID: knowledge.ID(id), Parent: parent}
	}
	del := func(id string) knowledge.Op {
		return knowledge.Op{Kind: knowledge.OpDelete, BlockID: knowledge.ID(id)}
	}
	scoped := agent.WritePolicy{Scope: root[0]}
	scopedOrNew := agent.WritePolicy{Scope: root[0], AllowNewTopic: true}
	newOnly := agent.WritePolicy{AllowNewTopic: true}

	cases := []struct {
		name    string
		policy  agent.WritePolicy
		ops     []knowledge.Op
		refused string // empty means allowed; else a fragment the refusal must name
	}{
		{"add under the scope root", scoped, []knowledge.Op{add("t1", scope)}, ""},
		{"add under a block deep in scope", scoped, []knowledge.Op{add("t1", a1)}, ""},
		{"add under a temp id of the same batch", scoped, []knowledge.Op{add("t1", a), add("t2", "t1")}, ""},
		{"edit the scope root", scoped, []knowledge.Op{edit(scope)}, ""},
		{"edit a block in scope", scoped, []knowledge.Op{edit(a1)}, ""},
		{"move within scope", scoped, []knowledge.Op{move(a1, b)}, ""},
		{"move under a new block", scoped, []knowledge.Op{add("t1", scope), move(a1, "t1")}, ""},
		{"delete a block in scope", scoped, []knowledge.Op{del(b)}, ""},

		{"edit outside scope", scoped, []knowledge.Op{edit(x)}, x},
		{"delete outside scope", scoped, []knowledge.Op{del(x)}, x},
		{"add under a block outside scope", scoped, []knowledge.Op{add("t1", x)}, x},
		{"add under an unknown temp id", scoped, []knowledge.Op{add("t1", "ghost")}, "ghost"},
		{"move a block out of scope", scoped, []knowledge.Op{move(a1, x)}, x},
		{"move a foreign block into scope", scoped, []knowledge.Op{move(x, a)}, x},
		{"move a block to the top level", scoped, []knowledge.Op{move(a, "")}, a},
		{"delete the scope root", scoped, []knowledge.Op{del(scope)}, scope},
		{"move the scope root", scoped, []knowledge.Op{move(scope, a)}, scope},
		{"a good op does not excuse a bad one", scoped, []knowledge.Op{edit(a), edit(x)}, x},

		{"new topic without permission", scoped, []knowledge.Op{add("t1", "")}, "new topic"},
		{"new topic with permission", scopedOrNew, []knowledge.Op{add("t1", ""), add("t2", "t1")}, ""},
		{"new topic and a scoped edit together", scopedOrNew, []knowledge.Op{add("t1", ""), edit(a)}, ""},
		{"new topic only", newOnly, []knowledge.Op{add("t1", ""), add("t2", "t1")}, ""},
		{"new topic only, edit of an existing block", newOnly, []knowledge.Op{edit(a)}, a},
		{"new topic only, add under an existing block", newOnly, []knowledge.Op{add("t1", a)}, a},

		{"zero policy, add", agent.WritePolicy{}, []knowledge.Op{add("t1", "")}, "new topic"},
		{"zero policy, edit", agent.WritePolicy{}, []knowledge.Op{edit(a)}, a},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.policy.Check(t.Context(), svc, tc.ops)

			if tc.refused == "" {
				assert.NoError(t, err)
				return
			}
			require.ErrorIs(t, err, agent.ErrOutOfScope)
			assert.ErrorContains(t, err, tc.refused)
		})
	}
}

func TestWritePolicyPermits(t *testing.T) {
	assert.False(t, agent.WritePolicy{}.Permits())
	assert.True(t, agent.WritePolicy{Scope: "01A"}.Permits())
	assert.True(t, agent.WritePolicy{AllowNewTopic: true}.Permits())
}

func seedUnder(t *testing.T, svc *knowledge.Service, parent knowledge.ID, content string) knowledge.ID {
	t.Helper()
	meta := knowledge.WriteMeta{Author: knowledge.AuthorOwner, Source: knowledge.SourceGUIManual, Title: "seed"}
	applied, err := svc.ApplyOps(t.Context(), meta,
		[]knowledge.Op{{Kind: knowledge.OpAdd, TempID: "n", Parent: string(parent), Content: content}})
	require.NoError(t, err)
	return applied.IDs["n"]
}
