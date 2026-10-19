// Package postgres carries a PostgreSQL connection on the request context, runs work inside
// transactions installed on that context, and applies migrations.
//
// The presets subpackage builds the connection pools behind [Config]. The postgrestest subpackage
// builds the databases tests run against.
package postgres
