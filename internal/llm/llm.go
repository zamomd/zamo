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
// Depth counts emitted blocks, not raw JSON nesting: blank nodes are skipped without
// adding a level (see the walk in ParseOps).
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
	root, ok := firstJSONObject(raw)
	if !ok {
		return nil, fmt.Errorf("%w: no JSON object in reply", knowledge.ErrLLMBadResponse)
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

// firstJSONObject scans for the first '{' that begins a complete JSON object and decodes
// it into a treeNode, ignoring markdown fences, prose and trailing text around it — even
// prose that itself contains braces. It returns the first object that carries usable data:
// an object with neither content nor children (a bare "{}" or one with only unknown fields,
// which models routinely emit in a preamble) is skipped so it cannot shadow the real tree.
func firstJSONObject(raw []byte) (treeNode, bool) {
	for i := range raw {
		if raw[i] != '{' {
			continue
		}
		var node treeNode
		if json.NewDecoder(bytes.NewReader(raw[i:])).Decode(&node) != nil {
			continue
		}
		if strings.TrimSpace(node.Content) == "" && len(node.Children) == 0 {
			continue
		}
		return node, true
	}
	return treeNode{}, false
}
