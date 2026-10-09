package voice

import (
	"context"
	"testing"
	"time"
)

func TestChromiumDoneWhenWindowConnectionCloses(t *testing.T) {
	lost := make(chan struct{})
	done := monitorChromiumDone(context.Background(), lost)
	close(lost)

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("browser session did not finish after Chromium disconnected")
	}
}

func TestChromiumDoneWhenSessionContextIsCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	done := monitorChromiumDone(ctx, make(chan struct{}))
	cancel()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("browser session did not finish after its context was canceled")
	}
}
