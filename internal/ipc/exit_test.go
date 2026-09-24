package ipc

import (
	"fmt"
	"os"
	"testing"
	"time"
)

func TestMain(m *testing.M) {
	exitEvent = fmt.Sprintf(`Local\WinDockGoExitTest-%d`, os.Getpid())
	instanceMutexName = fmt.Sprintf(`Local\WinDockGoInstanceTest-%d`, os.Getpid())
	os.Exit(m.Run())
}

func TestSignalExitReachesAListener(t *testing.T) {

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

	if err := WaitGone(time.Second); err != nil {
		t.Fatalf("WaitGone with no instance: %v", err)
	}
}

func TestWaitGoneTimesOutWhileTheLockIsHeld(t *testing.T) {

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

// Close must return only once the waiter no longer uses the event handles.
// Repeated close/reopen exercises handle reuse that used to race the waiter.
func TestListenerCloseAndRestart(t *testing.T) {
	for i := 0; i < 50; i++ {
		l, err := Listen(func() { t.Error("unexpected stop during Close") })
		if err != nil {
			t.Fatal(err)
		}
		l.Close()
		l.Close()
		select {
		case <-l.finished:
		default:
			t.Fatal("Close returned before the waiter finished")
		}
	}
}
