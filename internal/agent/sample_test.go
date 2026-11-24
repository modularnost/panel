package agent

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"modularnost/internal/docker"
)

func TestStatsSampleSharesWorkAcrossCallers(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		c := &Collector{}
		release := make(chan struct{})
		var calls atomic.Int32
		collect := func(ctx context.Context, service string) ([]docker.TaskStats, error) {
			calls.Add(1)
			select {
			case <-release:
				return []docker.TaskStats{{Service: service}}, nil
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		}

		ctx, cancel := context.WithCancel(t.Context())
		first := make(chan error, 1)
		go func() {
			_, err := c.sample(ctx, "web", collect)
			first <- err
		}()
		synctest.Wait()
		go func() {
			stats, err := c.sample(t.Context(), "web", collect)
			if err != nil || len(stats) != 1 || stats[0].Service != "web" {
				t.Errorf("shared sample = %+v, %v", stats, err)
			}
		}()
		synctest.Wait()
		cancel()
		if err := <-first; !errors.Is(err, context.Canceled) {
			t.Errorf("canceled caller returned %v", err)
		}
		close(release)
		synctest.Wait()

		c.sample(t.Context(), "web", collect)
		if got := calls.Load(); got != 1 {
			t.Fatalf("collected %d times before expiry, want 1", got)
		}
		c.sample(t.Context(), "db", collect)
		time.Sleep(sampleTTL)
		c.sample(t.Context(), "web", collect)
		if got := calls.Load(); got != 3 {
			t.Fatalf("collected %d times after a different filter and expiry, want 3", got)
		}
	})
}

func TestStatsSampleRetriesErrors(t *testing.T) {
	c := &Collector{}
	want := errors.New("unavailable")
	_, err := c.sample(t.Context(), "", func(context.Context, string) ([]docker.TaskStats, error) {
		return nil, want
	})
	if !errors.Is(err, want) {
		t.Fatalf("sample returned %v, want %v", err, want)
	}
	_, err = c.sample(t.Context(), "", func(context.Context, string) ([]docker.TaskStats, error) {
		return nil, nil
	})
	if err != nil {
		t.Fatalf("sample reused a failed collection: %v", err)
	}
}
