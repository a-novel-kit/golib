// Package postgrestest provides PostgreSQL test harnesses backed by postgres.
//
// It owns disposable-database, isolated-schema, migration-roundtrip, and schema-census helpers;
// production connection, transaction, and migration primitives remain in postgres.
package postgrestest
