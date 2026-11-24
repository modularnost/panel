package web

import (
	"context"
	"sync"
	"time"

	"modularnost/internal/firewall"
)

const firewallSpecTTL = 3 * time.Second

type firewallSpecCache struct {
	mu      sync.Mutex
	spec    firewall.Spec
	expires time.Time
	epoch   uint64
	pending *firewallSpecLoad
}

type firewallSpecLoad struct {
	done chan struct{}
	err  error
}

func (c *firewallSpecCache) invalidate() {
	c.mu.Lock()
	c.epoch++
	c.expires = time.Time{}
	c.mu.Unlock()
}

func (c *firewallSpecCache) get(ctx context.Context, load func(context.Context) (firewall.Spec, time.Time, error)) (firewall.Spec, error) {
	for ctx.Err() == nil {
		c.mu.Lock()
		if time.Now().Before(c.expires) {
			spec := c.spec
			c.mu.Unlock()
			return spec, nil
		}
		flight, epoch := c.pending, c.epoch
		if flight != nil {
			c.mu.Unlock()
			select {
			case <-ctx.Done():
				return firewall.Spec{}, ctx.Err()
			case <-flight.done:
				if flight.err != nil {
					return firewall.Spec{}, flight.err
				}
			}
			continue
		}
		flight = &firewallSpecLoad{done: make(chan struct{})}
		c.pending = flight
		c.mu.Unlock()
		spec, expires, err := load(ctx)
		c.mu.Lock()
		current := epoch == c.epoch
		if current && err == nil {
			c.spec, c.expires = spec, expires
		}
		flight.err = err
		c.pending = nil
		close(flight.done)
		c.mu.Unlock()
		if current {
			return spec, err
		}
	}
	return firewall.Spec{}, ctx.Err()
}
