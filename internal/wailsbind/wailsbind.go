// Package wailsbind is an entry point: methods bound into the Wails
// frontend. A thin layer: take a UI call → call knowledge.Service →
// return the result.
package wailsbind

import (
	"context"

	"github.com/zamomd/zamo/internal/jobs"
	"github.com/zamomd/zamo/internal/knowledge"
	"github.com/zamomd/zamo/internal/settings"
)

// Emitter delivers a named event with a payload to the frontend. The Wails
// application's event manager (app.Event) satisfies it; tests pass a fake. The
// bool return is Wails' delivered-flag, which the bridge ignores.
type Emitter interface {
	Emit(name string, data ...any) bool
}

// App is the object bound into the Wails runtime; the frontend calls
// its methods.
type App struct {
	svc      *knowledge.Service
	settings *settings.Manager
	jobs     *jobs.Registry
	emit     Emitter // set via SetEmitter once the Wails app exists; nil when headless (see SetEmitter)

	schedules ScheduleLister // set via SetScheduler; nil when headless
	agent     AgentRunner    // set via SetAgent
}

// New wraps the service, the settings manager and the job registry for
// binding. Agent runs are jobs, so the frontend can cancel them.
func New(svc *knowledge.Service, mgr *settings.Manager, reg *jobs.Registry) *App {
	return &App{svc: svc, settings: mgr, jobs: reg}
}

// SetEmitter wires the frontend event emitter. It is called once at startup,
// after the Wails application (whose event manager is the emitter) is built but
// before it runs, so no agent run can be in flight yet.
//
// A package function rather than a method on App: App is registered as a Wails
// service, so every exported method it has becomes a callable frontend binding.
// As a method this was reachable from JavaScript, where passing null would
// silently detach the app's events. Startup wiring is not the frontend's to do.
func SetEmitter(a *App, e Emitter) { a.emit = e }

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

// CreateTopic passes the call to knowledge.Service: a new, empty topic named by
// the user, written without the AI and reporting the new root's id.
func (a *App) CreateTopic(ctx context.Context, name string, mode knowledge.Mode) (knowledge.ID, error) {
	return a.svc.CreateTopic(ctx, name, mode)
}

// SetTopicMode passes the call to knowledge.Service: converts a topic between
// an outline and a document.
func (a *App) SetTopicMode(ctx context.Context, id knowledge.ID, mode knowledge.Mode) error {
	_, err := a.svc.SetTopicMode(ctx, id, mode)
	return err
}

// GetSubtree passes the call to knowledge.Service.
func (a *App) GetSubtree(ctx context.Context, root knowledge.ID) ([]knowledge.Block, error) {
	return a.svc.GetSubtree(ctx, root)
}

// Backlinks lists every reference pointing at the block or topic.
func (a *App) Backlinks(ctx context.Context, id knowledge.ID) ([]knowledge.Backlink, error) {
	return a.svc.Backlinks(ctx, id)
}

// TopicProps returns what a topic carries beside its text: its aliases and its
// tags.
func (a *App) TopicProps(ctx context.Context, id knowledge.ID) (knowledge.TopicProps, error) {
	return a.svc.TopicProps(ctx, id)
}

// SetTopicProps replaces a topic's aliases and tags, both as written.
func (a *App) SetTopicProps(
	ctx context.Context, id knowledge.ID, aliases, tags []string,
) (knowledge.TopicProps, error) {
	return a.svc.SetTopicProps(ctx, id, aliases, tags)
}

// TopicAliases returns every alias in the base with the topic it resolves to.
func (a *App) TopicAliases(ctx context.Context) ([]knowledge.TopicAlias, error) {
	return a.svc.TopicAliases(ctx)
}

// TopicTags returns every tag in the base with the topic that carries it.
func (a *App) TopicTags(ctx context.Context) ([]knowledge.TopicTag, error) {
	return a.svc.TopicTags(ctx)
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

// InsertFirstChild is Enter on the block the view is rooted at: an empty block
// opens its children. It is how an empty topic gets its first block.
func (a *App) InsertFirstChild(ctx context.Context, id knowledge.ID, content string) (knowledge.OutlineChange, error) {
	return a.svc.InsertFirstChild(ctx, id, content)
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
func (a *App) Search(ctx context.Context, query string) ([]knowledge.SearchHit, error) {
	return a.svc.Search(ctx, query)
}

// Unreviewed passes the call to knowledge.Service: the AI-authored blocks
// still awaiting a human decision.
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
