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
	emit     Emitter // set via SetEmitter once the Wails app exists; nil when headless (see SetEmitter)
}

// New wraps the service and the settings manager for binding.
func New(svc *knowledge.Service, mgr *settings.Manager) *App {
	return &App{svc: svc, settings: mgr}
}

// SetEmitter wires the frontend event emitter. It is called once at startup,
// after the Wails application (whose event manager is the emitter) is built but
// before it runs, so no generation can be in flight yet.
//
// A package function rather than a method on App: App is registered as a Wails
// service, so every exported method it has becomes a callable frontend binding.
// As a method this was reachable from JavaScript, where passing null would
// silently detach progress events. Startup wiring is not the frontend's to do.
func SetEmitter(a *App, e Emitter) { a.emit = e }

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

// TopicGraph passes the call to knowledge.Service.
func (a *App) TopicGraph(ctx context.Context) (knowledge.TopicGraph, error) {
	return a.svc.TopicGraph(ctx)
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

// Backlinks lists every reference pointing at the block or topic.
func (a *App) Backlinks(ctx context.Context, id knowledge.ID) ([]knowledge.Backlink, error) {
	return a.svc.Backlinks(ctx, id)
}

// EditBlockContent passes the call to knowledge.Service. A manual edit by the
// owner applies directly, without the review queue.
func (a *App) EditBlockContent(ctx context.Context, id knowledge.ID, content string) error {
	return a.svc.EditBlockContent(ctx, id, content)
}

// SplitBlock is Enter with a caret inside a block: the block keeps head, a new
// sibling below takes tail.
func (a *App) SplitBlock(ctx context.Context, id knowledge.ID, head, tail string) (knowledge.OutlineChange, error) {
	return a.svc.SplitBlock(ctx, id, head, tail)
}

// InsertBlockBefore is Enter at the very start of a block: an empty sibling
// appears above and the block keeps its id and history.
func (a *App) InsertBlockBefore(ctx context.Context, id knowledge.ID, content string) (knowledge.OutlineChange, error) {
	return a.svc.InsertBefore(ctx, id, content)
}

// IndentBlock is Tab: the block becomes the last child of the sibling above.
func (a *App) IndentBlock(ctx context.Context, id knowledge.ID) (knowledge.OutlineChange, error) {
	return a.svc.IndentBlock(ctx, id)
}

// OutdentBlock is Shift+Tab: the block becomes its parent's next sibling.
func (a *App) OutdentBlock(ctx context.Context, id knowledge.ID) (knowledge.OutlineChange, error) {
	return a.svc.OutdentBlock(ctx, id)
}

// MergeBlocks is Backspace at the start of a block: target absorbs source's
// text and children, source is removed.
func (a *App) MergeBlocks(ctx context.Context, target, source knowledge.ID) (knowledge.OutlineChange, error) {
	return a.svc.MergeBlocks(ctx, target, source)
}

// DeleteBlock removes a block and everything under it. Meant for a childless
// empty block the owner deletes.
func (a *App) DeleteBlock(ctx context.Context, id knowledge.ID) (knowledge.OutlineChange, error) {
	return a.svc.DeleteBlock(ctx, id)
}

// MoveBlock reparents a block after the named sibling, or last when after is
// empty.
func (a *App) MoveBlock(ctx context.Context, id, parent, after knowledge.ID) (knowledge.OutlineChange, error) {
	return a.svc.MoveBlock(ctx, id, parent, after)
}

// UndoGroup rolls back an outline operation by its revision group.
func (a *App) UndoGroup(ctx context.Context, groupID knowledge.ID) error {
	return a.svc.UndoGroup(ctx, groupID)
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
