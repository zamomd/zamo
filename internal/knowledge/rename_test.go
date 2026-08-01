package knowledge

import (
	"errors"
	"slices"
	"testing"
)

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
			name:    "rewrites every occurrence",
			content: "[[Zig]] then [[zig]] again.",
			oldKey:  "zig",
			newName: "Ziglang",
			want:    "[[Ziglang]] then [[Ziglang]] again.",
		},
		{
			name:    "leaves fenced code alone",
			content: "```\n[[Zig]]\n```\n[[Zig]]",
			oldKey:  "zig",
			newName: "Ziglang",
			want:    "```\n[[Zig]]\n```\n[[Ziglang]]",
		},
		{
			name:    "a shorter run does not close a fence, so its content stays code",
			content: "````\n[[Zig]]\n```\n[[Zig]]",
			oldKey:  "zig",
			newName: "Ziglang",
			want:    "````\n[[Zig]]\n```\n[[Zig]]",
		},
		{
			name:    "rewrites after a closer longer than its opener",
			content: "```\n[[Zig]]\n`````\n[[Zig]]",
			oldKey:  "zig",
			newName: "Ziglang",
			want:    "```\n[[Zig]]\n`````\n[[Ziglang]]",
		},
		{
			name:    "an info string does not close a fence",
			content: "```\n[[Zig]]\n```go\n[[Zig]]\n```\n[[Zig]]",
			oldKey:  "zig",
			newName: "Ziglang",
			want:    "```\n[[Zig]]\n```go\n[[Zig]]\n```\n[[Ziglang]]",
		},
		{
			name:    "a tilde line does not close a backtick fence",
			content: "```\n[[Zig]]\n~~~\n[[Zig]]",
			oldKey:  "zig",
			newName: "Ziglang",
			want:    "```\n[[Zig]]\n~~~\n[[Zig]]",
		},
		{
			name:    "tilde fences follow the same rules",
			content: "~~~~\n[[Zig]]\n~~~\n[[Zig]]",
			oldKey:  "zig",
			newName: "Ziglang",
			want:    "~~~~\n[[Zig]]\n~~~\n[[Zig]]",
		},
		{
			name:    "a backtick in the info string opens no fence",
			content: "``` a`b\n[[Zig]]",
			oldKey:  "zig",
			newName: "Ziglang",
			want:    "``` a`b\n[[Ziglang]]",
		},
		{
			// ParseRefs does not index a reference inside a backtick span, so
			// there is no backlink a rewrite would keep true — only a code
			// sample it would silently edit.
			name:    "leaves inline code alone",
			content: "`[[Zig]]` and [[Zig]].",
			oldKey:  "zig",
			newName: "Ziglang",
			want:    "`[[Zig]]` and [[Ziglang]].",
		},
		{
			name:    "leaves a multi-backtick span alone",
			content: "Write ``[[Zig]]`` for [[Zig]].",
			oldKey:  "zig",
			newName: "Ziglang",
			want:    "Write ``[[Zig]]`` for [[Ziglang]].",
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
			if got := RewriteTopicRef(tt.content, tt.oldKey, tt.newName); got != tt.want {
				t.Errorf("RewriteTopicRef()\n got %q\nwant %q", got, tt.want)
			}
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
					if got != tc.Content {
						t.Fatalf("%q is not indexed here, yet renaming it edited the text\n got %q\nwant %q",
							key, got, tc.Content)
					}
					return
				}
				want := slices.Clone(before)
				for i := range want {
					if want[i].Kind == RefTopic && want[i].TargetKey == key {
						want[i].TargetKey = newKey
					}
				}
				if after := ParseRefs(got); !slices.Equal(after, want) {
					t.Errorf("renaming %q in %q gave %q\n refs %v\n want %v", key, tc.Content, got, after, want)
				}
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
	svc := NewService(nil, repo, seqIDs(), nil)

	if err := svc.EditBlockContent(t.Context(), "01ROOT", "Local-first architecture"); err != nil {
		t.Fatalf("EditBlockContent: %v", err)
	}
	if repo.gotReferencingKey != "local-first" {
		t.Errorf("looked up %q, want the old normalized name", repo.gotReferencingKey)
	}
	if repo.applyCalls != 1 {
		t.Errorf("storage saw %d batches, want 1 so the rename is one revision group", repo.applyCalls)
	}
	if len(repo.applyOps) != 2 {
		t.Fatalf("ops = %+v, want the root edit plus one rewrite", repo.applyOps)
	}
	if got := repo.applyOps[0]; got.BlockID != "01ROOT" || got.Content != "Local-first architecture" {
		t.Errorf("op 0 = %+v, want the root's own edit", got)
	}
	want := "We use [[Local-first architecture]], see [[Local-first architecture]] again."
	if got := repo.applyOps[1]; got.Kind != OpEdit || got.BlockID != "01NOTE" || got.Content != want {
		t.Errorf("op 1 = %+v, want an edit of 01NOTE with %q", got, want)
	}
	if repo.applyMeta.Author != AuthorOwner || repo.applyMeta.Title != "Rename topic and rewrite references in 1 block" {
		t.Errorf("meta = %+v, want an owner batch titled by the rename", repo.applyMeta)
	}
}

