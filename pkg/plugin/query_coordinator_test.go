package plugin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"
	"unsafe"

	"github.com/doitintl/grafana-cloud-intelligence-plugin/pkg/doitapi"
)

func TestQueryCoordinatorCachesSuccessfulResultForSixHours(t *testing.T) {
	now := time.Date(2026, time.July, 25, 0, 0, 0, 0, time.UTC)
	coordinator := newQueryCoordinator(func() time.Time { return now })
	key := makeQueryCacheKey(queryTypeSavedReport, []byte("report-1"), "2026-07-01", "2026-07-25")
	executions := 0

	execute := func(context.Context) (cachedQueryResult, error) {
		executions++
		return cachedQueryResult{name: fmt.Sprintf("result-%d", executions)}, nil
	}

	first, err := coordinator.do(t.Context(), key, execute)
	if err != nil {
		t.Fatalf("first execution: %v", err)
	}

	now = now.Add(queryCacheTTL - time.Nanosecond)
	cached, err := coordinator.do(t.Context(), key, execute)
	if err != nil {
		t.Fatalf("cached execution: %v", err)
	}

	if cached.name != first.name || executions != 1 {
		t.Fatalf("cache hit = %q with %d executions, want %q with 1 execution", cached.name, executions, first.name)
	}

	now = now.Add(time.Nanosecond)
	expired, err := coordinator.do(t.Context(), key, execute)
	if err != nil {
		t.Fatalf("expired execution: %v", err)
	}

	if expired.name == first.name || executions != 2 {
		t.Fatalf("expired result = %q with %d executions, want a fresh second execution", expired.name, executions)
	}
}

func TestQueryCoordinatorDoesNotCacheErrors(t *testing.T) {
	coordinator := newQueryCoordinator(time.Now)
	key := makeQueryCacheKey(queryTypeAdHoc, []byte(`{"metric":"cost"}`), "", "")
	wantErr := errors.New("upstream failure")
	executions := 0

	execute := func(context.Context) (cachedQueryResult, error) {
		executions++
		return cachedQueryResult{}, wantErr
	}

	for range 2 {
		_, err := coordinator.do(t.Context(), key, execute)
		if !errors.Is(err, wantErr) {
			t.Fatalf("execution error = %v, want %v", err, wantErr)
		}
	}

	if executions != 2 {
		t.Fatalf("executions = %d, want 2", executions)
	}
}

func TestCachedQueryResultSizeBytes(t *testing.T) {
	schema := make([]doitapi.SchemaField, 1, 3)
	schema[0] = doitapi.SchemaField{Name: "cost", Type: "float"}

	rows := make([][]json.RawMessage, 1, 2)
	row := make([]json.RawMessage, 2, 4)
	rawBacking := make([]byte, 4, 64)
	copy(rawBacking, `12.3`)
	row[0] = json.RawMessage(rawBacking)
	row[1] = json.RawMessage(`"x"`)
	rows[0] = row

	forecastRows := make([][]json.RawMessage, 1)
	forecastRow := make([]json.RawMessage, 1, 2)
	forecastBacking := make([]byte, 1, 16)
	forecastBacking[0] = '5'
	forecastRow[0] = json.RawMessage(forecastBacking)
	forecastRows[0] = forecastRow

	result := cachedQueryResult{
		name: "q",
		result: doitapi.ReportResult{
			Schema:       schema,
			Rows:         rows,
			ForecastRows: forecastRows,
		},
	}

	want := int64(unsafe.Sizeof(result)) +
		int64(len(result.name)) +
		int64(cap(schema))*int64(unsafe.Sizeof(doitapi.SchemaField{})) +
		int64(len(schema[0].Name)+len(schema[0].Type)) +
		int64(cap(rows))*int64(unsafe.Sizeof([]json.RawMessage(nil))) +
		int64(cap(row))*int64(unsafe.Sizeof(json.RawMessage(nil))) +
		int64(cap(row[0])+cap(row[1])) +
		int64(cap(forecastRows))*int64(unsafe.Sizeof([]json.RawMessage(nil))) +
		int64(cap(forecastRow))*int64(unsafe.Sizeof(json.RawMessage(nil))) +
		int64(cap(forecastRow[0]))

	if got := result.sizeBytes(); got != want {
		t.Fatalf("result size = %d, want %d", got, want)
	}

	rawLength := len(row[0]) + len(row[1]) + len(forecastRow[0])
	if result.sizeBytes() <= int64(rawLength) {
		t.Fatalf("result size = %d, want structural overhead beyond %d raw bytes", result.sizeBytes(), rawLength)
	}
}

