package sentryf

import (
	"io/fs"
	"strconv"
	"strings"
)

// cgroupRoot holds the cgroup v2 files of the process's own cgroup, inside a container with its own
// cgroup namespace.
const cgroupRoot = "/sys/fs/cgroup"

// readCgroupValue reads name, a cgroup v2 file holding one number, such as memory.current. It returns
// false when the file is missing or holds anything else, such as max, the value of an unlimited
// control.
func readCgroupValue(cgroup fs.FS, name string) (uint64, bool) {
	content, err := fs.ReadFile(cgroup, name)
	if err != nil {
		return 0, false
	}

	value, err := strconv.ParseUint(strings.TrimSpace(string(content)), 10, 64)

	return value, err == nil
}

// readCPUStat reads the counters of cpu.stat, such as usage_usec, by name. It returns an empty map
// when the file is missing, and skips a line that holds no counter.
func readCPUStat(cgroup fs.FS) map[string]uint64 {
	counters := map[string]uint64{}

	content, err := fs.ReadFile(cgroup, "cpu.stat")
	if err != nil {
		return counters
	}

	for line := range strings.Lines(string(content)) {
		name, value, _ := strings.Cut(strings.TrimSpace(line), " ")

		counter, err := strconv.ParseUint(value, 10, 64)
		if err == nil {
			counters[name] = counter
		}
	}

	return counters
}
