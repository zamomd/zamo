// Package wailsbind is an entry point: methods bound into the Wails
// frontend. A thin layer: take a UI call → call knowledge.Service →
// return the result.
package wailsbind

import (
	"context"

	"github.com/cominddev/comind/internal/knowledge"
)

// App is the object bound into the Wails runtime; the frontend calls
// its methods.
type App struct {
	svc *knowledge.Service
}

// New wraps the service for binding.
func New(svc *knowledge.Service) *App { return &App{svc: svc} }

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

// GetSubtree passes the call to knowledge.Service.
func (a *App) GetSubtree(ctx context.Context, root knowledge.ID) ([]knowledge.Block, error) {
	return a.svc.GetSubtree(ctx, root)
}

// EditBlockContent passes the call to knowledge.Service. A manual edit by the
// owner applies directly, without the review queue.
func (a *App) EditBlockContent(ctx context.Context, id knowledge.ID, content string) error {
	return a.svc.EditBlockContent(ctx, id, content)
}
