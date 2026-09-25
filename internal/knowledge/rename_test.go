package knowledge

import (
	"encoding/json/v2"
	"errors"
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// equalPublicOps compares ops by everything a caller writes, ignoring the
// guards an expansion attaches. Marshalling is the comparison because it drops
// exactly those fields, for the reason they are unexported: a guard is one
// expansion's reading of the base and never travels.
func equalPublicOps(a, b []Op) bool {
	return slices.EqualFunc(a, b, func(x, y Op) bool {
		jx, _ := json.Marshal(x)
		jy, _ := json.Marshal(y)
		return string(jx) == string(jy)
	})
}

func TestRewriteTopicRef(t *testing.T) {
	tests := []struct {
		name    string
		content string
		oldKey  string
		newName string
		want    string
	}{
		{
			name:    "rewrites a match",
			content: "We use [[Local-first]] here.",
			oldKey:  "local-first",
			newName: "Local-first architecture",
			want:    "We use [[Local-first architecture]] here.",
		},
		{
			name:    "match is case-insensitive",
			content: "See [[LOCAL-FIRST]].",
			oldKey:  "local-first",
			newName: "Offline-first",
			want:    "See [[Offline-first]].",
		},
		{
			name:    "leaves other topics alone",
			content: "[[Zig]] and [[Local-first]].",
			oldKey:  "local-first",
			newName: "Offline-first",
			want:    "[[Zig]] and [[Offline-first]].",
		},
		{
			name:    "leaves an escaped reference alone",
			content: `Literal \[[Zig]] and [[Zig]].`,
			oldKey:  "zig",
			newName: "Ziglang",
			want:    `Literal \[[Zig]] and [[Ziglang]].`,
		},
		{
			name:    "rewrites every occurrence",
			content: "[[Zig]] then [[zig]] again.",
			oldKey:  "zig",
			newName: "Ziglang",
			want:    "[[Ziglang]] then [[Ziglang]] again.",
		},
		{
			name:    "rewrites references in Markdown code",
			content: "```\n[[Zig]]\n```\n`[[Zig]]`",
			oldKey:  "zig",
			newName: "Ziglang",
			want:    "```\n[[Ziglang]]\n```\n`[[Ziglang]]`",
		},
		{
			// An unterminated run is literal text to the reader, which does
			// index the reference after it.
			name:    "rewrites after an unclosed backtick run",
			content: "``still text [[Zig]]",
			oldKey:  "zig",
			newName: "Ziglang",
			want:    "``still text [[Ziglang]]",
		},
		{
			name:    "leaves block references alone",
			content: "((01JT4X9K2M8P0QRSTVWXYZ2345|Zig)) and [[Zig]].",
			oldKey:  "zig",
			newName: "Ziglang",
			want:    "((01JT4X9K2M8P0QRSTVWXYZ2345|Zig)) and [[Ziglang]].",
		},
		{
			// ParseRefs does not index a reference written inside a label, so
			// rewriting one would edit text no rename depends on.
			name:    "leaves a reference inside a block reference label alone",
			content: "((01JT4X9K2M8P0QRSTVWXYZ2345|about [[Zig]])) and [[Zig]].",
			oldKey:  "zig",
			newName: "Ziglang",
			want:    "((01JT4X9K2M8P0QRSTVWXYZ2345|about [[Zig]])) and [[Ziglang]].",
		},
		{
			// The same span without a valid ULID is plain text, and ParseRefs
			// does index the reference inside it.
			name:    "rewrites inside a span that is not a block reference",
			content: "((not-a-ulid|[[Zig]]))",
			oldKey:  "zig",
			newName: "Ziglang",
			want:    "((not-a-ulid|[[Ziglang]]))",
		},
		{
			name:    "a name carrying brackets is refused",
			content: "We use [[Zig]] here.",
			oldKey:  "zig",
			newName: "Zig]] and [[Rust",
			want:    "We use [[Zig]] here.",
		},
		{
			name:    "no match is unchanged",
			content: "Nothing to do here.",
			oldKey:  "zig",
			newName: "Ziglang",
			want:    "Nothing to do here.",
		},
		{
			name:    "an empty key rewrites nothing",
			content: "[[Zig]] and [[]].",
			oldKey:  "",
			newName: "Ziglang",
			want:    "[[Zig]] and [[]].",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, RewriteTopicRef(tt.content, tt.oldKey, tt.newName))
		})
	}
}