func TestRenameBatchTitleCountsTheBlocksItTouched(t *testing.T) {
	repo := renameRepo(blk("01ROOT", "", 0, "Local-first"),
		blk("01A", "01ZIG", 0, "One [[Local-first]] and another [[Local-first]]."),
		blk("01B", "01ZIG", 1, "A second block on [[Local-first]]."))
	svc := NewService(nil, repo, seqIDs(), nil)

	if err := svc.EditBlockContent(t.Context(), "01ROOT", "Offline-first"); err != nil {
		t.Fatalf("EditBlockContent: %v", err)
	}
	if want := "Rename topic and rewrite references in 2 blocks"; repo.applyMeta.Title != want {
		t.Errorf("title = %q, want %q", repo.applyMeta.Title, want)
	}
}

func TestRenameToAHeadingWritesThePlainName(t *testing.T) {
	repo := renameRepo(blk("01ROOT", "", 0, "# Local-first"),
		blk("01NOTE", "01ZIG", 0, "We use [[Local-first]]."))
	svc := NewService(nil, repo, seqIDs(), nil)

	if err := svc.EditBlockContent(t.Context(), "01ROOT", "# Offline-first"); err != nil {
		t.Fatalf("EditBlockContent: %v", err)
	}
	if len(repo.applyOps) != 2 {
		t.Fatalf("ops = %+v, want the root edit plus the rewrite", repo.applyOps)
	}
	if got, want := repo.applyOps[1].Content, "We use [[Offline-first]]."; got != want {
		t.Errorf("rewritten content = %q, want %q: the heading marker belongs to the root, not the reference", got, want)
	}
}

func TestUnnamingAReferencedTopicIsRefused(t *testing.T) {
	repo := renameRepo(blk("01ROOT", "", 0, "Local-first"),
		blk("01NOTE", "01ZIG", 0, "We use [[Local-first]]."))
	svc := NewService(nil, repo, seqIDs(), nil)

	err := svc.EditBlockContent(t.Context(), "01ROOT", "###")
	if !errors.Is(err, ErrUnreferenceableTopic) {
		t.Errorf("error = %v, want ErrUnreferenceableTopic: [[Local-first]] would resolve to nothing", err)
	}
	if repo.applyCalls != 0 {
		t.Errorf("a batch was applied despite the refusal: %d calls", repo.applyCalls)
	}
}

func TestUnnamingAnUnreferencedTopicIsAllowed(t *testing.T) {
	repo := renameRepo(blk("01ROOT", "", 0, "Local-first"))
	svc := NewService(nil, repo, seqIDs(), nil)

	if err := svc.EditBlockContent(t.Context(), "01ROOT", "###"); err != nil {
		t.Fatalf("EditBlockContent: %v", err)
	}
	if len(repo.applyOps) != 1 {
		t.Errorf("ops = %+v, want the root edit: an unnamed root is a designed state", repo.applyOps)
	}
}

func TestUnnamingATopicTheBatchAlreadyRewritesIsAllowed(t *testing.T) {
	note := blk("01NOTE", "01ZIG", 0, "We use [[Local-first]].")
	repo := renameRepo(blk("01ROOT", "", 0, "Local-first"), note)
	svc := NewService(nil, repo, seqIDs(), nil)

	ops := []Op{
		{Kind: OpEdit, BlockID: "01ROOT", Content: "###"},
		{Kind: OpEdit, BlockID: "01NOTE", Content: "We use it."},
	}
	if _, err := svc.ApplyOps(t.Context(), ownerMeta(), ops); err != nil {
		t.Fatalf("ApplyOps: %v", err)
	}
	if len(repo.applyOps) != 2 {
		t.Errorf("ops = %+v, want the batch's own two ops: it drops the reference itself", repo.applyOps)
	}
}

