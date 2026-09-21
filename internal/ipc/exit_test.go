package ipc

import (
	"testing"
	"time"
)

// skipIfWinDockRunning keeps the tests off a real instance: they signal the
// exit event by name, and a running tray would take that personally.
func skipIfWinDockRunning(t *testing.T) {
	t.Helper()
	running, err := Running()
	if err != nil {
		t.Fatalf("Running: %v", err)
	}
	if running {
		t.Skip("a copy of windock is running; skipping so the tests do not stop it")
	}
}

func TestSignalExitReachesAListener(t *testing.T) {
	skipIfWinDockRunning(t)

	stopped := make(chan struct{})
	l, err := Listen(func() { close(stopped) })
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	defer l.Close()

	running, err := SignalExit()
	if err != nil {
		t.Fatalf("SignalExit: %v", err)
	}
	if !running {
		t.Fatal("SignalExit did not find the listener")
	}

	select {
	case <-stopped:
	case <-time.After(5 * time.Second):
		t.Fatal("the listener was never told to stop")
	}
}

func TestSignalExitWithNothingRunning(t *testing.T) {
	skipIfWinDockRunning(t)

	running, err := SignalExit()
	if err != nil {
		t.Fatalf("SignalExit: %v", err)
	}
	if running {
		t.Fatal("SignalExit reported a running copy when the event does not exist")
	}
}

// Close must retire the waiting goroutine without it mistaking the shutdown for
// an exit request, or an ordinary quit would call stop on the way out.
func TestCloseDoesNotTriggerStop(t *testing.T) {
	skipIfWinDockRunning(t)

	stopped := make(chan struct{})
	l, err := Listen(func() { close(stopped) })
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	l.Close()

	select {
	case <-stopped:
		t.Fatal("Close ran the stop function")
	case <-time.After(250 * time.Millisecond):
	}
}

func TestWaitGoneReturnsWhenTheLockIsFree(t *testing.T) {
	skipIfWinDockRunning(t)

	if err := WaitGone(time.Second); err != nil {
		t.Fatalf("WaitGone with no instance: %v", err)
	}
}

func TestWaitGoneTimesOutWhileTheLockIsHeld(t *testing.T) {
	skipIfWinDockRunning(t)

	release := holdInstanceLock(t)
	defer release()

	running, err := Running()
	if err != nil {
		t.Fatalf("Running: %v", err)
	}
	if !running {
		t.Fatal("Running did not see the held lock")
	}
	if err := WaitGone(300 * time.Millisecond); err == nil {
		t.Fatal("WaitGone returned while the lock was still held")
	}
}
