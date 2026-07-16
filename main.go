// Command comind is a single binary with two run modes:
//
//	comind      — the GUI application (Wails);
//	comind mcp  — the MCP server over stdio (spawned by the MCP client
//	              itself: Claude Code, Claude Desktop, Cursor, etc.).
//
// Both modes work with the same database file.
package main

import (
	"context"
	"embed"
	"fmt"
	"os"
	"os/signal"
	"time"

	"github.com/cominddev/comind/internal/config"
	"github.com/cominddev/comind/internal/knowledge"
	"github.com/cominddev/comind/internal/llm"
	"github.com/cominddev/comind/internal/llm/anthropic"
	"github.com/cominddev/comind/internal/llm/openaicompat"
	"github.com/cominddev/comind/internal/mcp"
	"github.com/cominddev/comind/internal/sqlite"
	"github.com/cominddev/comind/internal/ulid"
	"github.com/cominddev/comind/internal/wailsbind"
	"github.com/wailsapp/wails/v3/pkg/application"
)

// The compiled frontend, embedded into the binary. The all: prefix picks
// up files starting with . and _ as well.
//
//go:embed all:frontend/dist
var assets embed.FS

// version is the build version. Release builds inject the git tag via
// -ldflags "-X main.version=..."; local builds keep "dev".
var version = "dev"

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "comind:", err)
		os.Exit(1)
	}
}

func run() error {
	mode := "gui"
	if len(os.Args) > 1 {
		mode = os.Args[1]
	}

	// --version must answer before any setup: it cannot depend on config
	// or the database, which may be absent on a fresh install or in CI.
	if mode == "version" || mode == "--version" || mode == "-v" {
		fmt.Println("comind", version)
		return nil
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	svc, cleanup, err := buildService(ctx)
	if err != nil {
		return err
	}
	defer cleanup()

	switch mode {
	case "gui":
		return runGUI(svc)
	case "mcp":
		return mcp.NewServer(svc, svc, svc, version).Run(ctx)
	default:
		return fmt.Errorf("unknown mode %q (available: gui, mcp)", mode)
	}
}

// buildService wires the dependencies: the only place where adapters
// meet the core.
func buildService(ctx context.Context) (*knowledge.Service, func(), error) {
	cfg, err := config.Load("")
	if err != nil {
		return nil, nil, err
	}

	dbPath, err := cfg.DatabasePath()
	if err != nil {
		return nil, nil, err
	}
	db, err := sqlite.Open(ctx, dbPath)
	if err != nil {
		return nil, nil, err
	}

	llmClient, err := buildLLMClient(cfg.LLM)
	if err != nil {
		db.Close()
		return nil, nil, err
	}
	newID := func() knowledge.ID { return knowledge.ID(ulid.New()) }

	svc := knowledge.NewService(llmClient, sqlite.NewRepo(db, time.Now), newID, time.Now)
	return svc, func() { db.Close() }, nil
}

func buildLLMClient(cfg config.LLM) (knowledge.LLMClient, error) {
	switch cfg.Provider {
	case "":
		// No provider configured is a supported state: AI features are off,
		// but the rest of the application (including comind mcp) works fully.
		return llm.Disabled{}, nil
	case config.ProviderAnthropic:
		return anthropic.New(cfg.APIKeyValue, cfg.Model), nil
	case config.ProviderOpenAICompat:
		return openaicompat.New(cfg.BaseURL, cfg.APIKeyValue, cfg.Model), nil
	default:
		// Load validates the provider; this is unreachable.
		return nil, fmt.Errorf("unknown llm provider %q", cfg.Provider)
	}
}

// runGUI runs the Wails application; it blocks until the window closes.
func runGUI(svc *knowledge.Service) error {
	app := application.New(application.Options{
		Name:        "Comind",
		Description: "Living knowledge base",
		Services: []application.Service{
			application.NewService(wailsbind.New(svc)),
		},
		Assets: application.AssetOptions{
			Handler: application.AssetFileServerFS(assets),
		},
		Mac: application.MacOptions{
			ApplicationShouldTerminateAfterLastWindowClosed: true,
		},
	})

	app.Window.NewWithOptions(application.WebviewWindowOptions{
		Title:  "Comind",
		Width:  1100,
		Height: 720,
		URL:    "/",
	})

	return app.Run()
}
