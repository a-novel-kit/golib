package postgrestest

import (
	"context"
	_ "embed"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/uptrace/bun"
)

//go:embed census.sql
var censusQuery string

// Kept separate so removing a census renderer produces a coverage mismatch.
//
//go:embed censusCoverage.sql
var censusCoverageQuery string

// ErrUnsupportedSchemaObject reports a schema object the census cannot safely compare. Such objects
// fail instead of being silently omitted.
var ErrUnsupportedSchemaObject = errors.New("schema holds an object class the census cannot render")

// The tables Bun keeps its migration history in, which the census leaves out.
const (
	censusMigrationsTable     = "bun_migrations"
	censusMigrationLocksTable = "bun_migration_locks"
)

// The census classes the Go side reads by name.
const (
	censusClassRelation    = "relation"
	censusClassUnsupported = "unsupported"
)

// censusFieldEscaper escapes the characters that would split a record or a field.
var censusFieldEscaper = strings.NewReplacer(`\`, `\\`, "\t", `\t`, "\r", `\r`, "\n", `\n`)

// censusRow is one object census.sql renders.
type censusRow struct {
	Class string `bun:"class"`
	// Catalog names the system catalog the coverage check counts the object against, or stays empty
	// for a class the check leaves out.
	Catalog    string `bun:"catalog"`
	Identity   string `bun:"identity"`
	Definition string `bun:"definition"`
}

// censusCoverageRow is the object count censusCoverage.sql reads from one system catalog.
type censusCoverageRow struct {
	Catalog  string `bun:"catalog"`
	Expected int    `bun:"expected"`
}

// SchemaSnapshot renders supported schema objects as canonical, sorted, one-line records.
// PostgreSQL renders definitions, so statement order and DDL spelling do not affect the result.
//
// It includes database-scoped extensions and schemas, reads no row data, and returns
// [ErrUnsupportedSchemaObject] for an object class it cannot render.
func SchemaSnapshot(ctx context.Context, db bun.IDB, schema string) (string, error) {
	var rows []censusRow

	err := db.NewRaw(censusQuery, schema).Scan(ctx, &rows)
	if err != nil {
		return "", fmt.Errorf("census schema %q: %w", schema, err)
	}

	var coverage []censusCoverageRow

	err = db.NewRaw(censusCoverageQuery, schema).Scan(ctx, &coverage)
	if err != nil {
		return "", fmt.Errorf("check census coverage for schema %q: %w", schema, err)
	}

	err = validateCensusCoverage(rows, coverage)
	if err != nil {
		return "", fmt.Errorf("check census coverage for schema %q: %w", schema, err)
	}

	return renderCensus(rows)
}

// renderCensus writes one tab-separated record per row, skipping the tables Bun keeps its
// migration history in. It returns [ErrUnsupportedSchemaObject] naming every unsupported row.
func renderCensus(rows []censusRow) (string, error) {
	var (
		unsupported []string
		out         strings.Builder
	)

	for _, row := range rows {
		if row.Class == censusClassUnsupported {
			unsupported = append(unsupported, row.Identity+" "+row.Definition)

			continue
		}

		if isCensusBookkeepingObject(row) {
			continue
		}

		_, _ = fmt.Fprintf(&out, "%s\t%s\t%s\n", censusFieldEscaper.Replace(row.Class),
			censusFieldEscaper.Replace(row.Identity), censusFieldEscaper.Replace(row.Definition))
	}

	if len(unsupported) > 0 {
		return "", fmt.Errorf("%w: %s", ErrUnsupportedSchemaObject, strings.Join(unsupported, "; "))
	}

	return out.String(), nil
}

// isCensusBookkeepingObject reports whether row belongs to the tables Bun keeps its migration
// history in.
func isCensusBookkeepingObject(row censusRow) bool {
	switch row.Class {
	case "column", "constraint":
		return strings.HasPrefix(row.Identity, censusMigrationsTable+".") ||
			strings.HasPrefix(row.Identity, censusMigrationLocksTable+".")
	case censusClassRelation:
		return row.Identity == censusMigrationsTable ||
			row.Identity == censusMigrationLocksTable ||
			row.Identity == censusMigrationsTable+"_id_seq" ||
			row.Identity == censusMigrationLocksTable+"_id_seq"
	case "sequence":
		return row.Identity == censusMigrationsTable+"_id_seq" ||
			row.Identity == censusMigrationLocksTable+"_id_seq"
	case "index":
		return row.Identity == censusMigrationsTable+"_pkey" ||
			row.Identity == censusMigrationsTable+"_name_unique" ||
			row.Identity == censusMigrationLocksTable+"_pkey" ||
			row.Identity == censusMigrationLocksTable+"_table_name_key"
	case "comment":
		return row.Identity == censusMigrationsTable ||
			row.Identity == censusMigrationLocksTable ||
			strings.HasPrefix(row.Identity, censusMigrationsTable+".") ||
			strings.HasPrefix(row.Identity, censusMigrationLocksTable+".")
	default:
		return false
	}
}

// validateCensusCoverage checks that rows hold as many objects of each catalog as coverage counts,
// so an object class census.sql fails to render cannot vanish from the snapshot.
func validateCensusCoverage(rows []censusRow, coverage []censusCoverageRow) error {
	actual := make(map[string]int, len(coverage))

	for _, row := range rows {
		if row.Catalog != "" {
			actual[row.Catalog]++
		}
	}

	for _, expected := range coverage {
		if actual[expected.Catalog] != expected.Expected {
			return fmt.Errorf("schema census coverage for %s: rendered %d objects, catalog has %d",
				expected.Catalog, actual[expected.Catalog], expected.Expected)
		}
	}

	return nil
}

// SnapshotDelta returns sorted per-object differences between want and got.
// Records are compared as a set.
func SnapshotDelta(want, got string) []string {
	wantLines, gotLines := snapshotLines(want), snapshotLines(got)

	var delta []string

	for line := range wantLines {
		if !gotLines[line] {
			delta = append(delta, "missing: "+line)
		}
	}

	for line := range gotLines {
		if !wantLines[line] {
			delta = append(delta, "unexpected: "+line)
		}
	}

	slices.Sort(delta)

	return delta
}

// snapshotLines returns the set of records snapshot holds.
func snapshotLines(snapshot string) map[string]bool {
	lines := map[string]bool{}

	for line := range strings.SplitSeq(strings.TrimRight(snapshot, "\n"), "\n") {
		if line != "" {
			lines[line] = true
		}
	}

	return lines
}
