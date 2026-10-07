// Package export is the feature module that writes the base to disk as
// portable markdown: one file per topic, in the shape the topic has. Beside
// the one-off export into any directory it keeps one destination: a
// directory it exports into again and again, on demand or on a schedule,
// and whose history it can keep in git.
package export

import (
	"context"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"fmt"
	"strings"
	"sync"
	"text/template"
	"time"

	"github.com/zamomd/zamo/internal/host"
	"github.com/zamomd/zamo/internal/knowledge"
)

// Name is the module's name with the scheduler.
const Name = "export"

// DefaultSpec is how often a destination is exported until the owner says
// otherwise.
const DefaultSpec = "@daily"

// DefaultMessage is the commit message template until the owner writes one.
const DefaultMessage = `Zamo export ({{.Time.Format "2006-01-02 15:04"}})`

// destinationKey is the one schedule record the module keeps. The record is
// the destination: its payload says where and how, its spec and switch say
// when.
const destinationKey = "markdown"

const payloadVersion = 1

type payload struct {
	V       int    `json:"v"`
	Dir     string `json:"dir"`
	Commit  bool   `json:"commit,omitzero"`
	Message string `json:"message,omitzero"`
}

// coded is a sentinel with a wire code (see host.Coded).
type coded struct{ code, msg string }

func (e coded) Error() string { return e.msg }
func (e coded) Code() string  { return e.code }

// The module's own failures, each with the wire code the frontend shows a
// sentence for.
var (
	ErrNoDestination error = coded{"export_no_destination", "no export destination is chosen"}
	ErrBusy          error = coded{"export_busy", "an export is already in progress"}
	ErrNoGit         error = coded{"export_no_git", "git is not installed, so the export was not committed"}
)

// Destination is the kept export destination as the settings screen shows
// it. Dir is empty when none is chosen; the other fields are then defaults.
type Destination struct {
	Dir       string    `json:"dir"`
	Commit    bool      `json:"commit"`
	Message   string    `json:"message"`
	Repo      string    `json:"repo"`
	Spec      string    `json:"spec"`
	Scheduled bool      `json:"scheduled"`
	Running   bool      `json:"running"`
	NextRun   time.Time `json:"nextRun,omitzero"`
	LastRun   time.Time `json:"lastRun,omitzero"`
	Outcome   string    `json:"outcome,omitempty"`
	Detail    string    `json:"detail,omitempty"`
}

// Options is what the owner decides about the destination besides where it
// is. An empty Message means the default one.
type Options struct {
	Commit    bool   `json:"commit"`
	Message   string `json:"message"`
	Spec      string `json:"spec"`
	Scheduled bool   `json:"scheduled"`
}

// Result is how one export into the destination went. Commit is the hash of
// the commit it made, empty when it made none.
type Result struct {
	Dir    string `json:"dir"`
	Commit string `json:"commit"`
}

// Module exports the base. Its exported methods are bound into the desktop
// frontend as the export service.
type Module struct {
	base      host.BaseReader
	files     host.FileWriter
	history   host.DirHistory
	sched     host.Scheduler
	now       func() time.Time
	picker    host.FilePicker
	exporting sync.Mutex
}

// New assembles the module from the capabilities it needs and registers its
// scheduled export. The file picker arrives through SetPicker once the
// desktop application exists.
func New(
	base host.BaseReader, files host.FileWriter, history host.DirHistory, sched host.Scheduler, now func() time.Time,
) *Module {
	m := &Module{base: base, files: files, history: history, sched: sched, now: now}
	sched.Handle(m.run, host.ScheduleOptions{MaxConcurrent: 1})
	return m
}

// SetPicker wires the platform file dialogs, which only exist once the
// desktop application does. A package function rather than a method so it
// is not itself exposed as a frontend binding.
func SetPicker(m *Module, p host.FilePicker) { m.picker = p }

