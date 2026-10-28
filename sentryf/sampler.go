package sentryf

import (
	"context"
	"database/sql"
	"io/fs"
	"runtime/metrics"
	"slices"
	"sync"
	"time"

	"github.com/getsentry/sentry-go"
)

// sampleInterval is the time between two samples of the process's resource use.
const sampleInterval = time.Minute

var (
	// stopSampler stops the sampler [Init] started last, and returns once it has. It does nothing
	// when that sampler has stopped already.
	stopSampler = func() {}

	// poolsMu guards pools.
	poolsMu sync.Mutex
	// pools holds the database connection pools [SamplePool] added.
	pools []*sql.DB
)

// SamplePool adds pool to the database connection pools the sampler reads. golib's postgres presets
// add the primary pool of each database they open, so a service calls it only for a pool it opens
// itself. A pool stays added once closed, and then holds no connection.
func SamplePool(pool *sql.DB) {
	poolsMu.Lock()
	defer poolsMu.Unlock()

	pools = append(pools, pool)
}

// sampler sends the resource use of the process to Sentry as metrics. It keeps the previous value of
// each cumulative counter, and sends the change since that value.
type sampler struct {
	meter  sentry.Meter
	cgroup fs.FS

	// at is the time of the previous sample, or of the start.
	at time.Time
	// cpu holds the counters of cpu.stat at the previous sample, or at the start.
	cpu map[string]uint64
	// gcCycles counts the garbage collections completed by the previous sample.
	gcCycles uint64
	// poolWaits counts the connections the pools handed out after a wait, by the previous sample.
	poolWaits int64
}

// startSampler starts a sampler that sends the resource use of the process to meter every interval,
// reading the cgroup files from cgroup. The function it returns stops the sampler, and returns once
// it has.
func startSampler(meter sentry.Meter, cgroup fs.FS, interval time.Duration) func() {
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})

	go func() {
		defer close(done)

		newSampler(meter, cgroup).run(ctx, interval)
	}()

	return func() {
		cancel()
		<-done
	}
}

// newSampler returns a sampler that reads cpu.stat once now, so its first sample already sends the
// CPU use.
func newSampler(meter sentry.Meter, cgroup fs.FS) *sampler {
	return &sampler{meter: meter, cgroup: cgroup, at: time.Now(), cpu: readCPUStat(cgroup)}
}

// run samples every interval, until ctx ends.
func (s *sampler) run(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case now := <-ticker.C:
			s.sample(now)
		}
	}
}

// sample sends the resource use read at now. A value the platform does not expose, such as a file of
// cgroup v1, skips its metric.
func (s *sampler) sample(now time.Time) {
	elapsed := now.Sub(s.at)
	s.at = now

	s.sampleMemory()
	s.sampleCPU(elapsed)
	s.sampleRuntime()
	s.samplePools()
}

// sampleMemory sends the memory the cgroup uses, and its limit when it has one.
func (s *sampler) sampleMemory() {
	current, ok := readCgroupValue(s.cgroup, "memory.current")
	if ok {
		s.meter.Gauge("container.memory.current", float64(current), sentry.WithUnit(sentry.UnitByte))
	}

	limit, ok := readCgroupValue(s.cgroup, "memory.max")
	if ok {
		s.meter.Gauge("container.memory.limit", float64(limit), sentry.WithUnit(sentry.UnitByte))
	}
}

// sampleCPU sends the CPUs the cgroup used over elapsed. A CPU limit grants a quota per period, and
// sampleCPU also sends the share of those periods in which the cgroup ran out of quota.
func (s *sampler) sampleCPU(elapsed time.Duration) {
	current := readCPUStat(s.cgroup)
	changes := counterChanges(s.cpu, current)
	s.cpu = current

	usage, ok := changes["usage_usec"]
	if ok {
		s.meter.Gauge("container.cpu.usage", usage/float64(elapsed.Microseconds()),
			sentry.WithUnit(sentry.UnitRatio))
	}

	// The cgroup counts periods only while it runs under a CPU limit.
	periods := changes["nr_periods"]
	if periods > 0 {
		s.meter.Gauge("container.cpu.throttled", changes["nr_throttled"]/periods,
			sentry.WithUnit(sentry.UnitRatio))
	}
}

// sampleRuntime sends the heap and the goroutines of the Go runtime, and the garbage collections it
// completed since the previous sample.
func (s *sampler) sampleRuntime() {
	samples := []metrics.Sample{
		{Name: "/memory/classes/heap/objects:bytes"},
		{Name: "/sched/goroutines:goroutines"},
		{Name: "/gc/cycles/total:gc-cycles"},
	}
	metrics.Read(samples)

	gcCycles := samples[2].Value.Uint64()

	s.meter.Gauge("go.memory.heap", float64(samples[0].Value.Uint64()), sentry.WithUnit(sentry.UnitByte))
	s.meter.Gauge("go.goroutines", float64(samples[1].Value.Uint64()))
	s.meter.Count("go.gc.cycles", int64(gcCycles-s.gcCycles)) //nolint:gosec // The runtime counts cycles in a uint32.

	s.gcCycles = gcCycles
}

// samplePools sends the connections the pools [SamplePool] added hold, summed, and the connections
// they handed out after a wait since the previous sample. It sends nothing before a pool is added.
func (s *sampler) samplePools() {
	poolsMu.Lock()
	added := slices.Clone(pools)
	poolsMu.Unlock()

	if len(added) == 0 {
		return
	}

	var (
		inUse, idle int
		waits       int64
	)

	for _, pool := range added {
		stats := pool.Stats()
		inUse += stats.InUse
		idle += stats.Idle
		waits += stats.WaitCount
	}

	s.meter.Gauge("db.pool.in_use", float64(inUse))
	s.meter.Gauge("db.pool.idle", float64(idle))
	s.meter.Count("db.pool.waits", waits-s.poolWaits)

	s.poolWaits = waits
}

// counterChanges returns how much each counter of current grew since previous. It leaves out a
// counter previous lacks.
func counterChanges(previous, current map[string]uint64) map[string]float64 {
	changes := make(map[string]float64, len(current))

	for name, value := range current {
		before, ok := previous[name]
		if ok {
			changes[name] = float64(value - before)
		}
	}

	return changes
}
