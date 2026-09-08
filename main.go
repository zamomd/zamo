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

	"github.com/cominddev/comind/internal/backup"
	"github.com/cominddev/comind/internal/config"
	"github.com/cominddev/comind/internal/fsexport"
	"github.com/cominddev/comind/internal/jobs"
	"github.com/cominddev/comind/internal/knowledge"
	"github.com/cominddev/comind/internal/llm"
	"github.com/cominddev/comind/internal/mcp"
	"github.com/cominddev/comind/internal/modules/basebackup"
	"github.com/cominddev/comind/internal/modules/export"
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

	livePolicy := settings.NewPolicy(resolved.AIEnabled())
	policy := knowledge.AIPolicy(livePolicy.AIEnabled)
	if mode == "mcp" {
		policy = func() bool {
			r, err := config.Resolve()
			return err == nil && r.AIEnabled()
		}
	}

	svc, holder, base, cleanup, err := buildService(ctx, resolved, policy)
	if err != nil {
		return err
	}
	defer cleanup()

	switch mode {
	case "gui":
		reg := jobs.New(ctx, ulid.New, time.Now)
		return runGUI(svc, settings.New(holder, livePolicy, settingsPath), reg, base)
	case "mcp":
		return mcp.NewServer(svc, svc, svc, version).Run(ctx)
	default:
		return fmt.Errorf("unknown mode %q (available: gui, mcp)", mode)
	}
}

// buildService creates the knowledge service and its runtime dependencies.
func buildService(
	ctx context.Context, resolved config.Resolved, policy knowledge.AIPolicy,
) (*knowledge.Service, *llm.Holder, backup.Base, func(), error) {
	dbPath, err := resolved.Config.DatabasePath()
	if err != nil {
		return nil, nil, backup.Base{}, nil, err
	}
	// A restore accepted in an earlier run is carried out here, before
	// anything opens the base: it is the one moment no process holds it.
	if _, err := backup.ApplyStaged(ctx, dbPath); err != nil {
		return nil, nil, backup.Base{}, nil, err
	}
	db, err := sqlite.Open(ctx, dbPath)
	if err != nil {
		return nil, nil, backup.Base{}, nil, err
	}

	client, err := settings.BuildClient(resolved)
	if err != nil {
		db.Close()
		return nil, nil, backup.Base{}, nil, err
	}
	holder := llm.NewHolder(client)
	newID := func() knowledge.ID { return knowledge.ID(ulid.New()) }

	svc := knowledge.NewService(holder, sqlite.NewRepo(db, time.Now, newID), newID, knowledge.WithAIPolicy(policy))
	return svc, holder, backup.NewBase(db, dbPath, version), func() { db.Close() }, nil
}

// runGUI runs the Wails application; it blocks until the window closes.
//
// Feature modules are assembled here, each from the host capabilities it
// names, and bound as their own Wails services next to the core bindings.
func runGUI(svc *knowledge.Service, mgr *settings.Manager, reg *jobs.Registry, base backup.Base) error {
	bind := wailsbind.New(svc, mgr, reg)
	exporter := export.New(svc, fsexport.Writer{})
	archive := basebackup.New(base)
	app := application.New(application.Options{
		Name:        "Comind",
		Description: "Living knowledge base",
		Services: []application.Service{
			wailsbind.Service(bind),
			application.NewServiceWithOptions(exporter, wailsbind.ServiceOptions()),
			application.NewServiceWithOptions(archive, wailsbind.ServiceOptions()),
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
	basebackup.SetPicker(archive, dialogPicker{app})
	export.SetPicker(exporter, dialogPicker{app})

	app.Window.NewWithOptions(application.WebviewWindowOptions{
		Title:  "Comind",
		Width:  1100,
		Height: 720,
		URL:    "/",
	})

	return app.Run()
}

// dialogPicker asks through the platform's own file dialogs. Cancelling
// yields an empty path and no error, which the bindings pass on as "nothing
// was chosen".
type dialogPicker struct{ app *application.App }

func (d dialogPicker) SaveFile(title, suggestedName string) (string, error) {
	return d.app.Dialog.SaveFile().
		SetMessage(title).
		SetFilename(suggestedName).
		CanCreateDirectories(true).
		PromptForSingleSelection()
}

func (d dialogPicker) OpenFile(title string) (string, error) {
	return d.app.Dialog.OpenFile().
		SetTitle(title).
		CanChooseFiles(true).
		CanChooseDirectories(false).
		AddFilter("Comind backup", "*.comind-backup").
		PromptForSingleSelection()
}

func (d dialogPicker) OpenDirectory(title string) (string, error) {
	return d.app.Dialog.OpenFile().
		SetTitle(title).
		CanChooseFiles(false).
		CanChooseDirectories(true).
		CanCreateDirectories(true).
		PromptForSingleSelection()
}
