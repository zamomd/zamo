// Package llm is the shared part of the LLM adapter: prompt templates and
// parsing of model responses into domain operations.
package llm

import (
	"bytes"
	"embed"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/cominddev/comind/internal/knowledge"
)

// Prompts are template files: the person responsible for expansion
// quality will iterate on them often, without digging through the code.
//
//go:embed prompts/*.md
var Prompts embed.FS

// Tree limits. A single expansion must stay reviewable, so the parser caps how deep
// and how wide a proposal can grow; anything past the caps is dropped, not an error.
const (
	maxDepth  = 5
	maxBlocks = 60
)

// treeNode is the shape ParseOps expects from the model: a topic tree of markdown
// blocks. It is deliberately separate from knowledge.Op — the model emits a nested
// tree, and ParseOps flattens it into an ordered, parent-before-child op list.
type treeNode struct {
	Content  string     `json:"content"`
	Children []treeNode `json:"children"`
}

// ParseOps parses a model's JSON response into domain operations:
// validation and garbage filtering. Shared by all providers.
//
// The reply may be wrapped in markdown fences or surrounding prose; only the
// outermost JSON object is read. Blank nodes are dropped and their children
// re-parented; the tree is capped at maxDepth levels and maxBlocks blocks. A reply
// with no usable JSON, or no block carrying real content, yields ErrLLMBadResponse.
func ParseOps(raw []byte) ([]knowledge.Op, error) {
	obj, ok := extractJSONObject(raw)
	if !ok {
		return nil, fmt.Errorf("%w: no JSON object in reply", knowledge.ErrLLMBadResponse)
	}

	var root treeNode
	if err := json.Unmarshal(obj, &root); err != nil {
		return nil, fmt.Errorf("%w: %w", knowledge.ErrLLMBadResponse, err)
	}

	var ops []knowledge.Op
	seq := 0
	var walk func(node treeNode, parent string, depth int)
	walk = func(node treeNode, parent string, depth int) {
		if len(ops) >= maxBlocks || depth > maxDepth {
			return
		}
		content := strings.TrimSpace(node.Content)
		if content == "" {
			// An empty node contributes no block: skip it, but keep its children,
			// re-parented onto its own parent and at the same depth.
			for _, child := range node.Children {
				walk(child, parent, depth)
			}
			return
		}
		seq++
		tempID := "t" + strconv.Itoa(seq)
		ops = append(ops, knowledge.Op{
			Kind:    knowledge.OpAdd,
			TempID:  tempID,
			Parent:  parent,
			Content: content,
		})
		for _, child := range node.Children {
			walk(child, tempID, depth+1)
		}
	}
	walk(root, "", 1)

	if len(ops) == 0 {
		return nil, fmt.Errorf("%w: reply had no usable blocks", knowledge.ErrLLMBadResponse)
	}
	return ops, nil
}

// extractJSONObject returns the slice from the first '{' to the last '}', stripping
// markdown fences and prose that weak local models wrap around the JSON. It does not
// validate the JSON — json.Unmarshal does that.
func extractJSONObject(raw []byte) ([]byte, bool) {
	start := bytes.IndexByte(raw, '{')
	end := bytes.LastIndexByte(raw, '}')
	if start < 0 || end < start {
		return nil, false
	}
	return raw[start : end+1], true
}