func TestQueryCoordinatorBoundsCacheAndEvictsOldestEntry(t *testing.T) {
	now := time.Date(2026, time.July, 25, 0, 0, 0, 0, time.UTC)
	coordinator := newQueryCoordinator(func() time.Time { return now })
	executions := 0

	execute := func(context.Context) (cachedQueryResult, error) {
		executions++
		return cachedQueryResult{name: "result"}, nil
	}

	for i := range queryCacheMaxEntries + 1 {
		key := makeQueryCacheKey(queryTypeSavedReport, []byte(fmt.Sprintf("report-%d", i)), "", "")
		if _, err := coordinator.do(t.Context(), key, execute); err != nil {
			t.Fatalf("populate key %d: %v", i, err)
		}
	}

	if got := len(coordinator.cache); got != queryCacheMaxEntries {
		t.Fatalf("cache entries = %d, want %d", got, queryCacheMaxEntries)
	}

	oldestKey := makeQueryCacheKey(queryTypeSavedReport, []byte("report-0"), "", "")
	if _, err := coordinator.do(t.Context(), oldestKey, execute); err != nil {
		t.Fatalf("reload oldest key: %v", err)
	}

	if want := queryCacheMaxEntries + 2; executions != want {
		t.Fatalf("executions = %d, want %d after oldest entry eviction", executions, want)
	}
}

func TestQueryCoordinatorEvictsOldestEntriesToStayWithinByteLimit(t *testing.T) {
	coordinator := newQueryCoordinator(time.Now)
	coordinator.maxEntries = 10
	result := cachedQueryResult{name: "12345"}
	resultSize := result.sizeBytes()
	coordinator.maxBytes = resultSize * 2
	coordinator.maxResultBytes = resultSize
	executions := 0

	execute := func(context.Context) (cachedQueryResult, error) {
		executions++
		return result, nil
	}

	for i := range 3 {
		key := makeQueryCacheKey(queryTypeSavedReport, []byte(fmt.Sprintf("report-%d", i)), "", "")
		if _, err := coordinator.do(t.Context(), key, execute); err != nil {
			t.Fatalf("populate key %d: %v", i, err)
		}
	}

	if got, want := len(coordinator.cache), 2; got != want {
		t.Fatalf("cache entries = %d, want %d", got, want)
	}
	if got, want := coordinator.cacheBytes, resultSize*2; got != want {
		t.Fatalf("cache bytes = %d, want %d", got, want)
	}

	oldestKey := makeQueryCacheKey(queryTypeSavedReport, []byte("report-0"), "", "")
	if _, err := coordinator.do(t.Context(), oldestKey, execute); err != nil {
		t.Fatalf("reload oldest key: %v", err)
	}

	if executions != 4 {
		t.Fatalf("executions = %d, want 4 after byte eviction", executions)
	}
}

func TestQueryCoordinatorSkipsOversizedResults(t *testing.T) {
	coordinator := newQueryCoordinator(time.Now)
	rawBacking := make([]byte, 1, 1024)
	rawBacking[0] = '1'
	result := cachedQueryResult{
		name: "query",
		result: doitapi.ReportResult{
			Rows: [][]json.RawMessage{{json.RawMessage(rawBacking)}},
		},
	}
	resultSize := result.sizeBytes()
	coordinator.maxBytes = resultSize * 2
	coordinator.maxResultBytes = resultSize - 1
	key := makeQueryCacheKey(queryTypeSavedReport, []byte("report-1"), "", "")
	executions := 0

	execute := func(context.Context) (cachedQueryResult, error) {
		executions++
		return result, nil
	}

	for range 2 {
		if _, err := coordinator.do(t.Context(), key, execute); err != nil {
			t.Fatalf("execute oversized result: %v", err)
		}
	}

	if executions != 2 {
		t.Fatalf("executions = %d, want 2", executions)
	}
	if len(coordinator.cache) != 0 || coordinator.cacheBytes != 0 {
		t.Fatalf("oversized result was cached: entries=%d bytes=%d", len(coordinator.cache), coordinator.cacheBytes)
	}
}

