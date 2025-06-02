// Package server provides concurrent request handling optimizations for MCP
package server

import (
	"context"
	"runtime"
	"sync"
	"sync/atomic"
	"time"

	"github.com/LerianStudio/gomcp-sdk/protocol"
)

// ConcurrentHandler provides optimized concurrent request handling
type ConcurrentHandler struct {
	// Worker pool configuration
	numWorkers   int
	maxQueueSize int

	// Request channels
	requestChan  chan *requestWork
	responseChan chan *responseWork

	// Worker management
	workers    []*worker
	workerPool sync.Pool
	wg         sync.WaitGroup

	// Metrics
	activeRequests   int64
	totalRequests    int64
	rejectedRequests int64

	// Shutdown
	ctx    context.Context
	cancel context.CancelFunc

	// Request handler
	handler RequestHandler
}

// RequestHandler defines the interface for handling requests
type RequestHandler interface {
	HandleRequest(ctx context.Context, req *protocol.JSONRPCRequest) (*protocol.JSONRPCResponse, error)
}

// requestWork represents a unit of work
type requestWork struct {
	ctx      context.Context
	request  *protocol.JSONRPCRequest
	response chan<- *responseWork
}

// responseWork represents a response
type responseWork struct {
	response *protocol.JSONRPCResponse
	err      error
}

// worker represents a worker goroutine
type worker struct {
	id         int
	handler    *ConcurrentHandler
	activeWork atomic.Value
}

// ConcurrentOptions configures the concurrent handler
type ConcurrentOptions struct {
	NumWorkers   int
	MaxQueueSize int
}

// DefaultConcurrentOptions returns default options
func DefaultConcurrentOptions() *ConcurrentOptions {
	return &ConcurrentOptions{
		NumWorkers:   runtime.NumCPU() * 2,
		MaxQueueSize: 1000,
	}
}

// NewConcurrentHandler creates a new concurrent handler
func NewConcurrentHandler(handler RequestHandler, opts *ConcurrentOptions) *ConcurrentHandler {
	if opts == nil {
		opts = DefaultConcurrentOptions()
	}

	ctx, cancel := context.WithCancel(context.Background())

	ch := &ConcurrentHandler{
		numWorkers:   opts.NumWorkers,
		maxQueueSize: opts.MaxQueueSize,
		requestChan:  make(chan *requestWork, opts.MaxQueueSize),
		responseChan: make(chan *responseWork, opts.MaxQueueSize),
		workers:      make([]*worker, opts.NumWorkers),
		handler:      handler,
		ctx:          ctx,
		cancel:       cancel,
	}

	// Initialize worker pool
	ch.workerPool = sync.Pool{
		New: func() interface{} {
			return &worker{
				handler: ch,
			}
		},
	}

	// Start workers
	for i := 0; i < ch.numWorkers; i++ {
		w := &worker{
			id:      i,
			handler: ch,
		}
		ch.workers[i] = w
		ch.wg.Add(1)
		go w.run()
	}

	return ch
}

// HandleRequest processes a request concurrently
func (ch *ConcurrentHandler) HandleRequest(ctx context.Context, req *protocol.JSONRPCRequest) (*protocol.JSONRPCResponse, error) {
	// Increment metrics
	atomic.AddInt64(&ch.totalRequests, 1)

	// Check if we're at capacity
	if atomic.LoadInt64(&ch.activeRequests) >= int64(ch.maxQueueSize) {
		atomic.AddInt64(&ch.rejectedRequests, 1)
		return nil, protocol.NewJSONRPCError(protocol.InternalError, "server at capacity", nil)
	}

	// Increment active requests
	atomic.AddInt64(&ch.activeRequests, 1)
	defer atomic.AddInt64(&ch.activeRequests, -1)

	// Create response channel
	respChan := make(chan *responseWork, 1)

	// Create work item
	work := &requestWork{
		ctx:      ctx,
		request:  req,
		response: respChan,
	}

	// Submit work
	select {
	case ch.requestChan <- work:
		// Work submitted
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-ch.ctx.Done():
		return nil, context.Canceled
	}

	// Wait for response
	select {
	case resp := <-respChan:
		return resp.response, resp.err
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-ch.ctx.Done():
		return nil, context.Canceled
	}
}

