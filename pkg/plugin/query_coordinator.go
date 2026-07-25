package plugin

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"hash"
	"io"
	"math"
	"sync"
	"time"
	"unsafe"

	"github.com/doitintl/grafana-cloud-intelligence-plugin/pkg/doitapi"
)

const (
	queryCacheTTL                  = 6 * time.Hour
	queryCacheMaxEntries           = 256
	queryCacheMaxBytes       int64 = 256 << 20
	queryCacheMaxResultBytes int64 = 32 << 20
	maxConcurrentAPIQueries        = 1
	maxPendingAPIQueries           = 256
)

var (
	errQueryQueueFull    = errors.New("report query queue is full")
	errCoordinatorClosed = errors.New("query coordinator is closed")
)

type queryCacheKey [sha256.Size]byte

type cachedQueryResult struct {
	name   string
	result doitapi.ReportResult
}

func (r cachedQueryResult) sizeBytes() int64 {
	size := uint64(unsafe.Sizeof(r))
	addBytes := func(bytes uint64) {
		if math.MaxUint64-size < bytes {
			size = math.MaxUint64
			return
		}

		size += bytes
	}
	addAllocation := func(capacity int, elementSize uintptr) {
		capacityBytes := uint64(capacity)
		elementBytes := uint64(elementSize)
		if capacityBytes != 0 && elementBytes > math.MaxUint64/capacityBytes {
			size = math.MaxUint64
			return
		}

		addBytes(capacityBytes * elementBytes)
	}

	addBytes(uint64(len(r.name)))
	addAllocation(cap(r.result.Schema), unsafe.Sizeof(doitapi.SchemaField{}))

	for _, field := range r.result.Schema {
		addBytes(uint64(len(field.Name) + len(field.Type)))
	}

	for _, rows := range [][][]json.RawMessage{r.result.Rows, r.result.ForecastRows} {
		addAllocation(cap(rows), unsafe.Sizeof([]json.RawMessage(nil)))

		for _, row := range rows {
			addAllocation(cap(row), unsafe.Sizeof(json.RawMessage(nil)))

			for _, value := range row {
				addBytes(uint64(cap(value)))
			}
		}
	}

	if size > math.MaxInt64 {
		return math.MaxInt64
	}

	return int64(size)
}

type queryCacheEntry struct {
	result    cachedQueryResult
	expiresAt time.Time
	sequence  uint64
	size      int64
}

type inFlightQuery struct {
	done      chan struct{}
	result    cachedQueryResult
	err       error
	waiters   int
	cancel    context.CancelFunc
	completed bool
}

type queryCoordinator struct {
	mu             sync.Mutex
	cache          map[queryCacheKey]queryCacheEntry
	cacheBytes     int64
	inFlight       map[queryCacheKey]*inFlightQuery
	slots          chan struct{}
	executions     sync.WaitGroup
	now            func() time.Time
	sequence       uint64
	timeout        time.Duration
	cacheTTL       time.Duration
	maxEntries     int
	maxBytes       int64
	maxResultBytes int64
	maxPending     int
	closed         bool
}

func newQueryCoordinator(now func() time.Time) *queryCoordinator {
	if now == nil {
		now = time.Now
	}

	return &queryCoordinator{
		cache:          make(map[queryCacheKey]queryCacheEntry),
		inFlight:       make(map[queryCacheKey]*inFlightQuery),
		slots:          make(chan struct{}, maxConcurrentAPIQueries),
		now:            now,
		timeout:        reportQueryTimeout,
		cacheTTL:       queryCacheTTL,
		maxEntries:     queryCacheMaxEntries,
		maxBytes:       queryCacheMaxBytes,
		maxResultBytes: queryCacheMaxResultBytes,
		maxPending:     maxPendingAPIQueries,
	}
}

func makeQueryCacheKey(queryType string, payload []byte, startDate, endDate string) queryCacheKey {
	hasher := sha256.New()
	writeHashPart(hasher, []byte(queryType))
	writeHashPart(hasher, payload)
	writeHashPart(hasher, []byte(startDate))
	writeHashPart(hasher, []byte(endDate))

	var key queryCacheKey
	copy(key[:], hasher.Sum(nil))

	return key
}

func makeAdHocQueryCacheKey(config json.RawMessage) (queryCacheKey, error) {
	decoder := json.NewDecoder(bytes.NewReader(config))
	decoder.UseNumber()

	var value any
	if err := decoder.Decode(&value); err != nil {
		return queryCacheKey{}, fmt.Errorf("decode query config: %w", err)
	}

	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return queryCacheKey{}, errors.New("decode query config: unexpected trailing data")
	}

	canonical, err := json.Marshal(value)
	if err != nil {
		return queryCacheKey{}, fmt.Errorf("encode canonical query config: %w", err)
	}

	return makeQueryCacheKey(queryTypeAdHoc, canonical, "", ""), nil
}

func writeHashPart(writer hash.Hash, value []byte) {
	var length [8]byte
	binary.BigEndian.PutUint64(length[:], uint64(len(value)))
	_, _ = writer.Write(length[:])
	_, _ = writer.Write(value)
}

