// Package agent is the long-running delivery sidecar: poll loop, pause, and
// shutdown. One claim→merge→push→ack pass lives on Drainer (same domain role as
// Tracewick\Runtime\SpoolDrainer), with agent HTTP policy from package delivery
// (401/403/404 keep spool; connection reuse on the HTTP client).
package agent
