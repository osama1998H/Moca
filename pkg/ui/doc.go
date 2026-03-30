// Package ui implements the MOCA server-side UI serving layer.
//
// MOCA ships a React 19 + TypeScript SPA called Desk for the back-office
// interface. This package serves the compiled Desk bundle, manages the
// WebSocket hub for real-time updates, and handles portal page rendering.
//
// Key components:
//   - Desk: serve React Desk SPA and static assets with cache headers
//   - Portal: SSR portal page renderer for public-facing pages
//   - WebSocket: hub for broadcasting real-time events to connected clients
//   - Assets: static asset serving with content-addressable filenames
package ui
