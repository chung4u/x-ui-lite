package web

import (
	"context"
	"fmt"
	"net/http"
)

type coreStopper interface {
	StopXray() error
}

// Drain writers before the final reset-on-read sample. A failed sample must
// leave the core and its pending counters alive so maintenance can be retried.
func stopServerComponents(ctx, jobs context.Context, server *http.Server, checkpoint func() error, core coreStopper) error {
	if jobs.Done() != nil {
		select {
		case <-jobs.Done():
		case <-ctx.Done():
			return fmt.Errorf("等待后台任务停止失败: %w", ctx.Err())
		}
	}
	if server != nil {
		if err := server.Shutdown(ctx); err != nil {
			return fmt.Errorf("等待管理请求结束失败: %w", err)
		}
	}
	if checkpoint != nil {
		if err := checkpoint(); err != nil {
			return fmt.Errorf("保存月流量失败: %w", err)
		}
	}
	return core.StopXray()
}
