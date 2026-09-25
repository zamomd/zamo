package knowledge

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestBacklinkExcerptFindsABlockReferenceWhateverItsLabel(t *testing.T) {
	const source = "An opening paragraph.\n\nSee ((01ARZ3NDEKTSV4RRFFQ69G5FAV|some other wording))."
	got := BacklinkExcerpt(source, "01ARZ3NDEKTSV4RRFFQ69G5FAV", "")
	assert.Equal(t, "See ((01ARZ3NDEKTSV4RRFFQ69G5FAV|some other wording)).", got)
}

func TestBacklinkExcerptFindsABareBlockReference(t *testing.T) {
	const source = "An opening paragraph.\n\nSee ((01ARZ3NDEKTSV4RRFFQ69G5FAV))."
	got := BacklinkExcerpt(source, "01ARZ3NDEKTSV4RRFFQ69G5FAV", "")
	assert.Equal(t, "See ((01ARZ3NDEKTSV4RRFFQ69G5FAV)).", got)
}

func TestBacklinkExcerptMatchesALowercasedBlockReference(t *testing.T) {
	const source = "Intro.\n\nSee ((01arz3ndektsv4rrffq69g5fav))."
	got := BacklinkExcerpt(source, "01ARZ3NDEKTSV4RRFFQ69G5FAV", "")
	assert.Equal(t, "See ((01arz3ndektsv4rrffq69g5fav)).", got)
}

func TestBacklinkExcerptFindsATopicReferenceThroughNormalization(t *testing.T) {
	const source = "An opening paragraph.\n\nCancellation is covered in [[go   contexts]]."
	got := BacklinkExcerpt(source, "01ARZ3NDEKTSV4RRFFQ69G5FAV", NormalizeTitle("Go Contexts"))
	assert.Equal(t, "Cancellation is covered in [[go   contexts]].", got)
}

func TestBacklinkExcerptIgnoresAReferenceToSomethingElse(t *testing.T) {
	const source = "Intro.\n\nSee [[Another Topic]].\n\nAnd [[Go Contexts]] here."
	got := BacklinkExcerpt(source, "01ARZ3NDEKTSV4RRFFQ69G5FAV", NormalizeTitle("Go Contexts"))
	assert.Equal(t, "And [[Go Contexts]] here.", got)
}

func TestBacklinkExcerptFallsBackToTheOpeningParagraph(t *testing.T) {
	const source = "An opening paragraph.\n\nA second one."
	got := BacklinkExcerpt(source, "01ARZ3NDEKTSV4RRFFQ69G5FAV", "")
	assert.Equal(t, "An opening paragraph.", got)
}

func TestBacklinkExcerptOfEmptyContentIsEmpty(t *testing.T) {
	assert.Empty(t, BacklinkExcerpt("   \n\n  ", "01ARZ3NDEKTSV4RRFFQ69G5FAV", ""))
}

func TestBacklinkExcerptKeepsAMultiLineParagraphWhole(t *testing.T) {
	const source = "Intro.\n\nA sentence.\nAnd [[Go Contexts]] on the next line."
	got := BacklinkExcerpt(source, "01ARZ3NDEKTSV4RRFFQ69G5FAV", NormalizeTitle("Go Contexts"))
	assert.Equal(t, "A sentence.\nAnd [[Go Contexts]] on the next line.", got)
}
