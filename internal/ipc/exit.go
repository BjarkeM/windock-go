// The ipc package carries the one message windock needs to send between its own
// processes: the copy that is running should stop.
//
// The installer has to get a running copy out of the way before it replaces the
// executable. Terminating it drops whatever the tray was in the middle of,
// so the running copy waits on a named event and shuts down when another process sets it.
//
// Both names live in the session-local namespace: two users logged on at once
// each get their own instance, and neither can stop the other's.
package ipc

import (
	"errors"
	"fmt"
	"time"

	"golang.org/x/sys/windows"
)

const (
	// InstanceMutex is the single-instance lock. Its presence is also how
	// WaitGone tells that a copy is still alive.
	InstanceMutex = `Local\WinDockGoSingleInstance`

	exitEvent = `Local\WinDockGoExit`

	// Not all builds of x/sys export this one, and it is the only access
	// right SignalExit needs.
	eventModifyState = 0x0002
)

// Listener holds the exit event on behalf of a running copy.
type Listener struct {
	event windows.Handle
	done  windows.Handle
}

// Listen creates the exit event and calls stop once another process signals it.
// stop runs on its own goroutine, so a listener whose stop touches the UI has
// to marshal that itself.
//
// Close releases the event and retires the goroutine.
func Listen(stop func()) (*Listener, error) {
	name, err := windows.UTF16PtrFromString(exitEvent)
	if err != nil {
		return nil, err
	}
	// Manual reset: the signal is a one-way announcement, and nothing should
	// be able to consume it before the waiter sees it.
	event, err := windows.CreateEvent(nil, 1, 0, name)
	if err != nil && !errors.Is(err, windows.ERROR_ALREADY_EXISTS) {
		return nil, fmt.Errorf("creating the exit event: %w", err)
	}

	// A second event so Close can retire the waiting goroutine rather than
	// closing a handle out from under it.
	done, err := windows.CreateEvent(nil, 1, 0, nil)
	if err != nil {
		windows.CloseHandle(event)
		return nil, fmt.Errorf("creating the shutdown event: %w", err)
	}

	l := &Listener{event: event, done: done}
	go func() {
		n, err := windows.WaitForMultipleObjects(
			[]windows.Handle{event, done}, false, windows.INFINITE)
		if err == nil && n == windows.WAIT_OBJECT_0 {
			stop()
		}
	}()
	return l, nil
}

// Close releases the exit event.
func (l *Listener) Close() {
	windows.SetEvent(l.done)
	windows.CloseHandle(l.event)
	windows.CloseHandle(l.done)
}

// SignalExit asks a running copy to stop, reporting whether one was there to
// ask. It returns as soon as the request is delivered; use WaitGone to wait for
// the process to actually go.
func SignalExit() (bool, error) {
	name, err := windows.UTF16PtrFromString(exitEvent)
	if err != nil {
		return false, err
	}
	h, err := windows.OpenEvent(eventModifyState, false, name)
	if err != nil {
		if errors.Is(err, windows.ERROR_FILE_NOT_FOUND) {
			return false, nil
		}
		return false, fmt.Errorf("opening the exit event: %w", err)
	}
	defer windows.CloseHandle(h)

	if err := windows.SetEvent(h); err != nil {
		return false, fmt.Errorf("signalling the exit event: %w", err)
	}
	return true, nil
}

// WaitGone waits until no copy holds the single-instance mutex, which is the
// last thing a copy lets go of. Waiting on the exit event would only say the
// message was received, and the installer needs to know the executable is no
// longer in use.
func WaitGone(timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for {
		running, err := instanceRunning()
		if err != nil {
			return err
		}
		if !running {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("windock was still running %s after being asked to stop", timeout)
		}
		time.Sleep(100 * time.Millisecond)
	}
}

// Running reports whether a copy of windock holds the single-instance lock.
func Running() (bool, error) { return instanceRunning() }

func instanceRunning() (bool, error) {
	name, err := windows.UTF16PtrFromString(InstanceMutex)
	if err != nil {
		return false, err
	}
	h, err := windows.CreateMutex(nil, false, name)
	if h != 0 {
		defer windows.CloseHandle(h)
	}
	if err != nil {
		if errors.Is(err, windows.ERROR_ALREADY_EXISTS) {
			return true, nil
		}
		return false, fmt.Errorf("single-instance check failed: %w", err)
	}
	return false, nil
}
