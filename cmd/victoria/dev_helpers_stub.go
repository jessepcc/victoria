//go:build !dev

package main

import "github.com/jessepcc/victoria/internal/app"

// enableDevHelpers is a no-op in production builds. See dev_helpers_dev.go
// for the implementation that's compiled in only with `-tags dev`.
func enableDevHelpers(_ *app.App, _ bool) {}
