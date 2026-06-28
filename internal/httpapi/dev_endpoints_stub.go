//go:build !dev

package httpapi

import "github.com/go-chi/chi/v5"

// registerDevEndpoints is a no-op in production builds. The dev variant in
// dev_endpoints_dev.go (built with `-tags dev`) mounts the demo-only routes;
// production binaries deliberately exclude all that code so an env-var
// misconfiguration cannot turn it on.
func registerDevEndpoints(_ *Server, _ chi.Router) {}
