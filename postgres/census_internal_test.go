package postgres

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
