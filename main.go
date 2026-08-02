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
	"github.com/cominddev/comind/internal/fsexport"
	"github.com/cominddev/comind/internal/knowledge"
	"github.com/cominddev/comind/internal/llm"
	"github.com/cominddev/comind/internal/mcp"
	"github.com/cominddev/comind/internal/settings"
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

	resolved, err := config.Resolve()
	if err != nil {
		return err
	}
	settingsPath, err := config.SettingsPath()
	if err != nil {
		return err
	}

	// Each mode reads the AI policy the way it can learn about a change. The
	// GUI owns the switch, so it holds one in memory and flips it on save. The
	// MCP server has no such event and outlives its client's session, so it
	// re-reads the files; a policy that cannot be read refuses, because the
	// safe answer to "may the AI write" is no.
	sw := settings.NewSwitch(resolved.AIEnabled())
	policy := knowledge.AIPolicy(sw.AIEnabled)
	if mode == "mcp" {
		policy = func() bool {
			r, err := config.Resolve()
			return err == nil && r.AIEnabled()
		}
	}

	svc, holder, cleanup, err := buildService(ctx, resolved, policy)
	if err != nil {
		return err
	}
	defer cleanup()

	switch mode {
	case "gui":
		return runGUI(svc, settings.New(holder, sw, settingsPath))
	case "mcp":
		return mcp.NewServer(svc, svc, svc, version).Run(ctx)
	default:
		return fmt.Errorf("unknown mode %q (available: gui, mcp)", mode)
	}
}

// buildService wires the dependencies: the only place where adapters
// meet the core. It also hands back the live client holder, which the GUI
// needs to wire up the settings screen.
func buildService(
	ctx context.Context, resolved config.Resolved, policy knowledge.AIPolicy,
) (*knowledge.Service, *llm.Holder, func(), error) {
	dbPath, err := resolved.Config.DatabasePath()
	if err != nil {
		return nil, nil, nil, err
	}
	db, err := sqlite.Open(ctx, dbPath)
	if err != nil {
		return nil, nil, nil, err
	}

	client, err := settings.BuildClient(resolved)
	if err != nil {
		db.Close()
		return nil, nil, nil, err
	}
	holder := llm.NewHolder(client)
	newID := func() knowledge.ID { return knowledge.ID(ulid.New()) }

	svc := knowledge.NewService(holder, sqlite.NewRepo(db, time.Now, newID), newID, fsexport.Writer{},
		knowledge.WithAIPolicy(policy))
	return svc, holder, func() { db.Close() }, nil
}

// runGUI runs the Wails application; it blocks until the window closes.
func runGUI(svc *knowledge.Service, mgr *settings.Manager) error {
	bind := wailsbind.New(svc, mgr)
	app := application.New(application.Options{
		Name:        "Comind",
		Description: "Living knowledge base",
		Services: []application.Service{
			application.NewService(bind),
		},
		// Binding errors cross to the frontend as {code, message}, mapped
		// from the domain sentinels in one place; the GUI switches on the
		// code, never on error text.
		MarshalError: wailsbind.MarshalError,
		Assets: application.AssetOptions{
			Handler: application.AssetFileServerFS(assets),
		},
		Mac: application.MacOptions{
			ApplicationShouldTerminateAfterLastWindowClosed: true,
		},
	})
	// The event manager exists only after the app is built; wire it now, before
	// Run, so generation progress reaches the frontend.
	wailsbind.SetEmitter(bind, app.Event)

	app.Window.NewWithOptions(application.WebviewWindowOptions{
		Title:  "Comind",
		Width:  1100,
		Height: 720,
		URL:    "/",
	})

	return app.Run()
}
