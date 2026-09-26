// Package llm is the shared part of the LLM adapter: prompt templates and
// parsing of model responses into domain operations.
package llm

import (
	"bytes"
	"embed"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"fmt"
	"strconv"
	"strings"

	"github.com/zamomd/zamo/internal/knowledge"
)

// Prompts are template files: the person responsible for expansion
// quality will iterate on them often, without digging through the code.
//
//go:embed prompts/*.md
var Prompts embed.FS

// Tree limits. A single expansion must stay reviewable, so the parser caps how deep
// and how wide a batch of ops can grow; anything past the caps is dropped, not an error.
// Depth counts emitted blocks, not raw JSON nesting: blank nodes are skipped without
// adding a level (see the walk in ParseOps).
const (
	maxDepth  = 5
	maxBlocks = 60

	// maxReplyBytes bounds the raw reply. The reply crosses the untrusted model
	// boundary and a reviewable expansion is a few KB; a multi-megabyte reply is
	// hostile or broken and is refused before it is decoded or allocated.
	maxReplyBytes = 1 << 20 // 1 MiB

	// maxNestDepth bounds raw JSON nesting so a degenerate reply cannot drive deep
	// recursion in the decoder and the walk. It sits far above any legitimate tree
	// (maxDepth emitted levels, two JSON levels each), so crossing it means hostile
	// input, not an over-long expansion — the latter is capped silently, this is not.
	maxNestDepth = 64
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
	if len(raw) > maxReplyBytes {
		return nil, fmt.Errorf("%w: reply too large (%d bytes)", knowledge.ErrLLMBadResponse, len(raw))
	}
	if tooDeeplyNested(raw) {
		return nil, fmt.Errorf("%w: reply nested past %d levels", knowledge.ErrLLMBadResponse, maxNestDepth)
	}
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

// tooDeeplyNested reports whether raw carries JSON structural nesting deeper than
// maxNestDepth. It scans the raw bytes before decoding — so a degenerate reply is
// refused without building the tree — and tracks string literals so braces inside
// block content do not count toward the depth.
func tooDeeplyNested(raw []byte) bool {
	depth := 0
	inString := false
	escaped := false
	for _, b := range raw {
		if inString {
			switch {
			case escaped:
				escaped = false
			case b == '\\':
				escaped = true
			case b == '"':
				inString = false
			}
			continue
		}
		switch b {
		case '"':
			inString = true
		case '{', '[':
			depth++
			if depth > maxNestDepth {
				return true
			}
		case '}', ']':
			if depth > 0 {
				depth--
			}
		}
	}
	return false
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
		if json.UnmarshalDecode(jsontext.NewDecoder(bytes.NewReader(raw[i:])), &node) != nil {
			continue
		}
		if strings.TrimSpace(node.Content) == "" && len(node.Children) == 0 {
			continue
		}
		return node, true
	}
	return treeNode{}, false
}
