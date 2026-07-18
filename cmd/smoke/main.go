// Command smoke is the fresh-run smoke test: it launches the built comind
// binary in MCP mode against a throwaway data dir — as on a clean machine — and
// drives it to the first proposal over stdio. It exits non-zero with a reason
// if any step fails, so CI can gate first-run health on both OSes.
//
// Usage: smoke <path-to-comind-binary>
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"time"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintln(os.Stderr, "usage: smoke <path-to-comind-binary>")
		os.Exit(2)
	}
	if err := run(os.Args[1]); err != nil {
		fmt.Fprintln(os.Stderr, "smoke: "+err.Error())
		os.Exit(1)
	}
	fmt.Println("smoke: fresh run reached the first proposal")
}

func run(bin string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// A throwaway data/config home so the run is genuinely first-run: an empty
	// base, no prior config, AI off (no provider) — propose_blocks is the
	// key-less write path, so it still reaches a proposal.
	home, err := os.MkdirTemp("", "comind-smoke-")
	if err != nil {
		return fmt.Errorf("temp home: %w", err)
	}
	defer os.RemoveAll(home)

	cmd := exec.CommandContext(ctx, bin, "mcp")
	cmd.Env = append(os.Environ(),
		"XDG_DATA_HOME="+home+"/data",
		"XDG_CONFIG_HOME="+home+"/config",
	)
	cmd.Stderr = os.Stderr

	client := mcpsdk.NewClient(&mcpsdk.Implementation{Name: "smoke", Version: "0"}, nil)
	session, err := client.Connect(ctx, &mcpsdk.CommandTransport{Command: cmd}, nil)
	if err != nil {
		return fmt.Errorf("connect to the MCP server: %w", err)
	}
	defer session.Close()

	var out struct {
		ProposalID string `json:"proposal_id"`
		Status     string `json:"status"`
	}
	res, err := session.CallTool(ctx, &mcpsdk.CallToolParams{
		Name: "propose_blocks",
		Arguments: map[string]any{
			"title": "smoke",
			"ops":   []map[string]any{{"op": "add", "temp_id": "t1", "content": "hello from the smoke test"}},
		},
	})
	if err != nil {
		return fmt.Errorf("call propose_blocks: %w", err)
	}
	if res.IsError {
		return fmt.Errorf("propose_blocks returned a tool error: %+v", res.Content)
	}
	raw, err := json.Marshal(res.StructuredContent)
	if err != nil {
		return fmt.Errorf("re-marshal propose_blocks output: %w", err)
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return fmt.Errorf("decode propose_blocks output: %w", err)
	}
	if out.ProposalID == "" {
		return fmt.Errorf("no proposal id in the response: %+v", out)
	}
	return nil
}