// TestRewriteTopicRefRewritesExactlyWhatParseRefsIndexes drives the rewriter
// over the parser's own fixture with every topic key the fixture uses. Two
// properties hold for each pair, and together they say the two sides read the
// same grammar: a key the reader does not index leaves the text byte-identical,
// and a key it does index comes back with that key — and only that key —
// replaced.
func TestRewriteTopicRefRewritesExactlyWhatParseRefsIndexes(t *testing.T) {
	cases := refFixture(t)
	var keys []string
	for _, tc := range cases {
		for _, r := range ParseRefs(tc.Content) {
			if r.Kind == RefTopic && !slices.Contains(keys, r.TargetKey) {
				keys = append(keys, r.TargetKey)
			}
		}
	}

	const newName = "Renamed target"
	newKey := NormalizeTitle(newName)
	for _, tc := range cases {
		for _, key := range keys {
			t.Run(tc.Name+"/"+key, func(t *testing.T) {
				before := ParseRefs(tc.Content)
				got := RewriteTopicRef(tc.Content, key, newName)
				if !slices.Contains(before, Ref{Kind: RefTopic, TargetKey: key}) {
					require.Equal(t, tc.Content, got, "%q is not indexed here, yet renaming it edited the text", key)
					return
				}
				want := slices.Clone(before)
				for i := range want {
					if want[i].Kind == RefTopic && want[i].TargetKey == key {
						want[i].TargetKey = newKey
					}
				}
				assert.Equal(t, want, ParseRefs(got), "renaming %q in %q gave %q", key, tc.Content, got)
			})
		}
	}
}

// renameRepo serves one topic root plus the blocks the refs index reports as
// referencing it.
func renameRepo(root Block, referencing ...Block) *fakeRepo {
	return &fakeRepo{
		blocks:      map[ID]Block{root.ID: root},
		referencing: referencing,
	}
}

func TestRenameTopicRewritesReferencesInOneBatch(t *testing.T) {
	root := blk("01ROOT", "", 0, "Local-first")
	repo := renameRepo(root,
		blk("01NOTE", "01ZIG", 0, "We use [[Local-first]], see [[local-first]] again."),
		blk("01OTHER", "01ZIG", 1, "Unrelated [[Zig]]."),
	)
	svc := NewService(nil, repo, seqIDs())

	require.NoError(t, svc.EditBlockContent(t.Context(), "01ROOT", "Local-first architecture"))
	assert.Equal(t, "local-first", repo.gotReferencingKey, "want the old normalized name")
	assert.Equal(t, 1, repo.applyCalls, "the rename must be one revision group")
	require.Len(t, repo.applyOps, 2, "want the root edit plus one rewrite")
	assert.Equal(t, ID("01ROOT"), repo.applyOps[0].BlockID)
	assert.Equal(t, "Local-first architecture", repo.applyOps[0].Content)
	want := "We use [[Local-first architecture]], see [[Local-first architecture]] again."
	assert.Equal(t, OpEdit, repo.applyOps[1].Kind)
	assert.Equal(t, ID("01NOTE"), repo.applyOps[1].BlockID)
	assert.Equal(t, want, repo.applyOps[1].Content)
	assert.Equal(t, AuthorOwner, repo.applyMeta.Author)
	assert.Equal(t, "Rename topic and rewrite references in 1 block", repo.applyMeta.Title)
	assert.True(t, repo.applyMeta.Atomic, "the batch is not atomic, so the rename and the rewrite could be reviewed apart")
}

