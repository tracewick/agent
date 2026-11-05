// Package delivery implements agent delivery policy: HTTP classify
// (ack / retry / reject / pause / shrink), claim ownership budget, decode and
// in-flight memory limits, and SIGTERM shutdown helpers. Behaviour matches
// AGENTS.md and the main-repo spool / Push contracts.
package delivery
