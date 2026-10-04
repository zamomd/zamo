package main

import (
	"runtime"
	"sync"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"
	"github.com/wailsapp/wails/v3/pkg/icons"
	"github.com/zamomd/zamo/internal/scheduler"
)

const (
	trayIdle    = "No scheduled run in progress"
	trayRunning = "Scheduled run in progress…"
)

// runIndicator is the tray menu's line about scheduled work. With the window
// closed it is the only sign that the app is spending LLM calls.
type runIndicator struct {
	mu      sync.Mutex
	running map[string]struct{}
	item    *application.MenuItem
}

func newRunIndicator() *runIndicator {
	return &runIndicator{running: make(map[string]struct{})}
}

func (r *runIndicator) attach(item *application.MenuItem) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.item = item
}

// observe is a scheduler OnRun callback.
func (r *runIndicator) observe(e scheduler.Event) {
	r.mu.Lock()
	defer r.mu.Unlock()
	id := e.Module + "/" + e.Key
	if e.Running {
		r.running[id] = struct{}{}
	} else {
		delete(r.running, id)
	}
	if r.item == nil {
		return
	}
	item, label := r.item, trayIdle
	if len(r.running) > 0 {
		label = trayRunning
	}
	// Runs finish on their own goroutines; the menu belongs to the main thread.
	application.InvokeAsync(func() { item.SetLabel(label) })
}

// keepInTray makes closing the window hide it instead of ending the app, so
// scheduled work goes on behind a tray icon until the owner quits from its
// menu.
func keepInTray(app *application.App, window *application.WebviewWindow, runs *runIndicator) {
	window.RegisterHook(events.Common.WindowClosing, func(e *application.WindowEvent) {
		window.Hide()
		e.Cancel()
	})

	menu := app.NewMenu()
	runs.attach(menu.Add(trayIdle).SetEnabled(false))
	menu.AddSeparator()
	menu.Add("Open Zamo").OnClick(func(*application.Context) { showWindow(window) })
	menu.Add("Quit Zamo").OnClick(func(*application.Context) { app.Quit() })

	tray := app.SystemTray.New().SetMenu(menu)
	if runtime.GOOS == "darwin" {
		tray.SetTemplateIcon(icons.SystrayMacTemplate)
	} else {
		tray.SetIcon(icons.SystrayLight).SetDarkModeIcon(icons.SystrayDark)
	}
}

func showWindow(window *application.WebviewWindow) {
	window.Show().Focus()
}