func TestRenameBatchTitleCountsTheBlocksItTouched(t *testing.T) {
	repo := renameRepo(blk("01ROOT", "", 0, "Local-first"),
		blk("01A", "01ZIG", 0, "One [[Local-first]] and another [[Local-first]]."),
		blk("01B", "01ZIG", 1, "A second block on [[Local-first]]."))
	svc := NewService(nil, repo, seqIDs())

	require.NoError(t, svc.EditBlockContent(t.Context(), "01ROOT", "Offline-first"))
	assert.Equal(t, "Rename topic and rewrite references in 2 blocks", repo.applyMeta.Title)
}

func TestRenameToAHeadingWritesThePlainName(t *testing.T) {
	repo := renameRepo(blk("01ROOT", "", 0, "# Local-first"),
		blk("01NOTE", "01ZIG", 0, "We use [[Local-first]]."))
	svc := NewService(nil, repo, seqIDs())

	require.NoError(t, svc.EditBlockContent(t.Context(), "01ROOT", "# Offline-first"))
	require.Len(t, repo.applyOps, 2, "want the root edit plus the rewrite")
	assert.Equal(t, "We use [[Offline-first]].", repo.applyOps[1].Content,
		"the heading marker belongs to the root, not the reference")
}

func TestUnnamingAReferencedTopicIsRefused(t *testing.T) {
	repo := renameRepo(blk("01ROOT", "", 0, "Local-first"),
		blk("01NOTE", "01ZIG", 0, "We use [[Local-first]]."))
	svc := NewService(nil, repo, seqIDs())

	err := svc.EditBlockContent(t.Context(), "01ROOT", "###")
	require.ErrorIs(t, err, ErrUnreferenceableTopic, "[[Local-first]] would resolve to nothing")
	assert.Zero(t, repo.applyCalls, "a batch was applied despite the refusal")
}

func TestUnnamingAnUnreferencedTopicIsAllowed(t *testing.T) {
	repo := renameRepo(blk("01ROOT", "", 0, "Local-first"))
	svc := NewService(nil, repo, seqIDs())

	require.NoError(t, svc.EditBlockContent(t.Context(), "01ROOT", "###"))
	assert.Len(t, repo.applyOps, 1, "an unnamed root is a designed state")
}

func TestMovingAReferencedTopicUnderAnotherBlockIsRefused(t *testing.T) {
	repo := renameRepo(blk("01ROOT", "", 0, "Local-first"),
		blk("01NOTE", "01OTHER", 0, "We use [[Local-first]]."))
	svc := NewService(nil, repo, seqIDs())

	_, err := svc.ApplyOps(t.Context(), ownerMeta(), []Op{
		{Kind: OpMove, BlockID: "01ROOT", Parent: "01OTHER"},
	})
	require.ErrorIs(t, err, ErrUnreferenceableTopic)
	assert.Zero(t, repo.applyCalls, "a batch was applied despite the refusal")
}

func TestMovingAnUnreferencedTopicUnderAnotherBlockIsAllowed(t *testing.T) {
	repo := renameRepo(blk("01ROOT", "", 0, "Local-first"))
	svc := NewService(nil, repo, seqIDs())

	_, err := svc.ApplyOps(t.Context(), ownerMeta(), []Op{
		{Kind: OpMove, BlockID: "01ROOT", Parent: "01OTHER"},
	})
	require.NoError(t, err)
	require.Len(t, repo.applyOps, 1)
	assert.Equal(t, OpMove, repo.applyOps[0].Kind)
}

// A move that unnames a topic writes no content, so nothing in the batch would
// notice the root being renamed under it — and the refusal it may raise was
// decided against a name that is no longer there.
func TestMovingATopicGuardsAgainstAConcurrentRename(t *testing.T) {
	repo := renameRepo(blk("01ROOT", "", 0, "Local-first"))
	svc := NewService(nil, repo, seqIDs())

	_, err := svc.ApplyOps(t.Context(), ownerMeta(), []Op{
		{Kind: OpMove, BlockID: "01ROOT", Parent: "01OTHER"},
	})
	require.NoError(t, err)
	got, ok := repo.applyOps[0].ExpectedContent()
	assert.True(t, ok, "guard not set")
	assert.Equal(t, "Local-first", got, "want the name the refusal was decided against")
}

