package knowledge

import (
	"errors"
	"reflect"
	"testing"
	"time"
)

// newLinkService wires a Service around the fake repo; links touch neither
// the LLM nor the writer.
func newLinkService(repo *fakeRepo) *Service {
	return NewService(&fakeLLM{}, repo, func() ID { return "01NEW" }, &fakeFileWriter{},
		clockAt(time.Date(2026, 7, 18, 12, 0, 0, 0, time.UTC)))
}

func TestLinkedBlocksHydratesBothDirections(t *testing.T) {
	// The panel shows each neighbour's content and which side of the edge the
	// queried block is on — an incoming example and an outgoing related link
	// are different affordances.
	repo := &fakeRepo{
		edges: []Edge{
			{From: "01EX", To: "01ME", Relation: RelationExample},
			{From: "01ME", To: "01REL", Relation: RelationRelated},
		},
		blocks: map[ID]Block{
			"01EX":  {ID: "01EX", Content: "an example"},
			"01REL": {ID: "01REL", Content: "related theory"},
		},
	}
	svc := newLinkService(repo)

	got, err := svc.LinkedBlocks(t.Context(), "01ME")
	if err != nil {
		t.Fatalf("LinkedBlocks: %v", err)
	}
	if repo.gotEdges != "01ME" {
		t.Errorf("GetEdges asked for %q, want 01ME", repo.gotEdges)
	}
	want := []LinkedBlock{
		{Relation: RelationExample, Outgoing: false, Block: Block{ID: "01EX", Content: "an example"}},
		{Relation: RelationRelated, Outgoing: true, Block: Block{ID: "01REL", Content: "related theory"}},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("LinkedBlocks = %+v, want %+v", got, want)
	}
}

func TestLinkedBlocksDropsVanishedNeighbour(t *testing.T) {
	// An edge whose neighbour vanished mid-read is dropped, not an error: the
	// next read simply no longer sees the edge either.
	repo := &fakeRepo{
		edges: []Edge{
			{From: "01ME", To: "01GONE", Relation: RelationRelated},
			{From: "01ME", To: "01OK", Relation: RelationRelated},
		},
		blocks: map[ID]Block{"01OK": {ID: "01OK", Content: "still here"}},
	}
	got, err := newLinkService(repo).LinkedBlocks(t.Context(), "01ME")
	if err != nil {
		t.Fatalf("LinkedBlocks: %v", err)
	}
	want := []LinkedBlock{
		{Relation: RelationRelated, Outgoing: true, Block: Block{ID: "01OK", Content: "still here"}},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("LinkedBlocks = %+v, want %+v", got, want)
	}
}

func TestCreateLinkRejectsSelfLink(t *testing.T) {
	repo := &fakeRepo{}
	err := newLinkService(repo).CreateLink(t.Context(), "01ME", "01ME", RelationRelated)
	if !errors.Is(err, ErrInvalidLink) {
		t.Errorf("self-link = %v, want ErrInvalidLink", err)
	}
	if len(repo.added) != 0 {
		t.Errorf("self-link reached storage: %+v", repo.added)
	}
}

func TestCreateLinkRejectsUnknownRelation(t *testing.T) {
	repo := &fakeRepo{}
	err := newLinkService(repo).CreateLink(t.Context(), "01A", "01B", "friend")
	if !errors.Is(err, ErrInvalidLink) {
		t.Errorf("unknown relation = %v, want ErrInvalidLink", err)
	}
	if len(repo.added) != 0 {
		t.Errorf("invalid link reached storage: %+v", repo.added)
	}
}

func TestCreateLinkDefaultsRelationToRelated(t *testing.T) {
	// Mirrors the link op's default: an unset relation means "related".
	repo := &fakeRepo{}
	if err := newLinkService(repo).CreateLink(t.Context(), "01A", "01B", ""); err != nil {
		t.Fatalf("CreateLink: %v", err)
	}
	want := []Edge{{From: "01A", To: "01B", Relation: RelationRelated}}
	if !reflect.DeepEqual(repo.added, want) {
		t.Errorf("added = %+v, want %+v", repo.added, want)
	}
}

func TestRemoveLinkPassesThrough(t *testing.T) {
	repo := &fakeRepo{}
	if err := newLinkService(repo).RemoveLink(t.Context(), "01A", "01B", RelationExample); err != nil {
		t.Fatalf("RemoveLink: %v", err)
	}
	want := []Edge{{From: "01A", To: "01B", Relation: RelationExample}}
	if !reflect.DeepEqual(repo.removed, want) {
		t.Errorf("removed = %+v, want %+v", repo.removed, want)
	}
}
