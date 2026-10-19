// Package httpftest scripts an HTTP server for testing outbound calls.
//
// Replies are declared up front and returned in order, and every request is recorded for later
// assertions. A reply can also hang until the caller gives up, or drop the connection: two failures
// a hand-written handler makes hard to produce.
package httpftest