// Two renames in one batch share a source block, so the reference index is read
// twice and a concurrent writer can land between the reads. The rewrite is
// computed from the first read, so the guard has to stay on it: moved to the
// second, it would pass against text the batch is about to overwrite with a
// rewrite of what that text replaced.
func TestGuardKeepsTheTextTheRewriteWasComputedFrom(t *testing.T) {
	note := blk("01NOTE", "01ZIG", 0, "We use [[Alpha]] and [[Beta]].")
	concurrent := blk("01NOTE", "01ZIG", 0, "Rewritten by someone else, still [[Alpha]] and [[Beta]].")
	repo := &fakeRepo{
		blocks: map[ID]Block{
			"01A": blk("01A", "", 0, "Alpha"),
			"01B": blk("01B", "", 1, "Beta"),
		},
		referencingByKey: map[string][]Block{"alpha": {note}, "beta": {concurrent}},
	}
	svc := NewService(nil, repo, seqIDs())

	_, err := svc.ApplyOps(t.Context(), ownerMeta(), []Op{
		{Kind: OpEdit, BlockID: "01A", Content: "Alpha renamed"},
		{Kind: OpEdit, BlockID: "01B", Content: "Beta renamed"},
	})
	require.NoError(t, err)
	i := slices.IndexFunc(repo.applyOps, func(op Op) bool { return op.BlockID == "01NOTE" })
	require.GreaterOrEqual(t, i, 0, "want a rewrite of 01NOTE")
	got, ok := repo.applyOps[i].ExpectedContent()
	assert.True(t, ok, "guard not set")
	assert.Equal(t, note.Content, got)
}

func TestUnnamingATopicTheBatchAlreadyRewritesIsAllowed(t *testing.T) {
	note := blk("01NOTE", "01ZIG", 0, "We use [[Local-first]].")
	repo := renameRepo(blk("01ROOT", "", 0, "Local-first"), note)
	svc := NewService(nil, repo, seqIDs())

	ops := []Op{
		{Kind: OpEdit, BlockID: "01ROOT", Content: "###"},
		{Kind: OpEdit, BlockID: "01NOTE", Content: "We use it."},
	}
	_, err := svc.ApplyOps(t.Context(), ownerMeta(), ops)
	require.NoError(t, err)
	assert.Len(t, repo.applyOps, 2, "want the batch's own two ops: it drops the reference itself")
}

// Naming a source in the batch is not enough to excuse the unnaming: what
// decides is whether the text the batch stores still points at the topic.
func TestUnnamingATopicTheBatchStillReferencesIsRefused(t *testing.T) {
	cases := []struct {
		name string
		op   Op
	}{
		{
			name: "the batch edits the source but keeps the reference",
			op:   Op{Kind: OpEdit, BlockID: "01NOTE", Content: "Still about [[Local-first]]."},
		},
		{
			name: "the batch only moves the source, so its text stands",
			op:   Op{Kind: OpMove, BlockID: "01NOTE", Parent: "01OTHER"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			repo := renameRepo(blk("01ROOT", "", 0, "Local-first"),
				blk("01NOTE", "01ZIG", 0, "We use [[Local-first]]."))
			svc := NewService(nil, repo, seqIDs())

			ops := []Op{{Kind: OpEdit, BlockID: "01ROOT", Content: "###"}, tc.op}
			_, err := svc.ApplyOps(t.Context(), ownerMeta(), ops)
			require.ErrorIs(t, err, ErrUnreferenceableTopic)
			assert.Zero(t, repo.applyCalls, "a batch was applied despite the refusal")
		})
	}
}

