package context_paths_fixture

import (
	"context"
	"time"
)

func WithTimeoutOnly() {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	_ = ctx
	_ = cancel
}

func WithDeadlineOnly() {
	ctx, _ := context.WithDeadline(context.Background(), time.Now().Add(time.Second))
	_ = ctx
}

func WithCancelOnly() {
	ctx, cancel := context.WithCancel(context.Background())
	_ = ctx
	_ = cancel
}

func TwoTimeoutSites() {
	a, _ := context.WithTimeout(context.Background(), time.Second)
	b, _ := context.WithTimeout(context.Background(), 2*time.Second)
	_ = a
	_ = b
}

func NoContextOps() {
	_ = "noop"
}
