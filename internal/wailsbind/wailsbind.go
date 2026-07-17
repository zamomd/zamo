// Package wailsbind is an entry point: methods bound into the Wails
// frontend. A thin layer: take a UI call → call knowledge.Service →
// return the result.
package wailsbind

import (
	"context"

	"github.com/cominddev/comind/internal/knowledge"
	"github.com/cominddev/comind/internal/settings"
)

// App is the object bound into the Wails runtime; the frontend calls
// its methods.
type App struct {
	svc      *knowledge.Service
	settings *settings.Manager
}

// New wraps the service and the settings manager for binding.
func New(svc *knowledge.Service, mgr *settings.Manager) *App {
	return &App{svc: svc, settings: mgr}
}

// The methods below are direct pass-throughs; the Wails frontend binds
// to them. The set grows with the screens (tree, review, search, export).

// TopicMap passes the call to knowledge.Service.
func (a *App) TopicMap(ctx context.Context) ([]knowledge.Block, error) {
	return a.svc.TopicMap(ctx)
}

// ExpandTopic passes the call to knowledge.Service.
func (a *App) ExpandTopic(ctx context.Context, topic, hint string) (knowledge.Proposal, error) {
	return a.svc.ExpandTopic(ctx, topic, hint)
}

// ApplyProposal passes the call to knowledge.Service.
func (a *App) ApplyProposal(ctx context.Context, id knowledge.ID, selected []int) error {
	return a.svc.ApplyProposal(ctx, id, selected)
}

// ProposeEdit passes the call to knowledge.Service. The frontend supplies the
// scope (a block or subtree root) and the user's text command.
func (a *App) ProposeEdit(ctx context.Context, scope knowledge.ID, instruction string) (knowledge.Proposal, error) {
	return a.svc.ProposeEdit(ctx, scope, instruction)
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

// PendingProposals passes the call to knowledge.Service: the review queue.
func (a *App) PendingProposals(ctx context.Context) ([]knowledge.Proposal, error) {
	return a.svc.PendingProposals(ctx)
}

// RejectProposal passes the call to knowledge.Service.
func (a *App) RejectProposal(ctx context.Context, id knowledge.ID) error {
	return a.svc.RejectProposal(ctx, id)
}

// ReviewProposal passes the call to knowledge.Service: a pending proposal
// annotated with stale flags and current content for the review screen.
func (a *App) ReviewProposal(ctx context.Context, id knowledge.ID) (knowledge.ProposalReview, error) {
	return a.svc.ReviewProposal(ctx, id)
}

// EffectiveSelection previews the apply cascade for the review screen; pure,
// no storage.
func (a *App) EffectiveSelection(_ context.Context, ops []knowledge.Op, selected []int) []int {
	return knowledge.EffectiveSelection(ops, selected)
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
