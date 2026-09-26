package vaultimport

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"strconv"
	"sync"

	"github.com/zamomd/zamo/internal/host"
	"github.com/zamomd/zamo/internal/knowledge"
)

// SourceImport marks the revisions an import writes.
const SourceImport knowledge.Source = "import"

// The module's own failures, each with the wire code the frontend shows a
// sentence for.
var (
	ErrUnknownFormat error = coded{
		"import_unknown_format", "the directory is neither an Obsidian vault nor a Logseq graph",
	}
	ErrStalePreview    error = coded{"import_stale_preview", "the preview is no longer current; run it again"}
	ErrNothingToImport error = coded{"import_nothing", "nothing to import: every page is in conflict"}
)

// coded is a sentinel with a wire code (see host.Coded).
type coded struct{ code, msg string }

func (e coded) Error() string { return e.msg }
func (e coded) Code() string  { return e.code }

// Result reports what an applied import wrote.
type Result struct {
	GroupID knowledge.ID `json:"groupId"`
	Pages   int          `json:"pages"`
	Blocks  int          `json:"blocks"`
}

// Module imports a vault in two steps: a preview the user reads, then the
// write of exactly that preview. Its exported methods are bound into the
// desktop frontend as the import service.
type Module struct {
	base     host.BaseReader
	writer   host.BatchWriter
	picker   host.FilePicker
	adapters []Adapter

	mu      sync.Mutex
	pending *Plan // the one preview that Apply may write, or nil
}

// New assembles the module from the capabilities it needs and the formats
// it understands. The file picker arrives through SetPicker once the desktop
// application exists.
func New(base host.BaseReader, writer host.BatchWriter, adapters ...Adapter) *Module {
	return &Module{base: base, writer: writer, adapters: adapters}
}

// SetPicker wires the platform file dialogs, which only exist once the
// desktop application does. A package function rather than a method so it
// is not itself exposed as a frontend binding.
func SetPicker(m *Module, p host.FilePicker) { m.picker = p }

// Preview asks for a vault directory, parses it and reports what an import
// would do without writing anything. An empty Token means the user
// cancelled. The plan replaces any earlier preview and is what Apply writes.
func (m *Module) Preview(ctx context.Context) (Plan, error) {
	dir, err := m.picker.OpenDirectory("Import an Obsidian vault or a Logseq graph")
	if err != nil || dir == "" {
		return Plan{}, err
	}
	adapter, ok := m.detect(dir)
	if !ok {
		return Plan{}, fmt.Errorf("%s: %w", dir, ErrUnknownFormat)
	}
	vault, err := adapter.Parse(ctx, dir)
	if err != nil {
		return Plan{}, fmt.Errorf("read %s: %w", dir, err)
	}
	existing, err := m.topicKeys(ctx)
	if err != nil {
		return Plan{}, err
	}
	p := plan(vault, existing)
	p.Token = newToken()
	p.Dir = dir
	m.mu.Lock()
	m.pending = &p
	m.mu.Unlock()
	return p, nil
}

// Apply writes the previewed plan named by token as one owner-authored,
// born-reviewed batch. A token other than the current preview's writes
// nothing. A batch the base refuses — a topic created since the preview
// took a name the plan claims — leaves the preview in place so the user can
// run it again.
func (m *Module) Apply(ctx context.Context, token string) (Result, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	p := m.pending
	if p == nil || token == "" || p.Token != token {
		return Result{}, ErrStalePreview
	}
	if len(p.ops) == 0 {
		return Result{}, ErrNothingToImport
	}
	meta := knowledge.WriteMeta{
		Author: knowledge.AuthorOwner,
		Source: SourceImport,
		Title:  "Import " + p.Format + ": " + strconv.Itoa(p.Pages) + " pages",
	}
	applied, err := m.writer.ApplyOps(ctx, meta, p.ops)
	if err != nil {
		if errors.Is(err, knowledge.ErrDuplicateTopic) {
			return Result{}, fmt.Errorf("the base changed since the preview; run it again: %w", err)
		}
		return Result{}, err
	}
	m.pending = nil
	return Result{GroupID: applied.GroupID, Pages: p.Pages, Blocks: p.Blocks}, nil
}

// Discard drops the pending preview, so nothing of it can be written.
func (m *Module) Discard(_ context.Context) error {
	m.mu.Lock()
	m.pending = nil
	m.mu.Unlock()
	return nil
}

func (m *Module) detect(dir string) (Adapter, bool) {
	for _, a := range m.adapters {
		if a.Detect(dir) {
			return a, true
		}
	}
	return nil, false
}

// topicKeys reads the names already taken in the base.
func (m *Module) topicKeys(ctx context.Context) (map[string]bool, error) {
	topics, err := m.base.TopicMap(ctx)
	if err != nil {
		return nil, err
	}
	keys := make(map[string]bool, len(topics))
	for _, t := range topics {
		if t.IsTopic() {
			keys[knowledge.TopicKey(t.Content)] = true
		}
	}
	return keys, nil
}

func newToken() string {
	var b [16]byte
	rand.Read(b[:])
	return hex.EncodeToString(b[:])
}
