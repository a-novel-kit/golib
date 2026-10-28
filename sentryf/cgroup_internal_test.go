package sentryf

import (
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// cgroupFixture returns the files of a cgroup fixture under testdata/cgroup.
func cgroupFixture(name string) fs.FS {
	return os.DirFS(filepath.Join("testdata", "cgroup", name))
}

func TestReadCgroupValue(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name string

		fixture string
		file    string

		expect   uint64
		expectOK bool
	}{
		{
			name:     "Success/Current",
			fixture:  "limited",
			file:     "memory.current",
			expect:   52428800,
			expectOK: true,
		},
		{
			name:     "Success/Limit",
			fixture:  "limited",
			file:     "memory.max",
			expect:   268435456,
			expectOK: true,
		},
		{
			name:    "Error/Unlimited",
			fixture: "unlimited",
			file:    "memory.max",
		},
		{
			name:    "Error/Malformed",
			fixture: "malformed",
			file:    "memory.current",
		},
		{
			name:    "Error/CgroupV1",
			fixture: "v1",
			file:    "memory.current",
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			value, ok := readCgroupValue(cgroupFixture(testCase.fixture), testCase.file)
			require.Equal(t, testCase.expectOK, ok)
			require.Equal(t, testCase.expect, value)
		})
	}
}

func TestReadCPUStat(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name string

		fixture string

		expect map[string]uint64
	}{
		{
			name:    "Success",
			fixture: "limited",
			expect: map[string]uint64{
				"usage_usec":                 120000000,
				"user_usec":                  90000000,
				"system_usec":                30000000,
				"core_sched.force_idle_usec": 0,
				"nr_periods":                 1200,
				"nr_throttled":               30,
				"throttled_usec":             1500000,
				"nr_bursts":                  0,
				"burst_usec":                 0,
			},
		},
		{
			name:    "Success/Malformed",
			fixture: "malformed",
			expect:  map[string]uint64{"nr_throttled": 3},
		},
		{
			name:    "Error/CgroupV1",
			fixture: "v1",
			expect:  map[string]uint64{},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			require.Equal(t, testCase.expect, readCPUStat(cgroupFixture(testCase.fixture)))
		})
	}
}
