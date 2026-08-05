package wailsbind

import "github.com/wailsapp/wails/v3/pkg/application"

// ServiceOptions are the options the App must be registered with.
//
// MarshalError has to be set here and not only in application.Options: the
// binding engine reads the marshaler for a service's methods from the service
// options alone, so a service registered without one falls back to the default
// marshaler. That default renders a wrapped error as an empty JSON object, and
// every mapped message in the frontend degrades to the generic fallback.
func ServiceOptions() application.ServiceOptions {
	return application.ServiceOptions{}
}

// Service wraps the App for registration with the Wails application.
func Service(a *App) application.Service {
	return application.NewServiceWithOptions(a, ServiceOptions())
}