// Shutdown gracefully shuts down the concurrent handler
func (ch *ConcurrentHandler) Shutdown(timeout time.Duration) error {
	// Signal shutdown
	ch.cancel()

	// Wait for workers with timeout
	done := make(chan struct{})
	go func() {
		ch.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		return nil
	case <-time.After(timeout):
		return context.DeadlineExceeded
	}
}

// Metrics returns current metrics
func (ch *ConcurrentHandler) Metrics() map[string]int64 {
	return map[string]int64{
		"active_requests":   atomic.LoadInt64(&ch.activeRequests),
		"total_requests":    atomic.LoadInt64(&ch.totalRequests),
		"rejected_requests": atomic.LoadInt64(&ch.rejectedRequests),
		"num_workers":       int64(ch.numWorkers),
		"max_queue_size":    int64(ch.maxQueueSize),
	}
}

// run is the worker loop
func (w *worker) run() {
	defer w.handler.wg.Done()

	for {
		select {
		case work := <-w.handler.requestChan:
			w.processWork(work)
		case <-w.handler.ctx.Done():
			return
		}
	}
}

// processWork handles a single work item
func (w *worker) processWork(work *requestWork) {
	// Store active work for monitoring
	w.activeWork.Store(work)
	defer w.activeWork.Store(nil)

	// Handle the request
	resp, err := w.handler.handler.HandleRequest(work.ctx, work.request)

	// Send response
	select {
	case work.response <- &responseWork{response: resp, err: err}:
		// Response sent
	case <-work.ctx.Done():
		// Context cancelled
	case <-w.handler.ctx.Done():
		// Handler shutting down
	}
}

// BatchProcessor handles multiple requests in batches for improved throughput
type BatchProcessor struct {
	handler      RequestHandler
	batchSize    int
	batchTimeout time.Duration

	mu       sync.Mutex
	batch    []*batchItem
	timer    *time.Timer
	resultCh chan *batchResult
}

// batchItem represents an item in a batch
type batchItem struct {
	ctx      context.Context
	request  *protocol.JSONRPCRequest
	resultCh chan<- *batchResult
}

// batchResult represents a batch result
type batchResult struct {
	response *protocol.JSONRPCResponse
	err      error
}

// BatchOptions configures batch processing
type BatchOptions struct {
	BatchSize    int
	BatchTimeout time.Duration
}

// DefaultBatchOptions returns default batch options
func DefaultBatchOptions() *BatchOptions {
	return &BatchOptions{
		BatchSize:    50,
		BatchTimeout: 10 * time.Millisecond,
	}
}

// NewBatchProcessor creates a new batch processor
func NewBatchProcessor(handler RequestHandler, opts *BatchOptions) *BatchProcessor {
	if opts == nil {
		opts = DefaultBatchOptions()
	}

	bp := &BatchProcessor{
		handler:      handler,
		batchSize:    opts.BatchSize,
		batchTimeout: opts.BatchTimeout,
		batch:        make([]*batchItem, 0, opts.BatchSize),
		resultCh:     make(chan *batchResult, opts.BatchSize),
	}

	return bp
}