// Export asks for a directory and writes the base into it as markdown,
// answering with the directory it wrote. An empty answer means the user
// cancelled. Files named after topics already in the directory are
// overwritten, and those an earlier export left for topics since gone are
// removed; nothing else in it is touched.
func (m *Module) Export(ctx context.Context) (string, error) {
	dir, err := m.picker.OpenDirectory("Export the Zamo base as Markdown")
	if err != nil || dir == "" {
		return "", err
	}
	if !m.exporting.TryLock() {
		return "", ErrBusy
	}
	defer m.exporting.Unlock()
	if err := m.Markdown(ctx, dir); err != nil {
		return "", err
	}
	return dir, nil
}

// Markdown exports the base into dir: one markdown file per topic, block
// nesting rendered as nested lists, and the marker file that lets an import
// recognise the directory. All reads complete before any write, so a read
// failure aborts with nothing written; how a mid-write failure is handled is
// the writer's concern.
func (m *Module) Markdown(ctx context.Context, dir string) error {
	topics, err := m.base.TopicMap(ctx)
	if err != nil {
		return err
	}

	var subtrees [][]knowledge.Block
	for _, t := range topics {
		if !t.IsTopic() { // TopicMap also returns first-level children; skip them
			continue
		}
		sub, err := m.base.GetSubtree(ctx, t.ID)
		if err != nil {
			return err
		}
		subtrees = append(subtrees, sub)
	}

	files := append(knowledge.RenderMarkdown(subtrees), knowledge.MarkdownMarker())
	return m.files.WriteFiles(ctx, dir, files)
}

// Destination returns the kept destination, or one with an empty Dir.
func (m *Module) Destination(ctx context.Context) (Destination, error) {
	s, p, err := m.load(ctx)
	if err != nil {
		return Destination{}, err
	}
	return m.view(ctx, s, p), nil
}

// ChooseDestination asks for a directory and keeps it as the destination,
// leaving every other choice as it was. A cancelled dialog changes nothing.
func (m *Module) ChooseDestination(ctx context.Context) (Destination, error) {
	s, p, err := m.load(ctx)
	if err != nil {
		return Destination{}, err
	}
	dir, err := m.picker.OpenDirectory("Choose where Zamo keeps its Markdown export")
	if err != nil {
		return Destination{}, err
	}
	if dir == "" {
		return m.view(ctx, s, p), nil
	}
	p.Dir = dir
	return m.save(ctx, s.Spec, s.Enabled, p)
}

// Configure sets how and when the destination is exported.
func (m *Module) Configure(ctx context.Context, opts Options) (Destination, error) {
	_, p, err := m.load(ctx)
	if err != nil {
		return Destination{}, err
	}
	if p.Dir == "" {
		return Destination{}, ErrNoDestination
	}
	message := strings.TrimSpace(opts.Message)
	if message == DefaultMessage {
		message = ""
	}
	if _, err := commitMessage(message, m.now()); err != nil {
		return Destination{}, coded{"export_bad_message", err.Error()}
	}
	p.Commit, p.Message = opts.Commit, message
	return m.save(ctx, opts.Spec, opts.Scheduled, p)
}

// ForgetDestination drops the destination and its schedule. The directory
// and everything in it stay.
func (m *Module) ForgetDestination(ctx context.Context) error {
	return m.sched.Delete(ctx, destinationKey)
}

// ExportNow exports into the destination at once.
func (m *Module) ExportNow(ctx context.Context) (Result, error) {
	_, p, err := m.load(ctx)
	if err != nil {
		return Result{}, err
	}
	if p.Dir == "" {
		return Result{}, ErrNoDestination
	}
	if !m.exporting.TryLock() {
		return Result{}, ErrBusy
	}
	defer m.exporting.Unlock()
	c, err := m.exportTo(ctx, p)
	if err != nil {
		return Result{}, err
	}
	return Result{Dir: p.Dir, Commit: c.Hash}, nil
}

// run is the scheduled export.
func (m *Module) run(ctx context.Context, _ string, raw jsontext.Value) host.RunResult {
	p, err := decode(raw)
	if err != nil {
		return host.RunResult{Outcome: host.OutcomeFailed, Detail: err.Error()}
	}
	m.exporting.Lock()
	defer m.exporting.Unlock()
	c, err := m.exportTo(ctx, p)
	switch {
	case err != nil:
		return host.RunResult{Outcome: host.OutcomeFailed, Detail: err.Error()}
	case !p.Commit:
		return host.RunResult{Outcome: host.OutcomeOK}
	case c.Hash == "":
		return host.RunResult{Outcome: host.OutcomeNoChange}
	default:
		return host.RunResult{Outcome: host.OutcomeOK, Detail: "committed " + shortHash(c.Hash)}
	}
}

