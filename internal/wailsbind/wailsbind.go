// Package wailsbind is an entry point: methods bound into the Wails
// frontend. A thin layer: take a UI call → call knowledge.Service →
// return the result.
package wailsbind

import (
	"context"

	"github.com/cominddev/comind/internal/knowledge"
	"github.com/cominddev/comind/internal/settings"
)

// GenerationProgressEvent is the name of the Wails event carrying live LLM
// generation progress to the frontend. Its payload is a progressPayload.
const GenerationProgressEvent = "generation:progress"

// Emitter delivers a named event with a payload to the frontend. The Wails
// application's event manager (app.Event) satisfies it; tests pass a fake. The
// bool return is Wails' delivered-flag, which the bridge ignores.
type Emitter interface {
	Emit(name string, data ...any) bool
}

// progressPayload is the JSON shape the frontend reads from a
// GenerationProgressEvent: the coarse phase of the in-flight generation.
type progressPayload struct {
	Phase string `json:"phase"`
}

// App is the object bound into the Wails runtime; the frontend calls
// its methods.
type App struct {
	svc      *knowledge.Service
	settings *settings.Manager
	emit     Emitter // set via SetEmitter once the Wails app exists; nil when headless
}

// New wraps the service and the settings manager for binding.
func New(svc *knowledge.Service, mgr *settings.Manager) *App {
	return &App{svc: svc, settings: mgr}
}

// SetEmitter wires the frontend event emitter. It is called once at startup,
// after the Wails application (whose event manager is the emitter) is built but
// before it runs, so no generation can be in flight yet.
func (a *App) SetEmitter(e Emitter) { a.emit = e }

// withProgress returns a context that forwards LLM generation progress to the
// frontend as GenerationProgressEvent events. With no emitter set (headless,
// tests) it returns ctx unchanged, so Report stays a no-op.
func (a *App) withProgress(ctx context.Context) context.Context {
	if a.emit == nil {
		return ctx
	}
	return knowledge.WithProgress(ctx, func(p knowledge.Progress) {
		a.emit.Emit(GenerationProgressEvent, progressPayload{Phase: string(p.Phase)})
	})
}

// The methods below are direct pass-throughs; the Wails frontend binds
// to them. The set grows with the screens (tree, review, search, export).

// TopicMap passes the call to knowledge.Service.
func (a *App) TopicMap(ctx context.Context) ([]knowledge.Block, error) {
	return a.svc.TopicMap(ctx)
}

// ExpandTopic passes the call to knowledge.Service.
func (a *App) ExpandTopic(ctx context.Context, topic, hint string) (knowledge.Applied, error) {
	return a.svc.ExpandTopic(a.withProgress(ctx), topic, hint)
}

// EditWithAI passes the call to knowledge.Service. The frontend supplies the
// scope (a block or subtree root) and the user's text command.
func (a *App) EditWithAI(ctx context.Context, scope knowledge.ID, instruction string) (knowledge.Applied, error) {
	return a.svc.EditWithAI(a.withProgress(ctx), scope, instruction)
}

// GetSubtree passes the call to knowledge.Service.
func (a *App) GetSubtree(ctx context.Context, root knowledge.ID) ([]knowledge.Block, error) {
	return a.svc.GetSubtree(ctx, root)
}

// GetLinks passes the call to knowledge.Service: the block's typed links,
// hydrated with their neighbour blocks.
func (a *App) GetLinks(ctx context.Context, id knowledge.ID) ([]knowledge.LinkedBlock, error) {
	return a.svc.LinkedBlocks(ctx, id)
}

// CreateLink passes the call to knowledge.Service. The owner links directly,
// without the review queue — like EditBlockContent.
func (a *App) CreateLink(ctx context.Context, from, to knowledge.ID, relation knowledge.Relation) error {
	return a.svc.CreateLink(ctx, from, to, relation)
}