func TestQueryCoordinatorPurgesAllExpiredEntriesOnAccess(t *testing.T) {
	now := time.Date(2026, time.July, 25, 0, 0, 0, 0, time.UTC)
	coordinator := newQueryCoordinator(func() time.Time { return now })
	result := cachedQueryResult{name: "x"}
	execute := func(context.Context) (cachedQueryResult, error) {
		return result, nil
	}

	for i := range 2 {
		key := makeQueryCacheKey(queryTypeSavedReport, []byte(fmt.Sprintf("report-%d", i)), "", "")
		if _, err := coordinator.do(t.Context(), key, execute); err != nil {
			t.Fatalf("populate key %d: %v", i, err)
		}
	}

	now = now.Add(queryCacheTTL)
	freshKey := makeQueryCacheKey(queryTypeSavedReport, []byte("fresh"), "", "")
	if _, err := coordinator.do(t.Context(), freshKey, execute); err != nil {
		t.Fatalf("execute fresh key: %v", err)
	}

	if got, want := len(coordinator.cache), 1; got != want {
		t.Fatalf("cache entries after expiry purge = %d, want %d", got, want)
	}
	if got, want := coordinator.cacheBytes, result.sizeBytes(); got != want {
		t.Fatalf("cache bytes after expiry purge = %d, want %d", got, want)
	}
}

func TestMakeQueryCacheKeyIncludesDataInputs(t *testing.T) {
	base := makeQueryCacheKey(queryTypeSavedReport, []byte("report-1"), "2026-07-01", "2026-07-25")
	keys := []queryCacheKey{
		makeQueryCacheKey(queryTypeAdHoc, []byte("report-1"), "2026-07-01", "2026-07-25"),
		makeQueryCacheKey(queryTypeSavedReport, []byte("report-2"), "2026-07-01", "2026-07-25"),
		makeQueryCacheKey(queryTypeSavedReport, []byte("report-1"), "2026-07-02", "2026-07-25"),
		makeQueryCacheKey(queryTypeSavedReport, []byte("report-1"), "2026-07-01", "2026-07-26"),
	}

	for i, key := range keys {
		if key == base {
			t.Errorf("key %d did not change when a data input changed", i)
		}
	}
}

func TestMakeAdHocQueryCacheKeyCanonicalizesObjects(t *testing.T) {
	first, err := makeAdHocQueryCacheKey(json.RawMessage(`{
		"metric": {"type": "basic", "value": "cost"},
		"filters": [{"type": "service", "values": ["BigQuery"]}]
	}`))
	if err != nil {
		t.Fatalf("first key: %v", err)
	}

	second, err := makeAdHocQueryCacheKey(json.RawMessage(
		`{"filters":[{"values":["BigQuery"],"type":"service"}],"metric":{"value":"cost","type":"basic"}}`,
	))
	if err != nil {
		t.Fatalf("second key: %v", err)
	}

	if first != second {
		t.Fatal("semantically identical ad-hoc configs produced different keys")
	}

	different, err := makeAdHocQueryCacheKey(json.RawMessage(
		`{"filters":[{"values":["Compute Engine"],"type":"service"}],"metric":{"value":"cost","type":"basic"}}`,
	))
	if err != nil {
		t.Fatalf("different key: %v", err)
	}

	if first == different {
		t.Fatal("different ad-hoc configs produced the same key")
	}
}

