package main

import (
	"reflect"
	"testing"
)

func TestShutdownFinalizesSessionBeforeExit(t *testing.T) {
	var calls []string
	finalize := newShutdownFinalizer(
		func() { calls = append(calls, "stop-snapshots") },
		func() error { calls = append(calls, "save-conversation"); return nil },
		func() error { calls = append(calls, "save-analytics"); return nil },
		func() error { calls = append(calls, "stop-dashboard"); return nil },
		func() error { calls = append(calls, "restore-terminal"); return nil },
	)
	finalize()
	finalize()
	want := []string{"stop-snapshots", "save-conversation", "save-analytics", "stop-dashboard", "restore-terminal"}
	if !reflect.DeepEqual(calls, want) {
		t.Fatalf("shutdown order = %v, want %v", calls, want)
	}
}
