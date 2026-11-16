package web

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

type testCoreStop struct {
	calls int
	err   error
}

func (c *testCoreStop) StopXray() error { c.calls++; return c.err }

func TestStopWaitsForJobsAndHTTPBeforeFinalTraffic(t *testing.T) {
	entered, release := make(chan struct{}), make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(entered)
		<-release
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()
	clientDone := make(chan struct{})
	go func() {
		defer close(clientDone)
		res, err := http.Get(srv.URL)
		if err == nil {
			res.Body.Close()
		}
	}()
	<-entered
	jobs, jobsDone := context.WithCancel(context.Background())
	ctx, cancel := context.WithTimeout(context.Background(), time.Second*3)
	defer cancel()
	core := &testCoreStop{}
	done := make(chan error, 1)
	go func() { done <- stopServerComponents(ctx, jobs, srv.Config, core) }()
	select {
	case err := <-done:
		t.Fatalf("stopped before jobs: %v", err)
	case <-time.After(20 * time.Millisecond):
	}
	jobsDone()
	select {
	case err := <-done:
		t.Fatalf("stopped before HTTP request: %v", err)
	case <-time.After(20 * time.Millisecond):
	}
	close(release)
	<-clientDone
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if core.calls != 1 {
		t.Fatalf("final traffic calls=%d", core.calls)
	}
}

func TestStopTimeoutDoesNotStopCore(t *testing.T) {
	jobs, jobsDone := context.WithCancel(context.Background())
	defer jobsDone()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	core := &testCoreStop{}
	if err := stopServerComponents(ctx, jobs, nil, core); !errors.Is(err, context.Canceled) {
		t.Fatalf("error=%v", err)
	}
	if core.calls != 0 {
		t.Fatal("core stopped before writers drained")
	}
}

func TestStopReturnsAccountingFailureForRetry(t *testing.T) {
	want := errors.New("database unavailable")
	core := &testCoreStop{err: want}
	if err := stopServerComponents(context.Background(), context.Background(), nil, core); !errors.Is(err, want) {
		t.Fatalf("error=%v", err)
	}
	core.err = nil
	if err := stopServerComponents(context.Background(), context.Background(), nil, core); err != nil {
		t.Fatal(err)
	}
	if core.calls != 2 {
		t.Fatal("failed final sample was not retried")
	}
}