func TestQueryCoordinatorDeduplicatesInFlightRequests(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		coordinator := newQueryCoordinator(time.Now)
		key := makeQueryCacheKey(queryTypeSavedReport, []byte("report-1"), "", "")
		started := make(chan struct{})
		release := make(chan struct{})
		var executions atomic.Int32

		execute := func(ctx context.Context) (cachedQueryResult, error) {
			executions.Add(1)
			started <- struct{}{}
			<-release
			if err := ctx.Err(); err != nil {
				return cachedQueryResult{}, err
			}

			return cachedQueryResult{name: "shared"}, nil
		}

		const callers = 12
		results := make([]cachedQueryResult, callers)
		errs := make([]error, callers)
		var callersDone sync.WaitGroup

		for i := range callers {
			callersDone.Go(func() {
				results[i], errs[i] = coordinator.do(t.Context(), key, execute)
			})
		}

		<-started
		synctest.Wait()

		if got := executions.Load(); got != 1 {
			t.Fatalf("upstream executions while in flight = %d, want 1", got)
		}

		close(release)
		callersDone.Wait()

		for i := range callers {
			if errs[i] != nil {
				t.Fatalf("caller %d: %v", i, errs[i])
			}
			if results[i].name != "shared" {
				t.Fatalf("caller %d result = %q, want shared", i, results[i].name)
			}
		}
	})
}

func TestQueryCoordinatorLimitsConcurrentExecutions(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		coordinator := newQueryCoordinator(time.Now)
		started := make(chan struct{}, 6)
		release := make(chan struct{})
		var active atomic.Int32
		var maximum atomic.Int32

		execute := func(ctx context.Context) (cachedQueryResult, error) {
			current := active.Add(1)
			for {
				previous := maximum.Load()
				if current <= previous || maximum.CompareAndSwap(previous, current) {
					break
				}
			}

			started <- struct{}{}
			<-release
			active.Add(-1)
			if err := ctx.Err(); err != nil {
				return cachedQueryResult{}, err
			}

			return cachedQueryResult{name: "result"}, nil
		}

		var callersDone sync.WaitGroup
		for i := range 6 {
			key := makeQueryCacheKey(queryTypeSavedReport, []byte(fmt.Sprintf("report-%d", i)), "", "")
			callersDone.Go(func() {
				_, _ = coordinator.do(t.Context(), key, execute)
			})
		}

		synctest.Wait()

		if got := len(started); got != maxConcurrentAPIQueries {
			t.Fatalf("started executions = %d, want %d", got, maxConcurrentAPIQueries)
		}
		if got := len(coordinator.slots); got != maxConcurrentAPIQueries {
			t.Fatalf("occupied slots = %d, want %d", got, maxConcurrentAPIQueries)
		}

		close(release)
		callersDone.Wait()

		if got := maximum.Load(); got != maxConcurrentAPIQueries {
			t.Fatalf("maximum concurrent executions = %d, want %d", got, maxConcurrentAPIQueries)
		}
	})
}

func TestQueryCoordinatorStartsTimeoutAfterExecutionSlotIsAcquired(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		coordinator := newQueryCoordinator(time.Now)
		coordinator.timeout = time.Second
		coordinator.slots <- struct{}{}

		started := make(chan context.Context)
		release := make(chan struct{})
		done := make(chan error, 1)
		key := makeQueryCacheKey(queryTypeSavedReport, []byte("queued-report"), "", "")
		go func() {
			_, err := coordinator.do(t.Context(), key, func(ctx context.Context) (cachedQueryResult, error) {
				started <- ctx
				<-release
				return cachedQueryResult{name: "result"}, nil
			})
			done <- err
		}()

		synctest.Wait()
		time.Sleep(2 * coordinator.timeout)
		<-coordinator.slots

		select {
		case ctx := <-started:
			if err := ctx.Err(); err != nil {
				t.Fatalf("execution context after queue wait = %v, want nil", err)
			}
		case err := <-done:
			t.Fatalf("queued execution completed before acquiring a slot: %v", err)
		}

		close(release)
		if err := <-done; err != nil {
			t.Fatalf("queued execution: %v", err)
		}
	})
}