// exportTo writes the base into the destination and, where asked, commits
// it. The zero Commit means none was made.
func (m *Module) exportTo(ctx context.Context, p payload) (host.Commit, error) {
	if err := m.Markdown(ctx, p.Dir); err != nil {
		return host.Commit{}, err
	}
	if !p.Commit {
		return host.Commit{}, nil
	}
	message, err := commitMessage(p.Message, m.now())
	if err != nil {
		return host.Commit{}, err
	}
	c, err := m.history.Commit(ctx, p.Dir, message)
	if errors.Is(err, host.ErrNoGit) {
		return host.Commit{}, ErrNoGit
	}
	return c, err
}

// load reads the destination's record. No record is a zero schedule and a
// zero payload, not an error.
func (m *Module) load(ctx context.Context) (host.Schedule, payload, error) {
	s, err := m.sched.Get(ctx, destinationKey)
	if errors.Is(err, host.ErrScheduleNotFound) {
		return host.Schedule{Spec: DefaultSpec}, payload{}, nil
	}
	if err != nil {
		return host.Schedule{}, payload{}, err
	}
	p, err := decode(s.Payload)
	if err != nil {
		return host.Schedule{}, payload{}, err
	}
	return s, p, nil
}

func (m *Module) save(ctx context.Context, spec string, enabled bool, p payload) (Destination, error) {
	p.V = payloadVersion
	raw, err := json.Marshal(p)
	if err != nil {
		return Destination{}, err
	}
	s, err := m.sched.Put(ctx, destinationKey, spec, enabled, raw)
	if errors.Is(err, host.ErrInvalidSpec) {
		detail := strings.TrimPrefix(err.Error(), host.ErrInvalidSpec.Error()+": ")
		return Destination{}, coded{"schedule_invalid_spec", detail}
	}
	if err != nil {
		return Destination{}, err
	}
	return m.view(ctx, s, p), nil
}

func decode(raw jsontext.Value) (payload, error) {
	var p payload
	if err := json.Unmarshal(raw, &p); err != nil {
		return payload{}, fmt.Errorf("unreadable export destination: %w", err)
	}
	if p.V > payloadVersion {
		return payload{}, fmt.Errorf("export destination version %d is newer than this build understands", p.V)
	}
	return p, nil
}

func (m *Module) view(ctx context.Context, s host.Schedule, p payload) Destination {
	message := p.Message
	if message == "" {
		message = DefaultMessage
	}
	var repo string
	if p.Commit && p.Dir != "" {
		// Unknown is shown as unknown: a destination that cannot be asked
		// about now still has to open in the settings screen.
		repo, _ = m.history.Root(ctx, p.Dir)
	}
	return Destination{
		Dir:       p.Dir,
		Commit:    p.Commit,
		Message:   message,
		Repo:      repo,
		Spec:      s.Spec,
		Scheduled: s.Enabled,
		Running:   s.Running,
		NextRun:   s.NextRun,
		LastRun:   s.LastRun,
		Outcome:   string(s.Outcome),
		Detail:    s.Detail,
	}
}

// commitMessage fills the template with the time of the export. A field the
// template names and the data lacks is an error, not a blank in the message.
func commitMessage(tmpl string, at time.Time) (string, error) {
	if tmpl == "" {
		tmpl = DefaultMessage
	}
	t, err := template.New("message").Option("missingkey=error").Parse(tmpl)
	if err != nil {
		return "", fmt.Errorf("commit message template: %w", err)
	}
	var b strings.Builder
	if err := t.Execute(&b, struct{ Time time.Time }{at}); err != nil {
		return "", fmt.Errorf("commit message template: %w", err)
	}
	if strings.TrimSpace(b.String()) == "" {
		return "", errors.New("commit message template: the message comes out empty")
	}
	return b.String(), nil
}

func shortHash(hash string) string {
	return hash[:min(len(hash), 7)]
}
