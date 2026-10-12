package agent

import (
	"context"
	"github.com/rackops/rackops/internal/types"
	"sync"
	"time"
)

const taskQueueCapacity = 64

// taskWorkers separates long operations from reporting. Dispatch preserves
// per-service order while allowing unrelated services to make progress.
type taskWorkers struct {
	mu      sync.Mutex
	queued  []types.AgentTask
	active  map[string]bool
	known   map[string]bool
	wake    chan struct{}
	results chan types.TaskResult
	execute func(context.Context, types.AgentTask) *types.TaskResult
	wg      sync.WaitGroup
}

func newTaskWorkers(ctx context.Context, execute func(context.Context, types.AgentTask) *types.TaskResult) *taskWorkers {
	w := &taskWorkers{active: map[string]bool{}, known: map[string]bool{}, wake: make(chan struct{}, 1), results: make(chan types.TaskResult, taskQueueCapacity), execute: execute}
	for i := 0; i < 2; i++ {
		w.wg.Add(1)
		go w.run(ctx)
	}
	return w
}

func taskKey(t types.AgentTask) string {
	if t.ServiceID != "" {
		return t.ServiceID
	}
	return t.ID
}

func (w *taskWorkers) signal() {
	select {
	case w.wake <- struct{}{}:
	default:
	}
}

func (w *taskWorkers) slots() int {
	w.mu.Lock()
	defer w.mu.Unlock()
	return taskQueueCapacity - len(w.known)
}

func (w *taskWorkers) enqueue(task types.AgentTask) bool {
	w.mu.Lock()
	if w.known[task.ID] {
		w.mu.Unlock()
		return true
	}
	if len(w.known) >= taskQueueCapacity {
		w.mu.Unlock()
		return false
	}
	w.known[task.ID] = true
	w.queued = append(w.queued, task)
	w.mu.Unlock()
	w.signal()
	return true
}

// forget follows successful result delivery; until then a retransmitted task
// ID must not execute a second time in this agent process.
func (w *taskWorkers) forget(results []types.TaskResult) {
	w.mu.Lock()
	defer w.mu.Unlock()
	for _, result := range results {
		delete(w.known, result.TaskID)
	}
}

func (w *taskWorkers) take() (types.AgentTask, bool) {
	w.mu.Lock()
	defer w.mu.Unlock()
	for i, task := range w.queued {
		key := taskKey(task)
		if w.active[key] {
			continue
		}
		w.active[key] = true
		w.queued = append(w.queued[:i], w.queued[i+1:]...)
		return task, true
	}
	return types.AgentTask{}, false
}

func (w *taskWorkers) run(ctx context.Context) {
	defer w.wg.Done()
	for {
		if ctx.Err() != nil {
			return
		}
		task, ok := w.take()
		if !ok {
			select {
			case <-ctx.Done():
				return
			case <-w.wake:
				continue
			}
		}
		w.signal() // allow the other worker to dispatch a different service
		deadline := 30 * time.Minute
		// Backup/transfer actions run in a dedicated worker and remain subject to
		// per-service ordering, while reporting continues on the agent loop.
		// A short deadline would turn a healthy multi-hour transfer into an
		// uncertain write after its source has already been quiesced.
		if task.Kind == types.TaskMigrationBackup || task.Kind == types.TaskMigrationTransferSource || task.Kind == types.TaskMigrationTransferDestination || task.Kind == types.TaskMigrationRestore || task.Kind == types.TaskMigrationValidate || task.Kind == types.TaskMigrationCutover || task.Kind == types.TaskMigrationEnableWrites {
			deadline = 24 * time.Hour
		}
		jobCtx, cancel := context.WithTimeout(ctx, deadline)
		var result *types.TaskResult
		if task.Kind == types.TaskLogsFollow {
			// A log follow outlives its dispatch task and belongs to the agent.
			result = w.execute(ctx, task)
		} else {
			result = w.execute(jobCtx, task)
		}
		cancel()
		if result == nil {
			result = &types.TaskResult{TaskID: task.ID, ServiceID: task.ServiceID, OK: true}
		}
		w.mu.Lock()
		delete(w.active, taskKey(task))
		w.mu.Unlock()
		w.signal()
		select {
		case w.results <- *result:
		case <-ctx.Done():
			return
		}
	}
}
