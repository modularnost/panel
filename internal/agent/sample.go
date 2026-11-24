package agent

import (
	"context"
	"errors"
	"time"

	"modularnost/internal/docker"
)

const (
	sampleTTL      = 2 * time.Second
	sampleTimeout  = 12 * time.Second
	maxSamples     = 64
	maxCollections = 8
)

type statsSample struct {
	done    chan struct{}
	stats   []docker.TaskStats
	err     error
	expires time.Time
}

func (c *Collector) sample(ctx context.Context, service string, collect func(context.Context, string) ([]docker.TaskStats, error)) ([]docker.TaskStats, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	c.samplesMu.Lock()
	now := time.Now()
	current := c.samples[service]
	if current == nil || (!current.expires.IsZero() && !now.Before(current.expires)) {
		if c.collecting >= maxCollections {
			c.samplesMu.Unlock()
			return nil, errors.New("metrics collection is busy")
		}
		if c.samples == nil {
			c.samples = make(map[string]*statsSample)
		}
		if current == nil && len(c.samples) >= maxSamples {
			var oldest time.Time
			var key string
			for name, entry := range c.samples {
				if !entry.expires.IsZero() && (oldest.IsZero() || entry.expires.Before(oldest)) {
					oldest, key = entry.expires, name
				}
			}
			delete(c.samples, key)
		}
		current = &statsSample{done: make(chan struct{})}
		c.samples[service] = current
		c.collecting++
		// A caller can leave without interrupting the other waiters.
		shared, cancel := context.WithTimeout(context.WithoutCancel(ctx), sampleTimeout)
		go func(sample *statsSample) {
			defer cancel()
			sample.stats, sample.err = collect(shared, service)
			c.samplesMu.Lock()
			defer c.samplesMu.Unlock()
			c.collecting--
			if sample.err != nil || shared.Err() != nil {
				delete(c.samples, service)
			} else {
				sample.expires = time.Now().Add(sampleTTL)
			}
			close(sample.done)
		}(current)
	}
	c.samplesMu.Unlock()
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-current.done:
		return current.stats, current.err
	}
}
