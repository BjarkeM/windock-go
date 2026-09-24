package ui

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/lxn/walk"
	"github.com/lxn/win"
)

// A subprocess gives the deadlock regression a deadline without leaving a
// blocked Windows thread in the test runner. The fake engine sends a synchronous
// message during cleanup, just like the Windows settings broadcast.
func TestTrayExitPumpsMessagesUntilEngineStops(t *testing.T) {
	if os.Getenv("WINDOCK_TEST_TRAY_EXIT") != "1" {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		// Go test binaries do not inherit the app's Common Controls 6
		// resource. A private copy with a sidecar manifest enables Walk.
		exe := filepath.Join(t.TempDir(), "tray-test.exe")
		binary, err := os.ReadFile(os.Args[0])
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(exe, binary, 0600); err != nil {
			t.Fatal(err)
		}
		manifest, err := os.ReadFile("../../cmd/windock/app.manifest")
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(exe+".manifest", manifest, 0600); err != nil {
			t.Fatal(err)
		}
		cmd := exec.CommandContext(ctx, exe, "-test.run=^TestTrayExitPumpsMessagesUntilEngineStops$")
		cmd.Env = append(os.Environ(), "WINDOCK_TEST_TRAY_EXIT=1")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("tray shutdown: %v: %s", err, out)
		}
		return
	}
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	w, err := walk.NewMainWindow()
	if err != nil {
		t.Fatal(err)
	}
	defer w.Dispose()
	// Leave another window alive, as happens when settings are open.
	settings, err := walk.NewMainWindow()
	if err != nil {
		t.Fatal(err)
	}
	defer settings.Dispose()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	a := &App{window: w, stopEng: cancel, engDone: make(chan struct{})}
	w.Closing().Attach(a.onClosing)
	go func() {
		<-ctx.Done()
		settings.SendMessage(win.WM_NULL, 0, 0)
		close(a.engDone)
	}()
	w.Synchronize(func() { w.Close(); w.Close() })
	w.Run()
	select {
	case <-a.engDone:
	default:
		t.Fatal("UI exited before engine cleanup finished")
	}
	if !a.engineStopped {
		t.Fatal("shutdown did not finish")
	}
}
