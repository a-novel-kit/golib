package sentryf

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/getsentry/sentry-go"
	"github.com/stretchr/testify/require"
)

// testDSN names a project on a reserved host, which no test contacts.
const testDSN = "https://public@sentry.invalid/1"

// fakeConnector opens connections that serve no statement, so a test can hold one from a pool.
type fakeConnector struct{}

func (fakeConnector) Connect(context.Context) (driver.Conn, error) { return fakeConn{}, nil }
func (connector fakeConnector) Driver() driver.Driver              { return connector }
func (fakeConnector) Open(string) (driver.Conn, error)             { return fakeConn{}, nil }

// fakeConn is the connection fakeConnector opens.
type fakeConn struct{}

func (fakeConn) Prepare(string) (driver.Stmt, error) { return nil, errors.ErrUnsupported }
func (fakeConn) Close() error                        { return nil }
func (fakeConn) Begin() (driver.Tx, error)           { return nil, errors.ErrUnsupported }

// sentMetric is the value and the unit of a metric a test client received.
type sentMetric struct {
	value any
	unit  string
}

// metricRecorder holds a meter whose client keeps the metrics it sends in memory.
type metricRecorder struct {
	meter     sentry.Meter
	client    *sentry.Client
	transport *sentry.MockTransport
}

// newMetricRecorder returns a recorder whose client closes once t ends.
func newMetricRecorder(t *testing.T) *metricRecorder {
	t.Helper()

	transport := &sentry.MockTransport{}

	client, err := sentry.NewClient(sentry.ClientOptions{Dsn: testDSN, Transport: transport})
	if err != nil {
		panic(err)
	}

	t.Cleanup(client.Close)

	ctx := sentry.SetHubOnContext(t.Context(), sentry.NewHub(client, sentry.NewScope()))

	return &metricRecorder{meter: sentry.NewMeter(ctx), client: client, transport: transport}
}

// sent returns every metric the meter sent so far.
func (recorder *metricRecorder) sent() []sentry.Metric {
	recorder.client.Flush(time.Second)

	var metrics []sentry.Metric

	for _, event := range recorder.transport.Events() {
		metrics = append(metrics, event.Metrics...)
	}

	return metrics
}

// metricsNamed returns the last of metrics for each name that starts with prefix.
func metricsNamed(metrics []sentry.Metric, prefix string) map[string]sentMetric {
	named := map[string]sentMetric{}

	for _, metric := range metrics {
		if strings.HasPrefix(metric.Name, prefix) {
			named[metric.Name] = sentMetric{value: metric.Value.AsInterface(), unit: metric.Unit}
		}
	}

	return named
}

func TestSampler(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name string

		// start names the cgroup fixture the sampler reads as it starts, and end the one it reads a
		// minute later, as it samples.
		start string
		end   string

		expect map[string]sentMetric
	}{
		{
			name:  "Success/Limited",
			start: "limited",
			end:   "limited-later",
			expect: map[string]sentMetric{
				"container.memory.current": {value: 62914560.0, unit: sentry.UnitByte},
				"container.memory.limit":   {value: 268435456.0, unit: sentry.UnitByte},
				"container.cpu.usage":      {value: 0.5, unit: sentry.UnitRatio},
				"container.cpu.throttled":  {value: 0.25, unit: sentry.UnitRatio},
			},
		},
		{
			name:  "Success/Unlimited",
			start: "unlimited",
			end:   "unlimited",
			expect: map[string]sentMetric{
				"container.memory.current": {value: 41943040.0, unit: sentry.UnitByte},
				"container.cpu.usage":      {value: 0.0, unit: sentry.UnitRatio},
			},
		},
		{
			name:   "Success/CgroupV1",
			start:  "v1",
			end:    "v1",
			expect: map[string]sentMetric{},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			recorder := newMetricRecorder(t)

			sampler := newSampler(recorder.meter, cgroupFixture(testCase.start))
			sampler.cgroup = cgroupFixture(testCase.end)
			sampler.sample(sampler.at.Add(time.Minute))

			require.Equal(t, testCase.expect, metricsNamed(recorder.sent(), "container."))
		})
	}

	t.Run("Success/Runtime", func(t *testing.T) {
		t.Parallel()

		recorder := newMetricRecorder(t)

		runtime.GC()
		newSampler(recorder.meter, cgroupFixture("v1")).sample(time.Now())

		goMetrics := metricsNamed(recorder.sent(), "go.")
		require.Len(t, goMetrics, 3)
		require.Equal(t, sentry.UnitByte, goMetrics["go.memory.heap"].unit)
		require.Positive(t, goMetrics["go.memory.heap"].value)
		require.Positive(t, goMetrics["go.goroutines"].value)
		require.Positive(t, goMetrics["go.gc.cycles"].value)
		require.IsType(t, int64(0), goMetrics["go.gc.cycles"].value)
	})

	t.Run("Success/Pools", func(t *testing.T) {
		t.Parallel()

		pool := sql.OpenDB(fakeConnector{})

		t.Cleanup(func() { _ = pool.Close() })

		conn, err := pool.Conn(t.Context())
		require.NoError(t, err)
		t.Cleanup(func() { _ = conn.Close() })

		SamplePool(pool)

		recorder := newMetricRecorder(t)
		newSampler(recorder.meter, cgroupFixture("v1")).sample(time.Now())

		require.Equal(t, map[string]sentMetric{
			"db.pool.in_use": {value: 1.0},
			"db.pool.idle":   {value: 0.0},
			"db.pool.waits":  {value: int64(0)},
		}, metricsNamed(recorder.sent(), "db.pool."))
	})

	t.Run("Success/Stops", func(t *testing.T) {
		t.Parallel()

		recorder := newMetricRecorder(t)
		stop := startSampler(recorder.meter, cgroupFixture("v1"), time.Millisecond)

		require.Eventually(t, func() bool { return len(recorder.sent()) > 0 }, time.Second, time.Millisecond)

		stop()

		sent := len(recorder.sent())

		time.Sleep(10 * time.Millisecond)
		require.Len(t, recorder.sent(), sent)
	})
}
