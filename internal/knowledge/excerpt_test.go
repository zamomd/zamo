package knowledge

import "testing"

func TestBacklinkExcerptFindsABlockReferenceWhateverItsLabel(t *testing.T) {
	const source = "An opening paragraph.\n\nSee ((01ARZ3NDEKTSV4RRFFQ69G5FAV|some other wording))."
	got := BacklinkExcerpt(source, "01ARZ3NDEKTSV4RRFFQ69G5FAV", "")
	if want := "See ((01ARZ3NDEKTSV4RRFFQ69G5FAV|some other wording))."; got != want {
		t.Errorf("excerpt = %q, want %q", got, want)
	}
}

func TestBacklinkExcerptFindsABareBlockReference(t *testing.T) {
	const source = "An opening paragraph.\n\nSee ((01ARZ3NDEKTSV4RRFFQ69G5FAV))."
	got := BacklinkExcerpt(source, "01ARZ3NDEKTSV4RRFFQ69G5FAV", "")
	if want := "See ((01ARZ3NDEKTSV4RRFFQ69G5FAV))."; got != want {
		t.Errorf("excerpt = %q, want %q", got, want)
	}
}

func TestBacklinkExcerptMatchesALowercasedBlockReference(t *testing.T) {
	const source = "Intro.\n\nSee ((01arz3ndektsv4rrffq69g5fav))."
	got := BacklinkExcerpt(source, "01ARZ3NDEKTSV4RRFFQ69G5FAV", "")
	if want := "See ((01arz3ndektsv4rrffq69g5fav))."; got != want {
		t.Errorf("excerpt = %q, want %q", got, want)
	}
}

func TestBacklinkExcerptFindsATopicReferenceThroughNormalization(t *testing.T) {
	const source = "An opening paragraph.\n\nCancellation is covered in [[go   contexts]]."
	got := BacklinkExcerpt(source, "01ARZ3NDEKTSV4RRFFQ69G5FAV", NormalizeTitle("Go Contexts"))
	if want := "Cancellation is covered in [[go   contexts]]."; got != want {
		t.Errorf("excerpt = %q, want %q", got, want)
	}
}

func TestBacklinkExcerptIgnoresAReferenceToSomethingElse(t *testing.T) {
	const source = "Intro.\n\nSee [[Another Topic]].\n\nAnd [[Go Contexts]] here."
	got := BacklinkExcerpt(source, "01ARZ3NDEKTSV4RRFFQ69G5FAV", NormalizeTitle("Go Contexts"))
	if want := "And [[Go Contexts]] here."; got != want {
		t.Errorf("excerpt = %q, want %q", got, want)
	}
}

func TestBacklinkExcerptFallsBackToTheOpeningParagraph(t *testing.T) {
	const source = "An opening paragraph.\n\nA second one."
	got := BacklinkExcerpt(source, "01ARZ3NDEKTSV4RRFFQ69G5FAV", "")
	if want := "An opening paragraph."; got != want {
		t.Errorf("excerpt = %q, want %q", got, want)
	}
}

func TestBacklinkExcerptOfEmptyContentIsEmpty(t *testing.T) {
	if got := BacklinkExcerpt("   \n\n  ", "01ARZ3NDEKTSV4RRFFQ69G5FAV", ""); got != "" {
		t.Errorf("excerpt = %q, want empty", got)
	}
}

func TestBacklinkExcerptKeepsAMultiLineParagraphWhole(t *testing.T) {
	const source = "Intro.\n\nA sentence.\nAnd [[Go Contexts]] on the next line."
	got := BacklinkExcerpt(source, "01ARZ3NDEKTSV4RRFFQ69G5FAV", NormalizeTitle("Go Contexts"))
	if want := "A sentence.\nAnd [[Go Contexts]] on the next line."; got != want {
		t.Errorf("excerpt = %q, want %q", got, want)
	}
}
