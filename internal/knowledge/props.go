package knowledge

import (
	"context"
	"fmt"
	"strings"
)

// TopicProps are what a topic carries beside its text: the other names it
// answers to and the labels it is found by. They are the owner's to set and
// sit outside revisions, so no batch — the AI's included — changes them.
type TopicProps struct {
	// Aliases are extra names a [[Topic]] reference may use, as written.
	Aliases []string
	// Tags are labels for finding and grouping topics, as written. A tag is
	// not a topic and resolves no reference.
	Tags []string
}

// TopicAlias is one alias with the topic it resolves to.
type TopicAlias struct {
	TopicID ID
	Alias   string
}

// TopicTag is one tag with the topic that carries it.
type TopicTag struct {
	TopicID ID
	Tag     string
}

// TagKey is the key two spellings of one tag share: without the leading #
// people write tags with, inner whitespace collapsed, casefolded. Empty means
// the text names no tag.
func TagKey(tag string) string {
	return strings.ToLower(TagName(tag))
}

// TagName is a tag as it is stored and shown: the author's casing, without the
// leading # and with inner whitespace collapsed.
func TagName(tag string) string {
	return strings.Join(strings.Fields(strings.TrimLeft(strings.TrimSpace(tag), "#")), " ")
}

// TopicProps returns the properties of the topic rooted at id.
func (s *Service) TopicProps(ctx context.Context, id ID) (TopicProps, error) {
	return s.repo.GetTopicProps(ctx, id)
}

// TopicAliases returns every alias in the base, which is what a reader needs
// to resolve a [[Topic]] reference the way storage does.
func (s *Service) TopicAliases(ctx context.Context) ([]TopicAlias, error) {
	return s.repo.ListTopicAliases(ctx)
}

// TopicTags returns every tag in the base with the topic that carries it,
// which is what filtering topics by tag reads.
func (s *Service) TopicTags(ctx context.Context) ([]TopicTag, error) {
	return s.repo.ListTopicTags(ctx)
}

// SetTopicProps replaces the properties of the topic rooted at id, both taken
// as written. A name written twice is kept once.
//
// An alias no reference can carry is refused with ErrUnreferenceableTopic, and
// one that is already a topic's name or alias with ErrDuplicateTopic.
func (s *Service) SetTopicProps(ctx context.Context, id ID, aliases, tags []string) (TopicProps, error) {
	props := TopicProps{}
	seen := map[string]bool{}
	for _, alias := range aliases {
		alias = strings.TrimSpace(alias)
		if alias == "" {
			continue
		}
		key := TopicKey(alias)
		if key == "" {
			return TopicProps{}, fmt.Errorf("alias %q: %w", alias, ErrUnreferenceableTopic)
		}
		if !seen[key] {
			seen[key] = true
			props.Aliases = append(props.Aliases, alias)
		}
	}
	tagged := map[string]bool{}
	for _, tag := range tags {
		key := TagKey(tag)
		if key == "" || tagged[key] {
			continue
		}
		tagged[key] = true
		props.Tags = append(props.Tags, TagName(tag))
	}

	if err := s.repo.SetTopicProps(ctx, id, props); err != nil {
		return TopicProps{}, err
	}
	return s.repo.GetTopicProps(ctx, id)
}
