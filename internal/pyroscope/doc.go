// Package pyroscope builds Push API requests and uploads pprof to Pyroscope.
//
// Wire contract matches Tracewick\Sink\PyroscopePushRequest in the main repo
// (path, application/proto body, series labels, sample ID). Connection reuse is
// via the caller's http.Client / Transport. HTTP classify / ack / release policy
// lives in package delivery.
package pyroscope