func TestQueryCoordinatorBoundsPendingExecutions(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		coordinator := newQueryCoordinator(time.Now)
		coordinator.maxPending = 1
		started := make(chan struct{})
		release := make(chan struct{})

		firstDone := make(chan error, 1)
		go func() {
			key := makeQueryCacheKey(queryTypeSavedReport, []byte("report-1"), "", "")
			_, err := coordinator.do(t.Context(), key, func(context.Context) (cachedQueryResult, error) {
				started <- struct{}{}
				<-release
				return cachedQueryResult{name: "result"}, nil
			})
			firstDone <- err
		}()

		<-started

		secondKey := makeQueryCacheKey(queryTypeSavedReport, []byte("report-2"), "", "")
		_, err := coordinator.do(t.Context(), secondKey, func(context.Context) (cachedQueryResult, error) {
			return cachedQueryResult{name: "unexpected"}, nil
		})
		if !errors.Is(err, errQueryQueueFull) {
			t.Fatalf("second execution error = %v, want queue full", err)
		}

		close(release)
		if err := <-firstDone; err != nil {
			t.Fatalf("first execution: %v", err)
		}
	})
}

func TestQueryCoordinatorDetachesSharedExecutionFromCallerCancellation(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		coordinator := newQueryCoordinator(time.Now)
		key := makeQueryCacheKey(queryTypeSavedReport, []byte("report-1"), "", "")
		started := make(chan context.Context)
		release := make(chan struct{})

		execute := func(ctx context.Context) (cachedQueryResult, error) {
			started <- ctx
			<-release
			if err := ctx.Err(); err != nil {
				return cachedQueryResult{}, err
			}

			return cachedQueryResult{name: "shared"}, nil
		}

		type contextKey string
		leaderContext, cancelLeader := context.WithCancel(context.WithValue(t.Context(), contextKey("request"), "leader"))
		defer cancelLeader()

		leaderDone := make(chan error, 1)
		go func() {
			_, err := coordinator.do(leaderContext, key, execute)
			leaderDone <- err
		}()

		upstreamContext := <-started
		if got := upstreamContext.Value(contextKey("request")); got != "leader" {
			t.Fatalf("upstream context value = %v, want leader", got)
		}

		followerDone := make(chan error, 1)
		go func() {
			_, err := coordinator.do(t.Context(), key, execute)
			followerDone <- err
		}()
		synctest.Wait()

		if got := inFlightWaiters(coordinator, key); got != 2 {
			t.Fatalf("waiters before cancellation = %d, want 2", got)
		}

		cancelLeader()
		synctest.Wait()

		if err := <-leaderDone; !errors.Is(err, context.Canceled) {
			t.Fatalf("leader error = %v, want context canceled", err)
		}
		if err := upstreamContext.Err(); err != nil {
			t.Fatalf("shared upstream context canceled with leader: %v", err)
		}
		if got := inFlightWaiters(coordinator, key); got != 1 {
			t.Fatalf("waiters after leader cancellation = %d, want 1", got)
		}

		close(release)
		if err := <-followerDone; err != nil {
			t.Fatalf("follower error: %v", err)
		}
	})
}

