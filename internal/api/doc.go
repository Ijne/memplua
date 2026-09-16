// Package api exposes the loopback-only HTTP boundary used by the desktop UI
// and the contributor control panel. It owns authentication, CORS, REST route
// validation, SSE replay, and transport-specific response projections; it does
// not implement domain or persistence rules.
package api
