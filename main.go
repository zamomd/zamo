// Command zamo is a single binary with two run modes:
//
//	zamo      — the GUI application (Wails);
//	zamo mcp  — the MCP server over stdio (spawned by the MCP client
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

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/zamomd/zamo/internal/agent"
	"github.com/zamomd/zamo/internal/backup"
	"github.com/zamomd/zamo/internal/config"
	"github.com/zamomd/zamo/internal/fsexport"
	"github.com/zamomd/zamo/internal/gitrepo"
	"github.com/zamomd/zamo/internal/jobs"
	"github.com/zamomd/zamo/internal/knowledge"
	"github.com/zamomd/zamo/internal/llm"
	"github.com/zamomd/zamo/internal/mcp"
	"github.com/zamomd/zamo/internal/modules/assistant"
	"github.com/zamomd/zamo/internal/modules/basebackup"
	"github.com/zamomd/zamo/internal/modules/export"
	"github.com/zamomd/zamo/internal/modules/smartcron"
	"github.com/zamomd/zamo/internal/modules/vaultimport"
	"github.com/zamomd/zamo/internal/modules/vaultimport/logseq"
	"github.com/zamomd/zamo/internal/modules/vaultimport/obsidian"
	"github.com/zamomd/zamo/internal/modules/vaultimport/zamo"
	"github.com/zamomd/zamo/internal/scheduler"
	"github.com/zamomd/zamo/internal/settings"
	"github.com/zamomd/zamo/internal/sqlite"
	"github.com/zamomd/zamo/internal/ulid"
	"github.com/zamomd/zamo/internal/wailsbind"
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
		fmt.Fprintln(os.Stderr, "zamo:", err)
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
		fmt.Println("zamo", version)
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

	svc, holder, base, store, cleanup, err := buildService(ctx, resolved, policy)
	if err != nil {
		return err
	}
	defer cleanup()

	switch mode {
	case "gui":
		reg := jobs.New(ctx, ulid.New, time.Now)
		runner := agent.New(holder, svc, livePolicy.AIEnabled, reg)
		return runGUI(ctx, svc, settings.New(holder, livePolicy, settingsPath), reg, runner, base, store)
	case "mcp":
		return mcp.NewServer(svc, svc, version).Run(ctx)
	default:
		return fmt.Errorf("unknown mode %q (available: gui, mcp)", mode)
	}
}

// buildService creates the knowledge service and its runtime dependencies,
// the schedule store among them.
func buildService(
	ctx context.Context, resolved config.Resolved, policy knowledge.AIPolicy,
) (*knowledge.Service, *llm.Holder, backup.Base, *sqlite.Schedules, func(), error) {
	dbPath, err := resolved.Config.DatabasePath()
	if err != nil {
		return nil, nil, backup.Base{}, nil, nil, err
	}
	// A restore accepted in an earlier run is carried out here, before
	// anything opens the base: it is the one moment no process holds it.
	if _, err := backup.ApplyStaged(ctx, dbPath); err != nil {
		return nil, nil, backup.Base{}, nil, nil, err
	}
	db, err := sqlite.Open(ctx, dbPath)
	if err != nil {
		return nil, nil, backup.Base{}, nil, nil, err
	}

	client, err := settings.BuildClient(resolved)
	if err != nil {
		db.Close()
		return nil, nil, backup.Base{}, nil, nil, err
	}
	holder := llm.NewHolder(client)
	newID := func() knowledge.ID { return knowledge.ID(ulid.New()) }

	svc := knowledge.NewService(sqlite.NewRepo(db, time.Now, newID), newID, knowledge.WithAIPolicy(policy))
	store := sqlite.NewSchedules(db, time.Now)
	return svc, holder, backup.NewBase(db, dbPath, version), store, func() { db.Close() }, nil
}

// runGUI runs the Wails application; it blocks until the window closes.
func runGUI(
	ctx context.Context, svc *knowledge.Service, mgr *settings.Manager, reg *jobs.Registry,
	runner *agent.Runner, base backup.Base, store *sqlite.Schedules,
) error {
	bind := wailsbind.New(svc, mgr, reg)
	var emitter wailsbind.Emitter
	emitRun := wailsbind.EmitScheduleRuns(func() wailsbind.Emitter { return emitter })
	runs := newRunIndicator()
	sched, err := scheduler.New(store, reg, time.Now, ulid.New, scheduler.Options{
		OnRun: func(e scheduler.Event) {
			emitRun(e)
			runs.observe(e)
		},
	})
	if err != nil {
		return err
	}
	wailsbind.SetScheduler(bind, sched)
	// Runs the owner starts report their steps to the window; a scheduled
	// run has nobody watching and reports none.
	attended := wailsbind.Announce(runner, func() wailsbind.Emitter { return emitter })
	wailsbind.SetAgent(bind, attended)
	cron := smartcron.New(sched.For(smartcron.Name), runner, svc)
	helper := assistant.New(attended, svc)
	exporter := export.New(svc, fsexport.Writer{}, gitrepo.New(time.Now), sched.For(export.Name), time.Now)
	archive := basebackup.New(base)
	importer := vaultimport.New(svc, svc, zamo.Adapter{}, obsidian.Adapter{}, logseq.Adapter{})
	var window *application.WebviewWindow
	app := application.New(application.Options{
		Name:        "Zamo",
		Description: "Living knowledge base",
		Services: []application.Service{
			wailsbind.Service(bind),
			application.NewServiceWithOptions(exporter, wailsbind.ServiceOptions()),
			application.NewServiceWithOptions(archive, wailsbind.ServiceOptions()),
			application.NewServiceWithOptions(importer, wailsbind.ServiceOptions()),
			application.NewServiceWithOptions(cron, wailsbind.ServiceOptions()),
			application.NewServiceWithOptions(helper, wailsbind.ServiceOptions()),
		},
		MarshalError: wailsbind.MarshalError,
		Assets: application.AssetOptions{
			Handler: application.AssetFileServerFS(assets),
		},
		SingleInstance: &application.SingleInstanceOptions{
			UniqueID: "dev.zamo.app",
			OnSecondInstanceLaunch: func(application.SecondInstanceData) {
				if window != nil {
					showWindow(window)
				}
			},
		},
	})

	wailsbind.SetEmitter(bind, app.Event)
	emitter = app.Event
	sched.Start(ctx)
	basebackup.SetPicker(archive, dialogPicker{app})
	export.SetPicker(exporter, dialogPicker{app})
	vaultimport.SetPicker(importer, dialogPicker{app})

	window = app.Window.NewWithOptions(application.WebviewWindowOptions{
		Title:  "Zamo",
		Width:  1100,
		Height: 720,
		URL:    "/",
	})
	keepInTray(app, window, runs)

	return app.Run()
}

// dialogPicker asks through the platform's own file dialogs. Cancelling
// yields an empty path and no error, which the bindings pass on as "nothing was chosen".
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
		AddFilter("Zamo backup", "*.zamo-backup").
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
