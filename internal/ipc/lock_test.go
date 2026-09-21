package ipc

import (
	"testing"

	"golang.org/x/sys/windows"
)

// holdInstanceLock takes the single-instance mutex the way a running copy does.
func holdInstanceLock(t *testing.T) func() {
	t.Helper()
	name, err := windows.UTF16PtrFromString(InstanceMutex)
	if err != nil {
		t.Fatalf("UTF16PtrFromString: %v", err)
	}
	h, err := windows.CreateMutex(nil, false, name)
	if err != nil {
		t.Fatalf("CreateMutex: %v", err)
	}
	return func() { windows.CloseHandle(h) }
}