// The root's own body dropping the reference is the same case as any other
// source dropping it, and is what lets a topic be unnamed at all.
func TestUnnamingATopicWhoseOnlyReferenceIsItsOwnBodyIsAllowed(t *testing.T) {
	root := blk("01ROOT", "", 0, "Local-first\nSee also [[Local-first]].")
	repo := renameRepo(root, root)
	svc := NewService(nil, repo, seqIDs())

	require.NoError(t, svc.EditBlockContent(t.Context(), "01ROOT", "###\nNo reference left."))
	assert.Len(t, repo.applyOps, 1)
}

// A bracketed name cannot be written inside [[...]], so taking a topic to one
// is the same transition as unnaming it: the backlinks have nothing to point
// at. What is refused is that transition, not the name — see the sibling test.
func TestRenamingAReferencedTopicToABracketedNameIsRefused(t *testing.T) {
	repo := renameRepo(blk("01ROOT", "", 0, "Local-first"),
		blk("01NOTE", "01ZIG", 0, "We use [[Local-first]]."))
	svc := NewService(nil, repo, seqIDs())

	err := svc.EditBlockContent(t.Context(), "01ROOT", "Offline-first [draft]")
	require.ErrorIs(t, err, ErrUnreferenceableTopic, "[[Local-first]] would resolve to nothing")
	assert.Zero(t, repo.applyCalls, "a batch was applied despite the refusal")
}

// Nothing resolved by the old name, so nothing is orphaned and the title is
// ordinary content. Refusing it would kill the whole batch over a checkbox.
func TestRenamingAnUnreferencedTopicToABracketedNameIsAllowed(t *testing.T) {
	repo := renameRepo(blk("01ROOT", "", 0, "Ship list"))
	svc := NewService(nil, repo, seqIDs())

	require.NoError(t, svc.EditBlockContent(t.Context(), "01ROOT", "- [ ] Ship v1"))
	require.Len(t, repo.applyOps, 1)
	assert.Equal(t, "- [ ] Ship v1", repo.applyOps[0].Content)
}

// The root's body naming its own topic by the old name is as stale as any other
// block's, and it is the rename's own op that would leave it so.
func TestRenameFollowsTheRootsOwnSelfReference(t *testing.T) {
	root := blk("01ROOT", "", 0, "Local-first\nSee also [[Local-first]].")
	repo := renameRepo(root, root)
	svc := NewService(nil, repo, seqIDs())

	require.NoError(t, svc.EditBlockContent(t.Context(), "01ROOT", "Offline-first\nSee also [[Local-first]]."))
	require.Len(t, repo.applyOps, 1, "want the root's edit alone and never a second edit of it")
	assert.Equal(t, "Offline-first\nSee also [[Offline-first]].", repo.applyOps[0].Content,
		"the rewrite lands on the rename's own op")
	// One op writes both, so there is no half of it to accept and the block
	// keeps the ordinary per-block controls.
	assert.False(t, repo.applyMeta.Atomic, "the batch is marked atomic though nothing in it can be decided on apart")
}

func TestRenameEmitsNothingForUnchangedSources(t *testing.T) {
	repo := renameRepo(blk("01ROOT", "", 0, "Local-first"),
		blk("01NOTE", "01ZIG", 0, "No reference in this text at all."))
	svc := NewService(nil, repo, seqIDs())

	require.NoError(t, svc.EditBlockContent(t.Context(), "01ROOT", "Offline-first"))
	assert.Len(t, repo.applyOps, 1)
	assert.Equal(t, ownerTitle, repo.applyMeta.Title)
}