// HandleRequest adds a request to the batch
func (bp *BatchProcessor) HandleRequest(ctx context.Context, req *protocol.JSONRPCRequest) (*protocol.JSONRPCResponse, error) {
	resultCh := make(chan *batchResult, 1)

	bp.mu.Lock()

	// Add to batch
	bp.batch = append(bp.batch, &batchItem{
		ctx:      ctx,
		request:  req,
		resultCh: resultCh,
	})

	// Check if batch is full
	if len(bp.batch) >= bp.batchSize {
		bp.processBatchLocked()
	} else if bp.timer == nil {
		// Start timer for batch timeout
		bp.timer = time.AfterFunc(bp.batchTimeout, func() {
			bp.mu.Lock()
			defer bp.mu.Unlock()
			bp.processBatchLocked()
		})
	}

	bp.mu.Unlock()

	// Wait for result
	select {
	case result := <-resultCh:
		return result.response, result.err
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// processBatchLocked processes the current batch
func (bp *BatchProcessor) processBatchLocked() {
	if len(bp.batch) == 0 {
		return
	}

	// Stop timer if running
	if bp.timer != nil {
		bp.timer.Stop()
		bp.timer = nil
	}

	// Process batch in parallel
	batch := bp.batch
	bp.batch = make([]*batchItem, 0, bp.batchSize)

	// Process each item in the batch concurrently
	var wg sync.WaitGroup
	for _, item := range batch {
		wg.Add(1)
		go func(item *batchItem) {
			defer wg.Done()

			resp, err := bp.handler.HandleRequest(item.ctx, item.request)

			select {
			case item.resultCh <- &batchResult{response: resp, err: err}:
				// Result sent
			case <-item.ctx.Done():
				// Context cancelled
			}
		}(item)
	}

	// Wait for all items to complete
	wg.Wait()
}

// PipelineHandler provides request pipelining for improved latency
type PipelineHandler struct {
	handler  RequestHandler
	pipeline chan *pipelineWork
	workers  int
	wg       sync.WaitGroup
	ctx      context.Context
	cancel   context.CancelFunc
}

// pipelineWork represents work in the pipeline
type pipelineWork struct {
	ctx      context.Context
	request  *protocol.JSONRPCRequest
	resultCh chan<- *pipelineResult
}

// pipelineResult represents a pipeline result
type pipelineResult struct {
	response *protocol.JSONRPCResponse
	err      error
}

// NewPipelineHandler creates a new pipeline handler
func NewPipelineHandler(handler RequestHandler, workers int) *PipelineHandler {
	ctx, cancel := context.WithCancel(context.Background())

	ph := &PipelineHandler{
		handler:  handler,
		pipeline: make(chan *pipelineWork, workers*2),
		workers:  workers,
		ctx:      ctx,
		cancel:   cancel,
	}

	// Start pipeline workers
	for i := 0; i < workers; i++ {
		ph.wg.Add(1)
		go ph.pipelineWorker()
	}

	return ph
}

// HandleRequest processes a request through the pipeline
func (ph *PipelineHandler) HandleRequest(ctx context.Context, req *protocol.JSONRPCRequest) (*protocol.JSONRPCResponse, error) {
	resultCh := make(chan *pipelineResult, 1)

	work := &pipelineWork{
		ctx:      ctx,
		request:  req,
		resultCh: resultCh,
	}

	select {
	case ph.pipeline <- work:
		// Work submitted
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-ph.ctx.Done():
		return nil, context.Canceled
	}

	select {
	case result := <-resultCh:
		return result.response, result.err
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-ph.ctx.Done():
		return nil, context.Canceled
	}
}

// pipelineWorker processes work from the pipeline
func (ph *PipelineHandler) pipelineWorker() {
	defer ph.wg.Done()

	for {
		select {
		case work := <-ph.pipeline:
			resp, err := ph.handler.HandleRequest(work.ctx, work.request)

			select {
			case work.resultCh <- &pipelineResult{response: resp, err: err}:
				// Result sent
			case <-work.ctx.Done():
				// Context cancelled
			}
		case <-ph.ctx.Done():
			return
		}
	}
}

// Shutdown gracefully shuts down the pipeline handler
func (ph *PipelineHandler) Shutdown() {
	ph.cancel()
	ph.wg.Wait()
}

// AdaptiveWorkerPool provides dynamic worker pool scaling based on load
type AdaptiveWorkerPool struct {
	*ConcurrentHandler

	// Scaling configuration
	minWorkers         int
	maxWorkers         int
	scaleUpThreshold   float64 // Scale up when utilization > threshold
	scaleDownThreshold float64 // Scale down when utilization < threshold
	scalingInterval    time.Duration

	// Current state
	currentWorkers int64
	lastScaleTime  time.Time

	// Metrics for scaling decisions
	utilizationHistory []float64
	historySize        int

	// Control
	scalingMu     sync.RWMutex
	scalingTicker *time.Ticker
	scalingCtx    context.Context
	scalingCancel context.CancelFunc
}

// AdaptiveOptions configures the adaptive worker pool
type AdaptiveOptions struct {
	*ConcurrentOptions
	MinWorkers         int
	MaxWorkers         int
	ScaleUpThreshold   float64
	ScaleDownThreshold float64
	ScalingInterval    time.Duration
}

// DefaultAdaptiveOptions returns default adaptive options
func DefaultAdaptiveOptions() *AdaptiveOptions {
	return &AdaptiveOptions{
		ConcurrentOptions:  DefaultConcurrentOptions(),
		MinWorkers:         runtime.NumCPU(),
		MaxWorkers:         runtime.NumCPU() * 8,
		ScaleUpThreshold:   0.8, // Scale up when 80% utilized
		ScaleDownThreshold: 0.3, // Scale down when < 30% utilized
		ScalingInterval:    30 * time.Second,
	}
}

// NewAdaptiveWorkerPool creates a new adaptive worker pool
func NewAdaptiveWorkerPool(handler RequestHandler, opts *AdaptiveOptions) *AdaptiveWorkerPool {
	if opts == nil {
		opts = DefaultAdaptiveOptions()
	}

	// Ensure min/max constraints
	if opts.MinWorkers < 1 {
		opts.MinWorkers = 1
	}
	if opts.MaxWorkers < opts.MinWorkers {
		opts.MaxWorkers = opts.MinWorkers * 2
	}
	if opts.ConcurrentOptions.NumWorkers < opts.MinWorkers {
		opts.ConcurrentOptions.NumWorkers = opts.MinWorkers
	}
	if opts.ConcurrentOptions.NumWorkers > opts.MaxWorkers {
		opts.ConcurrentOptions.NumWorkers = opts.MaxWorkers
	}

	// Create base concurrent handler
	concurrentHandler := NewConcurrentHandler(handler, opts.ConcurrentOptions)

	scalingCtx, scalingCancel := context.WithCancel(context.Background())

	awp := &AdaptiveWorkerPool{
		ConcurrentHandler:  concurrentHandler,
		minWorkers:         opts.MinWorkers,
		maxWorkers:         opts.MaxWorkers,
		scaleUpThreshold:   opts.ScaleUpThreshold,
		scaleDownThreshold: opts.ScaleDownThreshold,
		scalingInterval:    opts.ScalingInterval,
		currentWorkers:     int64(opts.ConcurrentOptions.NumWorkers),
		lastScaleTime:      time.Now(),
		utilizationHistory: make([]float64, 0, 10),
		historySize:        10,
		scalingCtx:         scalingCtx,
		scalingCancel:      scalingCancel,
	}

	// Start scaling monitor
	awp.startScalingMonitor()

	return awp
}

// startScalingMonitor starts the scaling monitoring goroutine
func (awp *AdaptiveWorkerPool) startScalingMonitor() {
	awp.scalingTicker = time.NewTicker(awp.scalingInterval)

	go func() {
		defer awp.scalingTicker.Stop()

		for {
			select {
			case <-awp.scalingTicker.C:
				awp.evaluateScaling()
			case <-awp.scalingCtx.Done():
				return
			case <-awp.ctx.Done():
				return
			}
		}
	}()
}

// evaluateScaling evaluates whether to scale workers up or down
func (awp *AdaptiveWorkerPool) evaluateScaling() {
	awp.scalingMu.Lock()
	defer awp.scalingMu.Unlock()

	// Calculate current utilization
	utilization := awp.calculateUtilization()

	// Add to history
	awp.utilizationHistory = append(awp.utilizationHistory, utilization)
	if len(awp.utilizationHistory) > awp.historySize {
		awp.utilizationHistory = awp.utilizationHistory[1:]
	}

	// Need at least 3 data points for scaling decisions
	if len(awp.utilizationHistory) < 3 {
		return
	}

	// Calculate average utilization over recent history
	avgUtilization := awp.calculateAverageUtilization()
	currentWorkers := atomic.LoadInt64(&awp.currentWorkers)

	// Determine scaling action
	var scaleAction string
	var targetWorkers int64

	if avgUtilization > awp.scaleUpThreshold && currentWorkers < int64(awp.maxWorkers) {
		// Scale up: increase by 25% or minimum 1 worker
		increase := max(1, int64(float64(currentWorkers)*0.25))
		targetWorkers = min(int64(awp.maxWorkers), currentWorkers+increase)
		scaleAction = "up"
	} else if avgUtilization < awp.scaleDownThreshold && currentWorkers > int64(awp.minWorkers) {
		// Scale down: decrease by 25% or minimum 1 worker
		decrease := max(1, int64(float64(currentWorkers)*0.25))
		targetWorkers = max(int64(awp.minWorkers), currentWorkers-decrease)
		scaleAction = "down"
	} else {
		// No scaling needed
		return
	}

	// Respect minimum time between scaling operations
	if time.Since(awp.lastScaleTime) < awp.scalingInterval {
		return
	}

	// Perform scaling
	if targetWorkers != currentWorkers {
		awp.scaleWorkers(targetWorkers, scaleAction)
		awp.lastScaleTime = time.Now()
	}
}

// calculateUtilization calculates current worker utilization
func (awp *AdaptiveWorkerPool) calculateUtilization() float64 {
	activeRequests := atomic.LoadInt64(&awp.activeRequests)
	currentWorkers := atomic.LoadInt64(&awp.currentWorkers)

	if currentWorkers == 0 {
		return 0.0
	}

	return float64(activeRequests) / float64(currentWorkers)
}

// calculateAverageUtilization calculates average utilization from history
func (awp *AdaptiveWorkerPool) calculateAverageUtilization() float64 {
	if len(awp.utilizationHistory) == 0 {
		return 0.0
	}

	var sum float64
	for _, util := range awp.utilizationHistory {
		sum += util
	}

	return sum / float64(len(awp.utilizationHistory))
}

// scaleWorkers scales the worker pool to the target size
func (awp *AdaptiveWorkerPool) scaleWorkers(targetWorkers int64, action string) {
	currentWorkers := atomic.LoadInt64(&awp.currentWorkers)

	if action == "up" && targetWorkers > currentWorkers {
		// Add workers
		workersToAdd := targetWorkers - currentWorkers
		awp.addWorkers(int(workersToAdd))
		atomic.StoreInt64(&awp.currentWorkers, targetWorkers)
	} else if action == "down" && targetWorkers < currentWorkers {
		// Remove workers (gracefully)
		workersToRemove := currentWorkers - targetWorkers
		awp.removeWorkers(int(workersToRemove))
		atomic.StoreInt64(&awp.currentWorkers, targetWorkers)
	}
}

// addWorkers adds new workers to the pool
func (awp *AdaptiveWorkerPool) addWorkers(count int) {
	for i := 0; i < count; i++ {
		worker := &worker{
			id:      len(awp.workers) + i,
			handler: awp.ConcurrentHandler,
		}

		awp.workers = append(awp.workers, worker)
		awp.wg.Add(1)
		go worker.run()
	}
}

// removeWorkers removes workers from the pool (graceful shutdown)
func (awp *AdaptiveWorkerPool) removeWorkers(count int) {
	// Note: This is a simplified implementation
	// In a production system, you'd want to gracefully stop specific workers
	// For now, we just reduce the effective worker count
	// The actual goroutines will stop when the context is cancelled

	if count >= len(awp.workers) {
		count = len(awp.workers) - awp.minWorkers
	}

	if count > 0 {
		awp.workers = awp.workers[:len(awp.workers)-count]
	}
}

// GetWorkerPoolStats returns current worker pool statistics
func (awp *AdaptiveWorkerPool) GetWorkerPoolStats() map[string]interface{} {
	awp.scalingMu.RLock()
	defer awp.scalingMu.RUnlock()

	currentUtilization := awp.calculateUtilization()
	avgUtilization := awp.calculateAverageUtilization()

	stats := map[string]interface{}{
		"current_workers":      atomic.LoadInt64(&awp.currentWorkers),
		"min_workers":          awp.minWorkers,
		"max_workers":          awp.maxWorkers,
		"current_utilization":  currentUtilization,
		"average_utilization":  avgUtilization,
		"scale_up_threshold":   awp.scaleUpThreshold,
		"scale_down_threshold": awp.scaleDownThreshold,
		"last_scale_time":      awp.lastScaleTime.Unix(),
		"utilization_history":  awp.utilizationHistory,
	}

	// Add base metrics
	baseMetrics := awp.ConcurrentHandler.Metrics()
	for k, v := range baseMetrics {
		stats[k] = v
	}

	return stats
}

// Shutdown gracefully shuts down the adaptive worker pool
func (awp *AdaptiveWorkerPool) Shutdown(timeout time.Duration) error {
	// Stop scaling monitor
	awp.scalingCancel()

	// Shutdown base handler
	return awp.ConcurrentHandler.Shutdown(timeout)
}

// Helper functions
func min(a, b int64) int64 {
	if a < b {
		return a
	}
	return b
}

func max(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}