func TestQueryCoordinatorCancelsExecutionWhenFinalWaiterLeaves(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		coordinator := newQueryCoordinator(time.Now)
		key := makeQueryCacheKey(queryTypeSavedReport, []byte("report-1"), "", "")
		started := make(chan context.Context)
		var cancellations atomic.Int32

		execute := func(ctx context.Context) (cachedQueryResult, error) {
			started <- ctx
			<-ctx.Done()
			return cachedQueryResult{name: "canceled"}, ctx.Err()
		}

		firstContext, cancelFirst := context.WithCancel(t.Context())
		defer cancelFirst()
		secondContext, cancelSecond := context.WithCancel(t.Context())
		defer cancelSecond()

		firstDone := make(chan error, 1)
		go func() {
			_, err := coordinator.do(firstContext, key, execute)
			firstDone <- err
		}()

		upstreamContext := <-started

		secondDone := make(chan error, 1)
		go func() {
			_, err := coordinator.do(secondContext, key, execute)
			secondDone <- err
		}()
		synctest.Wait()

		coordinator.mu.Lock()
		call := coordinator.inFlight[key]
		originalCancel := call.cancel
		call.cancel = func() {
			cancellations.Add(1)
			originalCancel()
		}
		coordinator.mu.Unlock()

		cancelFirst()
		synctest.Wait()

		if err := upstreamContext.Err(); err != nil {
			t.Fatalf("upstream canceled with one waiter remaining: %v", err)
		}

		cancelSecond()
		synctest.Wait()

		if err := <-firstDone; !errors.Is(err, context.Canceled) {
			t.Fatalf("first waiter error = %v, want context canceled", err)
		}
		if err := <-secondDone; !errors.Is(err, context.Canceled) {
			t.Fatalf("second waiter error = %v, want context canceled", err)
		}
		if !errors.Is(upstreamContext.Err(), context.Canceled) {
			t.Fatalf("upstream error = %v, want context canceled", upstreamContext.Err())
		}
		if got := cancellations.Load(); got != 1 {
			t.Fatalf("shared cancellation calls = %d, want 1", got)
		}
		if got := inFlightWaiters(coordinator, key); got != 0 {
			t.Fatalf("waiters after final cancellation = %d, want 0", got)
		}
	})
}

func TestQueryCoordinatorCloseCancelsWorkAndClearsState(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		coordinator := newQueryCoordinator(time.Now)
		cachedKey := makeQueryCacheKey(queryTypeSavedReport, []byte("cached"), "", "")
		if _, err := coordinator.do(t.Context(), cachedKey, func(context.Context) (cachedQueryResult, error) {
			return cachedQueryResult{name: "cached"}, nil
		}); err != nil {
			t.Fatalf("populate cache: %v", err)
		}

		started := make(chan struct{}, 4)
		execute := func(ctx context.Context) (cachedQueryResult, error) {
			started <- struct{}{}
			<-ctx.Done()
			return cachedQueryResult{name: "canceled"}, ctx.Err()
		}

		const executions = 4
		errs := make([]error, executions)
		var callersDone sync.WaitGroup
		for i := range executions {
			key := makeQueryCacheKey(queryTypeSavedReport, []byte(fmt.Sprintf("running-%d", i)), "", "")
			callersDone.Go(func() {
				_, errs[i] = coordinator.do(t.Context(), key, execute)
			})
		}

		synctest.Wait()

		if got := len(started); got != maxConcurrentAPIQueries {
			t.Fatalf("started executions before close = %d, want %d", got, maxConcurrentAPIQueries)
		}

		coordinator.Close()
		callersDone.Wait()
		coordinator.Close()

		if got := len(started); got != maxConcurrentAPIQueries {
			t.Fatalf("executions invoked after close = %d, want %d total", got, maxConcurrentAPIQueries)
		}

		for i, err := range errs {
			if !errors.Is(err, errCoordinatorClosed) {
				t.Errorf("caller %d error = %v, want coordinator closed", i, err)
			}
		}

		coordinator.mu.Lock()
		defer coordinator.mu.Unlock()

		if !coordinator.closed {
			t.Error("coordinator is not marked closed")
		}
		if len(coordinator.inFlight) != 0 {
			t.Errorf("in-flight entries after close = %d, want 0", len(coordinator.inFlight))
		}
		if len(coordinator.cache) != 0 || coordinator.cacheBytes != 0 {
			t.Errorf("cache after close: entries=%d bytes=%d", len(coordinator.cache), coordinator.cacheBytes)
		}
		if got := len(coordinator.slots); got != 0 {
			t.Errorf("occupied slots after close = %d, want 0", got)
		}
	})
}

func inFlightWaiters(coordinator *queryCoordinator, key queryCacheKey) int {
	coordinator.mu.Lock()
	defer coordinator.mu.Unlock()

	call, ok := coordinator.inFlight[key]
	if !ok {
		return 0
	}

	return call.waiters
}