func TestEditsThatAreNotRenamesDoNotExpand(t *testing.T) {
	cases := []struct {
		name    string
		block   Block
		content string
	}{
		{
			name:    "a child block is not a topic",
			block:   blk("01KID", "01ROOT", 0, "Local-first"),
			content: "Offline-first",
		},
		{
			name:    "the name is unchanged",
			block:   blk("01ROOT", "", 0, "Local-first"),
			content: "  local-first  \nbody text",
		},
		{
			name:    "the root had no name",
			block:   blk("01ROOT", "", 0, "###"),
			content: "Offline-first",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			repo := renameRepo(tc.block, blk("01NOTE", "01ZIG", 0, "We use [[Local-first]]."))
			svc := NewService(nil, repo, seqIDs())

			require.NoError(t, svc.EditBlockContent(t.Context(), tc.block.ID, tc.content))
			assert.Empty(t, repo.gotReferencingKey, "want no lookup at all")
			assert.Len(t, repo.applyOps, 1)
		})
	}
}

func TestRenameSurfacesLookupErrorWithoutWriting(t *testing.T) {
	boom := errors.New("db down")
	repo := renameRepo(blk("01ROOT", "", 0, "Local-first"))
	repo.referencingErr = boom
	svc := NewService(nil, repo, seqIDs())

	err := svc.EditBlockContent(t.Context(), "01ROOT", "Offline-first")
	require.ErrorIs(t, err, boom)
	assert.Zero(t, repo.applyCalls, "a batch was applied despite the failed lookup")
}

func TestAIRenameExpandsTheSameWayTheOwnersDoes(t *testing.T) {
	repo := renameRepo(blk("01ROOT", "", 0, "Local-first"),
		blk("01NOTE", "01ZIG", 0, "We use [[Local-first]]."))
	svc := NewService(nil, repo, seqIDs())

	meta := WriteMeta{Author: AuthorAI, Source: SourceMCP, Title: "rename the topic"}
	_, err := svc.ApplyOps(t.Context(), meta,
		[]Op{{Kind: OpEdit, BlockID: "01ROOT", Content: "Offline-first"}})
	require.NoError(t, err)
	require.Len(t, repo.applyOps, 2, "want the root edit plus the rewrite of 01NOTE")
	got := repo.applyOps[1]
	assert.Equal(t, OpEdit, got.Kind)
	assert.Equal(t, ID("01NOTE"), got.BlockID)
	assert.Equal(t, "We use [[Offline-first]].", got.Content)
	assert.Equal(t, AuthorAI, repo.applyMeta.Author, "want the rewrite to await review with the rest of the batch")
	assert.Equal(t, "rename the topic", repo.applyMeta.Title, "want the caller's own title kept")
}

// A rename is expanded against the text the batch will actually store, so a
// block the batch already names is skipped only when there is nothing left to
// rewrite in it. Anything else leaves an accepted [[Local-first]] pointing at a
// topic that no longer exists.
func TestRenameRewritesTheContentTheBatchWillStore(t *testing.T) {
	cases := []struct {
		name string
		op   Op
		want []Op // the ops after the root's own rename op
	}{
		{
			name: "an edit that keeps the reference is rewritten in place",
			op:   Op{Kind: OpEdit, BlockID: "01NOTE", Content: "Rewritten by the author, still [[Local-first]]."},
			want: []Op{{Kind: OpEdit, BlockID: "01NOTE", Content: "Rewritten by the author, still [[Offline-first]]."}},
		},
		{
			name: "an edit that drops the reference is left alone",
			op:   Op{Kind: OpEdit, BlockID: "01NOTE", Content: "Rewritten by the author, no reference left."},
			want: []Op{{Kind: OpEdit, BlockID: "01NOTE", Content: "Rewritten by the author, no reference left."}},
		},
		{
			// A move carries no content, so the stored text stands — and stays
			// stale unless the rewrite is appended alongside it.
			name: "a move gets the rewrite the batch cannot carry",
			op:   Op{Kind: OpMove, BlockID: "01NOTE", Parent: "01OTHER"},
			want: []Op{
				{Kind: OpMove, BlockID: "01NOTE", Parent: "01OTHER"},
				{Kind: OpEdit, BlockID: "01NOTE", Content: "We use [[Offline-first]]."},
			},
		},
		{
			name: "a delete has nothing to rewrite",
			op:   Op{Kind: OpDelete, BlockID: "01NOTE"},
			want: []Op{{Kind: OpDelete, BlockID: "01NOTE"}},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			repo := renameRepo(blk("01ROOT", "", 0, "Local-first"),
				blk("01NOTE", "01ZIG", 0, "We use [[Local-first]]."))
			svc := NewService(nil, repo, seqIDs())

			meta := WriteMeta{Author: AuthorAI, Source: SourceMCP, Title: "rename and more"}
			ops := []Op{{Kind: OpEdit, BlockID: "01ROOT", Content: "Offline-first"}, tc.op}
			_, err := svc.ApplyOps(t.Context(), meta, ops)
			require.NoError(t, err)
			assert.Equal(t, ID("01ROOT"), repo.applyOps[0].BlockID, "want the rename itself")
			assert.Equal(t, "Offline-first", repo.applyOps[0].Content, "want the rename itself")
			got := repo.applyOps[1:]
			assert.True(t, equalPublicOps(got, tc.want), "ops after the rename = %+v, want %+v", got, tc.want)
			assert.True(t, equalPublicOps(ops, []Op{{Kind: OpEdit, BlockID: "01ROOT", Content: "Offline-first"}, tc.op}),
				"the caller's slice was rewritten under it: %+v", ops)
		})
	}
}

