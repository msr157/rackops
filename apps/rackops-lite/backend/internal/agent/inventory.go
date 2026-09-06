package agent

import (
	"context"
	"sync"
	"time"

	"github.com/rackops/rackops/internal/types"
)

// Inventory is sampled independently. Only a new successful sample may drive
// reconciliation; a timeout must never mean that all containers disappeared.
type inventory struct {
	mu         sync.Mutex
	containers []types.ContainerStatus
	fresh      bool
	finished   time.Time
}

func (i *inventory) take() ([]types.ContainerStatus, bool) {
	i.mu.Lock()
	defer i.mu.Unlock()
	valid := i.fresh && time.Since(i.finished) < 10*time.Second
	i.fresh = false
	return append([]types.ContainerStatus(nil), i.containers...), valid
}

func (i *inventory) run(ctx context.Context, interval time.Duration, sample func(context.Context) ([]types.ContainerStatus, error)) {
	for ctx.Err() == nil {
		pollCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		containers, err := sample(pollCtx)
		valid := err == nil && pollCtx.Err() == nil
		cancel()
		i.mu.Lock()
		if valid {
			i.containers = containers
		}
		i.fresh, i.finished = valid, time.Now()
		i.mu.Unlock()
		if !sleepCtx(ctx, interval) {
			return
		}
	}
}
