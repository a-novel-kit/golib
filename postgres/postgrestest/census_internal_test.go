package postgrestest

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestValidateCensusCoverage(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name string

		rows     []censusRow
		coverage []censusCoverageRow

		expectError string
	}{
		{
			name: "Success",
			rows: []censusRow{
				{Catalog: "pg_class", Class: censusClassRelation},
				{Catalog: "pg_class", Class: "unsupported"},
				{Catalog: "pg_proc", Class: "routine"},
				{Class: "trigger"},
			},
			coverage: []censusCoverageRow{
				{Catalog: "pg_class", Expected: 2},
				{Catalog: "pg_proc", Expected: 1},
			},
		},
		{
			name: "Error/ClassMissing",
			rows: []censusRow{
				{Catalog: "pg_proc", Class: "routine"},
			},
			coverage: []censusCoverageRow{
				{Catalog: "pg_class", Expected: 1},
				{Catalog: "pg_proc", Expected: 1},
			},
			expectError: "coverage for pg_class: rendered 0 objects, catalog has 1",
		},
		{
			name: "Error/ClassOvercounted",
			rows: []censusRow{
				{Catalog: "pg_class", Class: censusClassRelation},
				{Catalog: "pg_class", Class: censusClassRelation},
			},
			coverage:    []censusCoverageRow{{Catalog: "pg_class", Expected: 1}},
			expectError: "coverage for pg_class: rendered 2 objects, catalog has 1",
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			err := validateCensusCoverage(testCase.rows, testCase.coverage)
			if testCase.expectError == "" {
				require.NoError(t, err)
			} else {
				require.ErrorContains(t, err, testCase.expectError)
			}
		})
	}
}

func TestIsCensusBookkeepingObject(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name string

		class    string
		identity string

		expect bool
	}{
		{name: "Relation/Migrations", class: "relation", identity: "bun_migrations", expect: true},
		{name: "Relation/Locks", class: "relation", identity: "bun_migration_locks", expect: true},
		{name: "Relation/Sequence", class: "relation", identity: "bun_migrations_id_seq", expect: true},
		{name: "Relation/LocksSequence", class: "relation", identity: "bun_migration_locks_id_seq", expect: true},
		{name: "Relation/UserTable", class: "relation", identity: "bun_migrations_archive"},
		{name: "Sequence/Migrations", class: "sequence", identity: "bun_migrations_id_seq", expect: true},
		{name: "Sequence/Locks", class: "sequence", identity: "bun_migration_locks_id_seq", expect: true},
		{name: "Sequence/TableName", class: "sequence", identity: "bun_migrations"},
		{name: "Index/MigrationsKey", class: "index", identity: "bun_migrations_pkey", expect: true},
		{name: "Index/MigrationsName", class: "index", identity: "bun_migrations_name_unique", expect: true},
		{name: "Index/LocksKey", class: "index", identity: "bun_migration_locks_pkey", expect: true},
		{name: "Index/LocksTable", class: "index", identity: "bun_migration_locks_table_name_key", expect: true},
		{name: "Index/UserIndex", class: "index", identity: "bun_migrations_archive_pkey"},
		{name: "Column/Migrations", class: "column", identity: "bun_migrations.name", expect: true},
		{name: "Column/Locks", class: "column", identity: "bun_migration_locks.table_name", expect: true},
		{name: "Column/UserTable", class: "column", identity: "bun_migrations_archive.name"},
		{name: "Column/TableName", class: "column", identity: "bun_migrations"},
		{name: "Constraint/Migrations", class: "constraint", identity: "bun_migrations.bun_migrations_pkey", expect: true},
		{name: "Constraint/UserTable", class: "constraint", identity: "probe.bun_migrations_pkey"},
		{name: "Comment/Table", class: "comment", identity: "bun_migration_locks", expect: true},
		{name: "Comment/Column", class: "comment", identity: "bun_migrations.name", expect: true},
		{name: "Comment/UserTable", class: "comment", identity: "bun_migrations_archive"},
		{name: "Routine", class: "routine", identity: "bun_migrations"},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			row := censusRow{Class: testCase.class, Identity: testCase.identity}
			require.Equal(t, testCase.expect, isCensusBookkeepingObject(row))
		})
	}
}