// One block, one written content: a rewrite lands on the op the batch already
// writes it through instead of adding a second one.
func TestRenameWritesEachBlocksContentThroughOneOp(t *testing.T) {
	note := blk("01NOTE", "01ZIG", 0, "Both [[Alpha]] and [[Beta]].")
	repo := &fakeRepo{
		blocks: map[ID]Block{
			"01A":    blk("01A", "", 0, "Alpha"),
			"01B":    blk("01B", "", 1, "Beta"),
			"01NOTE": note,
		},
		referencingByKey: map[string][]Block{"alpha": {note}, "beta": {note}},
	}
	svc := NewService(nil, repo, seqIDs())

	meta := WriteMeta{Author: AuthorAI, Source: SourceMCP, Title: "rename both topics"}
	_, err := svc.ApplyOps(t.Context(), meta, []Op{
		{Kind: OpEdit, BlockID: "01A", Content: "Alpha prime"},
		{Kind: OpEdit, BlockID: "01B", Content: "Beta prime"},
		{Kind: OpEdit, BlockID: "01NOTE", Content: "Still both [[Alpha]] and [[Beta]]."},
	})
	require.NoError(t, err)
	require.Len(t, repo.applyOps, 3, "want the author's three ops and no fourth for 01NOTE")
	assert.Equal(t, ID("01NOTE"), repo.applyOps[2].BlockID)
	assert.Equal(t, "Still both [[Alpha prime]] and [[Beta prime]].", repo.applyOps[2].Content)
}

func TestTwoRenamesInOneBatchChainOntoOneRewrite(t *testing.T) {
	note := blk("01NOTE", "01ZIG", 0, "Both [[Alpha]] and [[Beta]].")
	repo := &fakeRepo{
		blocks: map[ID]Block{
			"01A": blk("01A", "", 0, "Alpha"),
			"01B": blk("01B", "", 1, "Beta"),
		},
		referencingByKey: map[string][]Block{"alpha": {note}, "beta": {note}},
	}
	svc := NewService(nil, repo, seqIDs())

	meta := WriteMeta{Author: AuthorAI, Source: SourceMCP, Title: "rename both topics"}
	_, err := svc.ApplyOps(t.Context(), meta, []Op{
		{Kind: OpEdit, BlockID: "01A", Content: "Alpha prime"},
		{Kind: OpEdit, BlockID: "01B", Content: "Beta prime"},
	})
	require.NoError(t, err)
	require.Len(t, repo.applyOps, 3, "want the two renames plus exactly one rewrite of 01NOTE")
	assert.Equal(t, ID("01NOTE"), repo.applyOps[2].BlockID)
	assert.Equal(t, "Both [[Alpha prime]] and [[Beta prime]].", repo.applyOps[2].Content)
}

