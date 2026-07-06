// Package llm is the shared part of the LLM adapter: prompt templates and
// parsing of model responses into domain operations.
package llm

import (
	"embed"

	"github.com/cominddev/comind/internal/knowledge"
)

// Prompts are template files: the person responsible for expansion
// quality will iterate on them often, without digging through the code.
//
//go:embed prompts/*.md
var Prompts embed.FS

// ParseOps parses a model's JSON response into domain operations:
// validation and garbage filtering. Shared by all providers.
func ParseOps(raw []byte) ([]knowledge.Op, error) {
	return nil, knowledge.ErrNotImplemented
}
