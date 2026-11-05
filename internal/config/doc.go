// Package config loads and validates agent flags and environment settings.
// Secrets (Pyroscope password) come from the environment only, never argv
// (parity with tracewick-drain). Optional TRACEWICK_TLS_CA / --tls-ca adds PEM roots.
package config