// A root edited several times in one batch is renamed once, from the name the
// backlinks resolve by to the one the batch finally leaves it with. A rewrite
// per edit would take them to a name only an intermediate op held, and no topic
// would answer to it once the batch landed.
func TestSeveralEditsOfOneRootRewriteReferencesOnce(t *testing.T) {
	cases := []struct {
		name     string
		root     string   // the root's stored content
		edits    []string // the successive contents the batch writes to it
		wantRoot []string // the content each of those edits carries once expanded
		wantRest []Op     // the ops after the root's own edits
		lookup   string   // the key the backlinks were looked up by, "" for no lookup
	}{
		{
			name:     "renamed twice",
			root:     "Local-first",
			edits:    []string{"Offline-first", "Final"},
			wantRoot: []string{"Offline-first", "Final"},
			wantRest: []Op{{Kind: OpEdit, BlockID: "01NOTE", Content: "We use [[Final]]."}},
			lookup:   "local-first",
		},
		{
			name:     "renamed, then edited again without touching the name",
			root:     "Local-first",
			edits:    []string{"Offline-first", "Offline-first\nWith a body."},
			wantRoot: []string{"Offline-first", "Offline-first\nWith a body."},
			wantRest: []Op{{Kind: OpEdit, BlockID: "01NOTE", Content: "We use [[Offline-first]]."}},
			lookup:   "local-first",
		},
		{
			name:     "renamed back to the name it started with",
			root:     "Local-first",
			edits:    []string{"Offline-first", "Local-first"},
			wantRoot: []string{"Offline-first", "Local-first"},
			lookup:   "",
		},
		{
			name:     "renamed twice while naming itself",
			root:     "Local-first\nSee also [[Local-first]].",
			edits:    []string{"Offline-first\nSee also [[Local-first]].", "Final\nSee also [[Local-first]]."},
			wantRoot: []string{"Offline-first\nSee also [[Local-first]].", "Final\nSee also [[Final]]."},
			wantRest: []Op{{Kind: OpEdit, BlockID: "01NOTE", Content: "We use [[Final]]."}},
			lookup:   "local-first",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root := blk("01ROOT", "", 0, tc.root)
			repo := renameRepo(root, root, blk("01NOTE", "01ZIG", 0, "We use [[Local-first]]."))
			svc := NewService(nil, repo, seqIDs())

			ops := make([]Op, len(tc.edits))
			for i, content := range tc.edits {
				ops[i] = Op{Kind: OpEdit, BlockID: "01ROOT", Content: content}
			}
			_, err := svc.ApplyOps(t.Context(), ownerMeta(), ops)
			require.NoError(t, err)
			assert.Equal(t, tc.lookup, repo.gotReferencingKey, "backlinks lookup key")
			require.GreaterOrEqual(t, len(repo.applyOps), len(tc.wantRoot), "want the root's edits")
			for i, want := range tc.wantRoot {
				got := repo.applyOps[i]
				assert.Equal(t, OpEdit, got.Kind, "op %d", i)
				assert.Equal(t, ID("01ROOT"), got.BlockID, "op %d", i)
				assert.Equal(t, want, got.Content, "op %d", i)
			}
			got := repo.applyOps[len(tc.wantRoot):]
			assert.True(t, equalPublicOps(got, tc.wantRest), "ops after the root's edits = %+v, want %+v", got, tc.wantRest)
		})
	}
}

func TestRenameOntoATakenNameFails(t *testing.T) {
	repo := renameRepo(blk("01ROOT", "", 0, "Local-first"),
		blk("01NOTE", "01ZIG", 0, "We use [[Local-first]]."))
	repo.applyErr = ErrDuplicateTopic
	svc := NewService(nil, repo, seqIDs())

	require.ErrorIs(t, svc.EditBlockContent(t.Context(), "01ROOT", "Offline-first"), ErrDuplicateTopic)
}
