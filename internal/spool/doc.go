// Package spool implements spool directory claim, scan, reject, and stale reclaim.
//
// Claim rename (canon): X.spool → .X.spool.claimed-<readerId>-<epoch>.
package spool