// A bracketed name cannot be written inside [[...]], so taking a topic to one
// is the same transition as unnaming it: the backlinks have nothing to point
// at. What is refused is that transition, not the name — see the sibling test.
func TestRenamingAReferencedTopicToABracketedNameIsRefused(t *testing.T) {
	repo := renameRepo(blk("01ROOT", "", 0, "Local-first"),
		blk("01NOTE", "01ZIG", 0, "We use [[Local-first]]."))
	svc := NewService(nil, repo, seqIDs(), nil)

	err := svc.EditBlockContent(t.Context(), "01ROOT", "Offline-first [draft]")
	if !errors.Is(err, ErrUnreferenceableTopic) {
		t.Errorf("error = %v, want ErrUnreferenceableTopic: [[Local-first]] would resolve to nothing", err)
	}
	if repo.applyCalls != 0 {
		t.Errorf("a batch was applied despite the refusal: %d calls", repo.applyCalls)
	}
}

// Nothing resolved by the old name, so nothing is orphaned and the title is
// ordinary content. Refusing it would kill the whole batch over a checkbox.
func TestRenamingAnUnreferencedTopicToABracketedNameIsAllowed(t *testing.T) {
	repo := renameRepo(blk("01ROOT", "", 0, "Ship list"))
	svc := NewService(nil, repo, seqIDs(), nil)

	if err := svc.EditBlockContent(t.Context(), "01ROOT", "- [ ] Ship v1"); err != nil {
		t.Fatalf("EditBlockContent: %v", err)
	}
	if len(repo.applyOps) != 1 || repo.applyOps[0].Content != "- [ ] Ship v1" {
		t.Errorf("ops = %+v, want the edit applied verbatim", repo.applyOps)
	}
}

func TestRenameSkipsTheRootsOwnSelfReference(t *testing.T) {
	root := blk("01ROOT", "", 0, "Local-first\nSee also [[Local-first]].")
	repo := renameRepo(root, root)
	svc := NewService(nil, repo, seqIDs(), nil)

	if err := svc.EditBlockContent(t.Context(), "01ROOT", "Offline-first\nSee also [[Local-first]]."); err != nil {
		t.Fatalf("EditBlockContent: %v", err)
	}
	if len(repo.applyOps) != 1 {
		t.Fatalf("ops = %+v, want the root's edit alone and never a second edit of it", repo.applyOps)
	}
	if repo.applyOps[0].Content != "Offline-first\nSee also [[Local-first]]." {
		t.Errorf("root content = %q, want the caller's text untouched", repo.applyOps[0].Content)
	}
}

func TestRenameEmitsNothingForUnchangedSources(t *testing.T) {
	repo := renameRepo(blk("01ROOT", "", 0, "Local-first"),
		blk("01NOTE", "01ZIG", 0, "No reference in this text at all."))
	svc := NewService(nil, repo, seqIDs(), nil)

	if err := svc.EditBlockContent(t.Context(), "01ROOT", "Offline-first"); err != nil {
		t.Fatalf("EditBlockContent: %v", err)
	}
	if len(repo.applyOps) != 1 {
		t.Errorf("ops = %+v, want only the root edit", repo.applyOps)
	}
	if repo.applyMeta.Title != ownerTitle {
		t.Errorf("title = %q, want the plain owner title when nothing was rewritten", repo.applyMeta.Title)
	}
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
			svc := NewService(nil, repo, seqIDs(), nil)

			if err := svc.EditBlockContent(t.Context(), tc.block.ID, tc.content); err != nil {
				t.Fatalf("EditBlockContent: %v", err)
			}
			if repo.gotReferencingKey != "" {
				t.Errorf("looked up references for %q, want no lookup at all", repo.gotReferencingKey)
			}
			if len(repo.applyOps) != 1 {
				t.Errorf("ops = %+v, want the single edit", repo.applyOps)
			}
		})
	}
}

func TestRenameSurfacesLookupErrorWithoutWriting(t *testing.T) {
	boom := errors.New("db down")
	repo := renameRepo(blk("01ROOT", "", 0, "Local-first"))
	repo.referencingErr = boom
	svc := NewService(nil, repo, seqIDs(), nil)

	err := svc.EditBlockContent(t.Context(), "01ROOT", "Offline-first")
	if !errors.Is(err, boom) {
		t.Errorf("error = %v, want the storage read error", err)
	}
	if repo.applyCalls != 0 {
		t.Errorf("a batch was applied despite the failed lookup: %d calls", repo.applyCalls)
	}
}

