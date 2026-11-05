// Package aggregate implements ADR 12 for the delivery agent: SpoolAggregationKey
// hashing, wall-clock merge windows, process-scoped CardinalityGate, and grouping
// of SpoolFile headers before compact-stacks merge / Push.
//
// Stack-level merge (ProfileMerger) lands with the compact-stacks → pprof path.
package aggregate
