package postgres

import (
	"context"
	_ "embed"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/uptrace/bun"
)

// censusQuery renders a schema as sorted (class, identity, definition) rows. Its own header
// documents the invariants it upholds.
//
//go:embed census.sql
var censusQuery string

// ErrUnsupportedSchemaObject is returned by [SchemaSnapshot] when the schema holds an object class
// the census cannot render, such as an aggregate or a custom operator.
//
// A snapshot that quietly omitted such an object would compare equal to one taken before it
// existed, so the whole point of the snapshot — proving nothing was left behind — would be lost on
// exactly the object nobody thought to cover. Teaching census.sql the class is the fix.
var ErrUnsupportedSchemaObject = errors.New("schema holds an object class the census cannot render")

// censusBookkeepingPrefix marks the tables bun's migrator keeps for itself. They record which
// migrations ran, so they exist at every step of a rollback and describe the harness rather than
// the schema under test.
//
// The prefix, rather than the two exact names, also covers the indexes and constraints those tables
// own. A caller's own table starting with it would be invisible to the census.
const censusBookkeepingPrefix = "bun_migration"

// censusClassUnsupported is the class census.sql gives an object it cannot render, so a gap in
// coverage arrives as a row to fail on rather than as silence.
const censusClassUnsupported = "unsupported"

// censusRow is one object in a schema census.
type censusRow struct {
	Class      string `bun:"class"`
	Identity   string `bun:"identity"`
	Definition string `bun:"definition"`
}

// SchemaSnapshot renders every object in schema as canonical, sorted text, one line per object.
// Two schemas are identical exactly when their snapshots are, which is what makes a snapshot usable
// as a fixture: [RunMigrationRoundtripTest] compares one against the next to prove a rollback
// landed where it should.
//
// The rendering is stable across databases and across the order statements were run in, so a
// snapshot can be committed and diffed. PostgreSQL renders the definitions itself, so a snapshot
// describes the schema that exists rather than the DDL that produced it.
//
// Alongside the objects in schema it covers extensions and schemas, which are database-scoped but
// which a migration can create and a rollback must therefore remove. It reads no row data.
//
// It returns [ErrUnsupportedSchemaObject] when the schema holds a class the census cannot render.
func SchemaSnapshot(ctx context.Context, db bun.IDB, schema string) (string, error) {
	var rows []censusRow

	err := db.NewRaw(censusQuery, schema).Scan(ctx, &rows)
	if err != nil {
		return "", fmt.Errorf("census schema %q: %w", schema, err)
	}

	var (
		unsupported []string
		out         strings.Builder
	)

	for _, row := range rows {
		if row.Class == censusClassUnsupported {
			unsupported = append(unsupported, row.Identity+" "+row.Definition)

			continue
		}

		if strings.HasPrefix(row.Identity, censusBookkeepingPrefix) {
			continue
		}

		out.WriteString(row.Class)
		out.WriteByte('\t')
		out.WriteString(row.Identity)
		out.WriteByte('\t')
		out.WriteString(row.Definition)
		out.WriteByte('\n')
	}

	if len(unsupported) > 0 {
		return "", fmt.Errorf("%w: %s", ErrUnsupportedSchemaObject, strings.Join(unsupported, "; "))
	}

	return out.String(), nil
}

// SnapshotDelta reports how got differs from want, as one line per object, or nil when the two
// snapshots are identical.
//
// Lines are compared as a set, so the report names the objects that actually differ instead of the
// point where two orderings diverged. The result is sorted, so a failure message reads the same on
// every run.
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

// snapshotLines indexes a snapshot by line. A snapshot holds one object per line and the census
// sorts them, so a set comparison loses nothing and gains a per-object report.
func snapshotLines(snapshot string) map[string]bool {
	lines := map[string]bool{}

	for line := range strings.SplitSeq(strings.TrimRight(snapshot, "\n"), "\n") {
		if line != "" {
			lines[line] = true
		}
	}

	return lines
}
