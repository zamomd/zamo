// Package assistant is the conversation with the AI the owner holds in the
// side panel: questions answered from the base, and changes made to what is
// open on screen. It keeps the conversation for as long as the app runs and
// stores nothing. Every change it makes is an AI-authored batch awaiting
// review.
package assistant

import (
	"cmp"
	"context"
	"strings"
	"sync"

	"github.com/zamomd/zamo/internal/host"
	"github.com/zamomd/zamo/internal/knowledge"
)

// Source marks the revisions the assistant writes.
const Source knowledge.Source = "assistant"

// coded is a sentinel with a wire code (see host.Coded).
type coded struct{ code, msg string }

func (e coded) Error() string { return e.msg }
func (e coded) Code() string  { return e.code }

// The module's own failures, each with the wire code the frontend shows a
// sentence for.
var (
	ErrEmpty    error = coded{"assistant_empty", "the message is empty"}
	ErrBusy     error = coded{"assistant_busy", "the assistant is still working on the last message"}
	ErrNoAnswer error = coded{"assistant_no_answer", "there is no such answer to save"}
)

// Context is what the owner has open when a message is sent. The assistant
// may change the selected block, or else the open topic, and everything
// under it; with neither it may only create a topic.
type Context struct {
	ScopeID knowledge.ID `json:"scopeId"`
	TopicID knowledge.ID `json:"topicId"`
}

// Answer is the assistant's reply to one message.
type Answer struct {
	// Turn numbers the answer within the conversation, for SaveAnswer.
	Turn int    `json:"turn"`
	Text string `json:"text"`
	// Exhausted reports that the assistant ran out of rounds before it
	// answered; Text is then empty.
	Exhausted bool `json:"exhausted"`
	// GroupID is the batch the assistant wrote, empty when it wrote none.
	GroupID knowledge.ID `json:"groupId"`
}

// Module is the feature. Its exported methods are bound into the desktop
// frontend as the assistant service.
type Module struct {
	agent  host.Agent
	writer host.BatchWriter

	mu      sync.Mutex
	running bool
	history []host.Message
	answers []string
}

// New builds the module with an empty conversation.
func New(agent host.Agent, writer host.BatchWriter) *Module {
	return &Module{agent: agent, writer: writer}
}

// Send carries one message of the conversation to the assistant and returns
// its answer. One message is handled at a time. A send that fails leaves the
// conversation as it was, so the same message can be sent again.
func (m *Module) Send(ctx context.Context, text string, c Context) (Answer, error) {
	text = strings.TrimSpace(text)
	if text == "" {
		return Answer{}, ErrEmpty
	}
	m.mu.Lock()
	if m.running {
		m.mu.Unlock()
		return Answer{}, ErrBusy
	}
	m.running = true
	history := m.history
	m.mu.Unlock()

	res, err := m.agent.Run(ctx, host.AgentRun{
		Persona:     host.PersonaAssistant,
		Instruction: text,
		History:     history,
		Write:       host.WritePolicy{Scope: cmp.Or(c.ScopeID, c.TopicID), AllowNewTopic: true},
		Source:      Source,
	})

	m.mu.Lock()
	defer m.mu.Unlock()
	m.running = false
	if err != nil {
		return Answer{}, err
	}
	m.history = res.Messages
	m.answers = append(m.answers, res.Text)
	return Answer{
		Turn:      len(m.answers) - 1,
		Text:      res.Text,
		Exhausted: res.Exhausted,
		GroupID:   res.Applied.GroupID,
	}, nil
}

// Reset forgets the conversation. It is refused while a message is being
// handled.
func (m *Module) Reset(_ context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.running {
		return ErrBusy
	}
	m.history, m.answers = nil, nil
	return nil
}

// SaveAnswer writes the answer of a turn as a new block under a topic,
// AI-authored and awaiting review, and returns the block's id.
func (m *Module) SaveAnswer(ctx context.Context, turn int, topicID knowledge.ID) (knowledge.ID, error) {
	m.mu.Lock()
	var text string
	if turn >= 0 && turn < len(m.answers) {
		text = m.answers[turn]
	}
	m.mu.Unlock()
	if text == "" {
		return "", ErrNoAnswer
	}
	const temp = "answer"
	applied, err := m.writer.ApplyOps(ctx,
		knowledge.WriteMeta{Author: knowledge.AuthorAI, Source: Source, Title: "Saved answer"},
		[]knowledge.Op{{
			Kind: knowledge.OpAdd, TempID: temp, Parent: string(topicID), Content: text, Origin: string(Source),
		}})
	if err != nil {
		return "", err
	}
	return applied.IDs[temp], nil
}
