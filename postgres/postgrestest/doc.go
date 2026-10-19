// Package postgrestest provides PostgreSQL test harnesses backed by postgres.
//
// It owns the harnesses that stand up a database for a test, prove a migration set reversible, and
// snapshot a schema. The connection, transaction and migration primitives they rest on live in
// postgres.
package postgrestest