func (c *queryCoordinator) do(
	ctx context.Context,
	key queryCacheKey,
	execute func(context.Context) (cachedQueryResult, error),
) (cachedQueryResult, error) {
	c.mu.Lock()
	c.purgeExpiredLocked(c.now())

	if c.closed {
		c.mu.Unlock()
		return cachedQueryResult{}, errCoordinatorClosed
	}

	if entry, ok := c.cache[key]; ok {
		c.mu.Unlock()
		return entry.result, nil
	}

	call, ok := c.inFlight[key]
	if ok {
		call.waiters++
	} else {
		if err := ctx.Err(); err != nil {
			c.mu.Unlock()
			return cachedQueryResult{}, err
		}
		if len(c.inFlight) >= c.maxPending {
			c.mu.Unlock()
			return cachedQueryResult{}, errQueryQueueFull
		}

		baseContext, cancel := context.WithCancel(context.WithoutCancel(ctx))
		call = &inFlightQuery{
			done:    make(chan struct{}),
			waiters: 1,
			cancel:  cancel,
		}
		c.inFlight[key] = call
		c.executions.Go(func() {
			c.execute(key, call, baseContext, execute)
		})
	}

	c.mu.Unlock()

	select {
	case <-call.done:
		return call.result, call.err
	case <-ctx.Done():
		c.removeWaiter(key, call)
		return cachedQueryResult{}, ctx.Err()
	}
}

func (c *queryCoordinator) removeWaiter(key queryCacheKey, call *inFlightQuery) {
	c.mu.Lock()
	defer c.mu.Unlock()

	current, ok := c.inFlight[key]
	if !ok || current != call || call.completed || call.waiters == 0 {
		return
	}

	call.waiters--
	if call.waiters == 0 {
		delete(c.inFlight, key)
		c.cancelCallLocked(call)
	}
}

func (c *queryCoordinator) execute(
	key queryCacheKey,
	call *inFlightQuery,
	baseContext context.Context,
	execute func(context.Context) (cachedQueryResult, error),
) {
	var result cachedQueryResult
	var err error
	defer func() {
		if recovered := recover(); recovered != nil {
			err = fmt.Errorf("query execution panic: %v", recovered)
		}

		c.finishExecution(key, call, result, err)
	}()

	select {
	case c.slots <- struct{}{}:
		func() {
			defer func() { <-c.slots }()
			ctx, cancel := context.WithTimeout(baseContext, c.timeout)
			defer cancel()

			if contextErr := ctx.Err(); contextErr != nil {
				err = contextErr
				return
			}
			if !c.executionAllowed(key, call) {
				err = errCoordinatorClosed
				return
			}

			result, err = execute(ctx)
		}()
	case <-baseContext.Done():
		err = baseContext.Err()
	}
}

func (c *queryCoordinator) executionAllowed(key queryCacheKey, call *inFlightQuery) bool {
	c.mu.Lock()
	defer c.mu.Unlock()

	current, ok := c.inFlight[key]

	return ok && current == call && !call.completed && !c.closed
}

func (c *queryCoordinator) finishExecution(
	key queryCacheKey,
	call *inFlightQuery,
	result cachedQueryResult,
	err error,
) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.cancelCallLocked(call)

	if current, ok := c.inFlight[key]; ok && current == call {
		delete(c.inFlight, key)
	}

	if call.completed {
		return
	}

	call.result = result
	call.err = err
	call.completed = true

	if !c.closed && err == nil {
		c.storeLocked(key, result)
	}

	close(call.done)
}

func (c *queryCoordinator) cancelCallLocked(call *inFlightQuery) {
	if call.cancel != nil {
		call.cancel()
		call.cancel = nil
	}
}

func (c *queryCoordinator) storeLocked(key queryCacheKey, result cachedQueryResult) {
	now := c.now()
	c.purgeExpiredLocked(now)

	size := result.sizeBytes()
	if c.maxEntries <= 0 || c.maxBytes <= 0 || size > c.maxResultBytes || size > c.maxBytes {
		return
	}

	if _, exists := c.cache[key]; exists {
		c.deleteCacheEntryLocked(key)
	}

	for len(c.cache) >= c.maxEntries || c.cacheBytes+size > c.maxBytes {
		if !c.evictOldestLocked() {
			return
		}
	}

	c.sequence++
	c.cache[key] = queryCacheEntry{
		result:    result,
		expiresAt: now.Add(c.cacheTTL),
		sequence:  c.sequence,
		size:      size,
	}
	c.cacheBytes += size
}

func (c *queryCoordinator) purgeExpiredLocked(now time.Time) {
	for key, entry := range c.cache {
		if !now.Before(entry.expiresAt) {
			c.deleteCacheEntryLocked(key)
		}
	}
}

func (c *queryCoordinator) deleteCacheEntryLocked(key queryCacheKey) {
	entry, ok := c.cache[key]
	if !ok {
		return
	}

	delete(c.cache, key)
	c.cacheBytes -= entry.size
}

func (c *queryCoordinator) evictOldestLocked() bool {
	var oldestKey queryCacheKey
	var oldestSequence uint64
	found := false

	for key, entry := range c.cache {
		if !found || entry.sequence < oldestSequence {
			oldestKey = key
			oldestSequence = entry.sequence
			found = true
		}
	}

	if found {
		c.deleteCacheEntryLocked(oldestKey)
	}

	return found
}

func (c *queryCoordinator) Close() {
	c.mu.Lock()

	if !c.closed {
		c.closed = true

		for key, call := range c.inFlight {
			c.cancelCallLocked(call)

			if !call.completed {
				call.err = errCoordinatorClosed
				call.completed = true
				close(call.done)
			}

			delete(c.inFlight, key)
		}

		clear(c.cache)
		c.cacheBytes = 0
		c.sequence = 0
	}

	c.mu.Unlock()
	c.executions.Wait()
}
