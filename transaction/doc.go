// Package transaction declares the scope a unit of work runs in, independently of what stores it.
//
// It depends on context alone. Nothing here reaches a connection, a pool or a driver symbol, so a
// layer that must stay free of persistence detail can import it and still name the scope it needs.
// Implementations live with their driver, such as
// [github.com/a-novel-kit/golib/postgres.Transactor] for PostgreSQL.
package transaction