// RemoveLink passes the call to knowledge.Service.
func (a *App) RemoveLink(ctx context.Context, from, to knowledge.ID, relation knowledge.Relation) error {
	return a.svc.RemoveLink(ctx, from, to, relation)
}

// EditBlockContent passes the call to knowledge.Service. A manual edit by the
// owner applies directly, without the review queue.
func (a *App) EditBlockContent(ctx context.Context, id knowledge.ID, content string) error {
	return a.svc.EditBlockContent(ctx, id, content)
}

// Search passes the call to knowledge.Service. The frontend supplies the raw
// query string; the backend owns the FTS syntax and its neutralisation.
func (a *App) Search(ctx context.Context, query string) ([]knowledge.Block, error) {
	return a.svc.Search(ctx, query)
}

// ExportMarkdown passes the call to knowledge.Service. The frontend supplies a
// directory chosen through the folder picker.
func (a *App) ExportMarkdown(ctx context.Context, dir string) error {
	return a.svc.ExportMarkdown(ctx, dir)
}

// Unreviewed passes the call to knowledge.Service: the AI-authored blocks and
// edges still awaiting a human decision.
func (a *App) Unreviewed(ctx context.Context) (knowledge.UnreviewedChanges, error) {
	return a.svc.Unreviewed(ctx)
}

// AcceptBlock passes the call to knowledge.Service.
func (a *App) AcceptBlock(ctx context.Context, id knowledge.ID) error {
	return a.svc.AcceptBlock(ctx, id)
}

// RejectBlock passes the call to knowledge.Service.
func (a *App) RejectBlock(ctx context.Context, id knowledge.ID) error {
	return a.svc.RejectBlock(ctx, id)
}

// AcceptPage passes the call to knowledge.Service: accepts every unreviewed
// block under topicID in one step.
func (a *App) AcceptPage(ctx context.Context, topicID knowledge.ID) error {
	return a.svc.AcceptPage(ctx, topicID)
}

// RejectPage passes the call to knowledge.Service.
func (a *App) RejectPage(ctx context.Context, topicID knowledge.ID) error {
	return a.svc.RejectPage(ctx, topicID)
}

// AcceptGroup passes the call to knowledge.Service: accepts every block from
// one AI batch, identified by its group id.
func (a *App) AcceptGroup(ctx context.Context, groupID knowledge.ID) error {
	return a.svc.AcceptGroup(ctx, groupID)
}

// BlockHistory passes the call to knowledge.Service: the block's revisions,
// newest first.
func (a *App) BlockHistory(ctx context.Context, id knowledge.ID) ([]knowledge.Revision, error) {
	return a.svc.BlockHistory(ctx, id)
}

// DeleteRevision passes the call to knowledge.Service.
func (a *App) DeleteRevision(ctx context.Context, blockID, revID knowledge.ID) error {
	return a.svc.DeleteRevision(ctx, blockID, revID)
}

// AcceptEdge passes the call to knowledge.Service.
func (a *App) AcceptEdge(ctx context.Context, from, to knowledge.ID, rel knowledge.Relation) error {
	return a.svc.AcceptEdge(ctx, from, to, rel)
}

// RejectEdge passes the call to knowledge.Service.
func (a *App) RejectEdge(ctx context.Context, from, to knowledge.ID, rel knowledge.Relation) error {
	return a.svc.RejectEdge(ctx, from, to, rel)
}

// Settings passes the call to settings.Manager: the effective provider
// configuration and the origin of each field. The API key is never part of the
// result — only whether one is set.
func (a *App) Settings(ctx context.Context) (settings.View, error) {
	return a.settings.Current(ctx)
}

// SaveSettings passes the call to settings.Manager. It takes effect
// immediately: the live client is swapped, no restart.
func (a *App) SaveSettings(ctx context.Context, in settings.In) error {
	return a.settings.Save(ctx, in)
}

// CheckSettings passes the call to settings.Manager: a reachability check for
// values the user has not saved yet.
func (a *App) CheckSettings(ctx context.Context, in settings.In) error {
	return a.settings.Check(ctx, in)
}
