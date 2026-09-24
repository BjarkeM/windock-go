package autostart

import (
	"bytes"
	"encoding/binary"
	"encoding/xml"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
	"unicode/utf16"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

func TestElevatedTaskDefinition(t *testing.T) {
	const exe = `C:\Program Files\A & B\日本語-🪟\windock.exe`
	const sid = "S-1-5-21-123-456-789-1001"
	var task struct {
		TriggerUser  string `xml:"Triggers>LogonTrigger>UserId"`
		User         string `xml:"Principals>Principal>UserId"`
		Logon        string `xml:"Principals>Principal>LogonType"`
		Level        string `xml:"Principals>Principal>RunLevel"`
		Instances    string `xml:"Settings>MultipleInstancesPolicy"`
		BatteryStart string `xml:"Settings>DisallowStartIfOnBatteries"`
		BatteryStop  string `xml:"Settings>StopIfGoingOnBatteries"`
		Limit        string `xml:"Settings>ExecutionTimeLimit"`
		Command      string `xml:"Actions>Exec>Command"`
		Args         string `xml:"Actions>Exec>Arguments"`
	}
	if err := xml.Unmarshal([]byte(elevatedTaskXML(exe, sid)), &task); err != nil {
		t.Fatal(err)
	}
	got := []string{task.TriggerUser, task.User, task.Logon, task.Level, task.Instances, task.BatteryStart, task.BatteryStop, task.Limit, task.Command, task.Args}
	want := []string{sid, sid, "InteractiveToken", "HighestAvailable", "IgnoreNew", "false", "false", "PT0S", exe, "tray"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("task = %v, want %v", got, want)
	}
}

func TestTaskXMLFileEncoding(t *testing.T) {
	definition := elevatedTaskXML(`C:\日本語-🪟\windock.exe`, "S-1-5-21-1001")
	data := encodeTaskXML(definition)
	if !bytes.HasPrefix(data, []byte{0xff, 0xfe}) || len(data)%2 != 0 {
		t.Fatal("task file must be UTF-16LE with a byte order mark")
	}
	units := make([]uint16, (len(data)-2)/2)
	for i := range units {
		units[i] = binary.LittleEndian.Uint16(data[2+2*i:])
	}
	decoded := string(utf16.Decode(units))
	if decoded != definition {
		t.Fatalf("task file did not preserve Unicode: %q", decoded)
	}
	if !bytes.HasPrefix([]byte(decoded), []byte(`<?xml version="1.0"?>`)) {
		t.Fatal("task XML must not declare a conflicting string encoding")
	}
}

func preserveRunEntry(t *testing.T) {
	t.Helper()
	on, original, err := runEntry()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if !on {
			disableRunEntry()
			return
		}
		k, _, err := registry.CreateKey(registry.CURRENT_USER, runKey, registry.SET_VALUE)
		if err != nil {
			t.Error(err)
			return
		}
		defer k.Close()
		if err := k.SetStringValue(valueName, original); err != nil {
			t.Error(err)
		}
	})
}

func TestElevatedRegistrationReplacesRunEntryOnlyOnSuccess(t *testing.T) {
	preserveRunEntry(t)
	old := runSchtasks
	t.Cleanup(func() { runSchtasks = old })
	for _, fail := range []bool{true, false} {
		if err := Enable(); err != nil {
			t.Fatal(err)
		}
		var filename string
		runSchtasks = func(args ...string) ([]byte, error) {
			if len(args) != 6 || args[0] != "/Create" || args[3] != "/XML" {
				t.Fatalf("unexpected registration: %v", args)
			}
			filename = args[4]
			data, err := os.ReadFile(filename)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.HasPrefix(data, []byte{0xff, 0xfe}) {
				t.Fatal("registration did not write a UTF-16LE file")
			}
			var task any
			if err := xml.Unmarshal([]byte(decodeUTF16(data)), &task); err != nil {
				t.Fatal(err)
			}
			if fail {
				return nil, errors.New("registration denied")
			}
			return nil, nil
		}
		err := EnableElevated()
		if (err != nil) != fail {
			t.Fatalf("registration error = %v, fail = %v", err, fail)
		}
		on, _, err := runEntry()
		if err != nil || on != fail {
			t.Fatalf("Run entry on = %v, err = %v; failed registration = %v", on, err, fail)
		}
		if _, err := os.Stat(filename); !os.IsNotExist(err) {
			t.Fatalf("temporary XML left behind: %v", err)
		}
	}
}

func TestDisableRemovesTaskWithoutRunEntry(t *testing.T) {
	preserveRunEntry(t)
	if err := disableRunEntry(); err != nil {
		t.Fatal(err)
	}
	old := runSchtasks
	t.Cleanup(func() { runSchtasks = old })
	deleted := false
	runSchtasks = func(args ...string) ([]byte, error) {
		switch args[0] {
		case "/Query":
			return []byte(elevatedTaskXML(`C:\windock.exe`, "test-user")), nil
		case "/Delete":
			deleted = true
			return nil, nil
		default:
			t.Fatalf("unexpected scheduler command: %v", args)
			return nil, nil
		}
	}
	if err := Disable(); err != nil {
		t.Fatal(err)
	}
	if !deleted {
		t.Fatal("Disable left the elevated task registered")
	}
}

// Opt in because this exercises the real scheduler, using a disabled task
// with a unique name that is removed before the test finishes.
func TestTaskXMLSchedulerImport(t *testing.T) {
	if os.Getenv("WINDOCK_TEST_SCHEDULER") != "1" {
		t.Skip("set WINDOCK_TEST_SCHEDULER=1 to test the real Task Scheduler importer")
	}
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		t.Fatal(err)
	}
	definition := elevatedTaskXML(`C:\WinDock XML test 日本語-🪟 &\windock.exe`, user.User.Sid.String())
	definition = strings.Replace(definition, "<Settings>", "<Settings><Enabled>false</Enabled>", 1)
	if !windows.GetCurrentProcessToken().IsElevated() {
		// Encoding and schema checks also work from an ordinary test shell.
		definition = strings.Replace(definition, "HighestAvailable", "LeastPrivilege", 1)
	}
	filename := filepath.Join(t.TempDir(), "task.xml")
	if err := os.WriteFile(filename, encodeTaskXML(definition), 0600); err != nil {
		t.Fatal(err)
	}
	name := fmt.Sprintf("WinDock-Go-EncodingTest-%d-%d", os.Getpid(), time.Now().UnixNano())
	out, err := schtasks("/Create", "/TN", name, "/XML", filename)
	if err != nil {
		t.Fatalf("Task Scheduler import: %v: %s", err, out)
	}
	t.Cleanup(func() {
		if out, err := schtasks("/Delete", "/TN", name, "/F"); err != nil {
			t.Errorf("removing test task %s: %v: %s", name, err, out)
		}
	})
}