func TestAIRenameExpandsTheSameWayTheOwnersDoes(t *testing.T) {
	repo := renameRepo(blk("01ROOT", "", 0, "Local-first"),
		blk("01NOTE", "01ZIG", 0, "We use [[Local-first]]."))
	svc := NewService(nil, repo, seqIDs(), nil)

	meta := WriteMeta{Author: AuthorAI, Source: SourceMCP, Title: "rename the topic"}
	if _, err := svc.ApplyOps(t.Context(), meta,
		[]Op{{Kind: OpEdit, BlockID: "01ROOT", Content: "Offline-first"}}); err != nil {
		t.Fatalf("ApplyOps: %v", err)
	}
	if len(repo.applyOps) != 2 {
		t.Fatalf("ops = %+v, want the root edit plus the rewrite of 01NOTE", repo.applyOps)
	}
	got := repo.applyOps[1]
	if got.Kind != OpEdit || got.BlockID != "01NOTE" || got.Content != "We use [[Offline-first]]." {
		t.Errorf("op 1 = %+v, want 01NOTE rewritten to the new name", got)
	}
	if repo.applyMeta.Author != AuthorAI {
		t.Errorf("author = %q, want the rewrite to await review with the rest of the batch", repo.applyMeta.Author)
	}
	if repo.applyMeta.Title != "rename the topic" {
		t.Errorf("title = %q, want the caller's own title kept", repo.applyMeta.Title)
	}
}

func TestRenameLeavesBlocksTheBatchAlreadyNamesToTheirOwnOps(t *testing.T) {
	referencing := blk("01NOTE", "01ZIG", 0, "We use [[Local-first]].")
	cases := []struct {
		name string
		op   Op
	}{
		{
			name: "the batch edits the referencing block itself",
			op:   Op{Kind: OpEdit, BlockID: "01NOTE", Content: "Rewritten by the author, still [[Local-first]]."},
		},
		{
			name: "the batch deletes the referencing block",
			op:   Op{Kind: OpDelete, BlockID: "01NOTE"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			repo := renameRepo(blk("01ROOT", "", 0, "Local-first"), referencing)
			svc := NewService(nil, repo, seqIDs(), nil)

			meta := WriteMeta{Author: AuthorAI, Source: SourceMCP, Title: "rename and edit"}
			ops := []Op{{Kind: OpEdit, BlockID: "01ROOT", Content: "Offline-first"}, tc.op}
			if _, err := svc.ApplyOps(t.Context(), meta, ops); err != nil {
				t.Fatalf("ApplyOps: %v", err)
			}
			if len(repo.applyOps) != 2 {
				t.Fatalf("ops = %+v, want the batch's own two ops and no third op for 01NOTE", repo.applyOps)
			}
			if got := repo.applyOps[1]; got != tc.op {
				t.Errorf("op 1 = %+v, want the author's op untouched", got)
			}
		})
	}
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
	svc := NewService(nil, repo, seqIDs(), nil)

	meta := WriteMeta{Author: AuthorAI, Source: SourceMCP, Title: "rename both topics"}
	if _, err := svc.ApplyOps(t.Context(), meta, []Op{
		{Kind: OpEdit, BlockID: "01A", Content: "Alpha prime"},
		{Kind: OpEdit, BlockID: "01B", Content: "Beta prime"},
	}); err != nil {
		t.Fatalf("ApplyOps: %v", err)
	}
	if len(repo.applyOps) != 3 {
		t.Fatalf("ops = %+v, want the two renames plus exactly one rewrite of 01NOTE", repo.applyOps)
	}
	want := "Both [[Alpha prime]] and [[Beta prime]]."
	if got := repo.applyOps[2]; got.BlockID != "01NOTE" || got.Content != want {
		t.Errorf("op 2 = %+v, want 01NOTE carrying both new names (%q)", got, want)
	}
}

func TestRenameOntoATakenNameFails(t *testing.T) {
	repo := renameRepo(blk("01ROOT", "", 0, "Local-first"),
		blk("01NOTE", "01ZIG", 0, "We use [[Local-first]]."))
	repo.applyErr = ErrDuplicateTopic
	svc := NewService(nil, repo, seqIDs(), nil)

	if err := svc.EditBlockContent(t.Context(), "01ROOT", "Offline-first"); !errors.Is(err, ErrDuplicateTopic) {
		t.Errorf("error = %v, want ErrDuplicateTopic", err)
	}
}
