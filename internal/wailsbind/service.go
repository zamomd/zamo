package wailsbind

import "github.com/wailsapp/wails/v3/pkg/application"

// ServiceOptions returns the binding options for App.
func ServiceOptions() application.ServiceOptions {
	return application.ServiceOptions{MarshalError: MarshalError}
}

// Service wraps the App for registration with the Wails application.
func Service(a *App) application.Service {
	return application.NewServiceWithOptions(a, ServiceOptions())
}
