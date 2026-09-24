package autostart

import (
	"bytes"
	"encoding/binary"
	"encoding/xml"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"syscall"
	"unicode/utf16"

	"golang.org/x/sys/windows"
)

// taskName is the scheduled task used for the elevated logon entry.
//
// A Run key entry always starts at medium integrity, and a medium-integrity
// process cannot call SetWindowPos on a window owned by an elevated one. A
// scheduled task registered with the highest privileges is the only way to
// start elevated at logon without a UAC prompt every time.
const taskName = "WinDock-Go"

// EnableElevated registers the scheduled task. Creating one requires
// administrator rights, which is why this is offered by the installer rather
// than from the tray menu.
func EnableElevated() error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		return fmt.Errorf("reading the task user: %w", err)
	}
	definition := elevatedTaskXML(exe, user.User.Sid.String())
	f, err := os.CreateTemp("", "windock-task-*.xml")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	_, writeErr := f.Write(encodeTaskXML(definition))
	closeErr := f.Close()
	if err := errors.Join(writeErr, closeErr); err != nil {
		return err
	}
	out, err := runSchtasks("/Create", "/TN", taskName, "/XML", f.Name(), "/F")
	if err != nil {
		return fmt.Errorf("creating the scheduled task: %w (%s)", err, strings.TrimSpace(string(out)))
	}
	// Register successfully before removing the fallback. Otherwise the Run
	// entry can win the single-instance lock with a medium-integrity process.
	return disableRunEntry()
}

// Use an interactive token in the registering user's desktop, without a
// password, a battery restriction, or the scheduler's default 72-hour limit.
func elevatedTaskXML(exe, sid string) string {
	escape := func(s string) string {
		var b bytes.Buffer
		xml.EscapeText(&b, []byte(s))
		return b.String()
	}
	return fmt.Sprintf(`<?xml version="1.0"?>
<Task version="1.2" xmlns="http://schemas.microsoft.com/windows/2004/02/mit/task">
  <Triggers><LogonTrigger><Enabled>true</Enabled><UserId>%s</UserId></LogonTrigger></Triggers>
  <Principals><Principal id="User">
    <UserId>%s</UserId><LogonType>InteractiveToken</LogonType><RunLevel>HighestAvailable</RunLevel>
  </Principal></Principals>
  <Settings>
    <MultipleInstancesPolicy>IgnoreNew</MultipleInstancesPolicy>
    <DisallowStartIfOnBatteries>false</DisallowStartIfOnBatteries>
    <StopIfGoingOnBatteries>false</StopIfGoingOnBatteries>
    <ExecutionTimeLimit>PT0S</ExecutionTimeLimit>
  </Settings>
  <Actions Context="User"><Exec><Command>%s</Command><Arguments>tray</Arguments></Exec></Actions>
</Task>`, escape(sid), escape(sid), escape(exe))
}

// schtasks loads XML into a UTF-16 string before passing it to Task Scheduler.
// An explicit UTF-8 declaration then conflicts with that string's encoding.
// Write UTF-16LE with a BOM (including paths outside the system code page),
// and leave the declaration encoding-neutral so string-based imports work too.
func encodeTaskXML(definition string) []byte {
	units := utf16.Encode([]rune(definition))
	data := make([]byte, 2+2*len(units))
	data[0], data[1] = 0xff, 0xfe
	for i, unit := range units {
		binary.LittleEndian.PutUint16(data[2+2*i:], unit)
	}
	return data
}

// DisableElevated removes the scheduled task. Removing one that is not there is
// not an error, matching Disable.
func DisableElevated() error {
	on, _, err := ElevatedEnabled()
	if err != nil {
		return err
	}
	if !on {
		return nil
	}
	out, err := runSchtasks("/Delete", "/TN", taskName, "/F")
	if err != nil {
		return fmt.Errorf("removing the scheduled task: %w (%s)", err, strings.TrimSpace(string(out)))
	}
	return nil
}

// ElevatedEnabled reports whether the scheduled task exists, and what it runs.
func ElevatedEnabled() (bool, string, error) {
	out, err := runSchtasks("/Query", "/TN", taskName, "/XML", "ONE")
	if err != nil {
		var exitErr *exec.ExitError
		if !errors.As(err, &exitErr) {
			return false, "", fmt.Errorf("querying the scheduled task: %w", err)
		}
		// The only failure worth distinguishing is "no such task", and schtasks
		// says so in whatever language Windows is installed in. A non-zero exit
		// with no XML is taken as absent.
		return false, "", nil
	}
	return true, commandFromTaskXML(decodeUTF16(out)), nil
}

var (
	taskCommandRe = regexp.MustCompile(`(?s)<Command>(.*?)</Command>`)
	taskArgsRe    = regexp.MustCompile(`(?s)<Arguments>(.*?)</Arguments>`)
)

// commandFromTaskXML rebuilds the command line from the task definition. The
// XML is read rather than the table output of /FO LIST because the latter is
// localised and this is not.
func commandFromTaskXML(xml string) string {
	m := taskCommandRe.FindStringSubmatch(xml)
	if m == nil {
		return ""
	}
	cmd := strings.TrimSpace(m[1])
	if a := taskArgsRe.FindStringSubmatch(xml); a != nil {
		if args := strings.TrimSpace(a[1]); args != "" {
			cmd += " " + args
		}
	}
	return cmd
}

// decodeUTF16 converts schtasks' /XML output, which is UTF-16 with a byte order
// mark, into a string. Anything without the mark is passed through.
func decodeUTF16(b []byte) string {
	if len(b) < 2 || b[0] != 0xFF || b[1] != 0xFE {
		return string(b)
	}
	b = b[2:]
	u := make([]uint16, 0, len(b)/2)
	for i := 0; i+1 < len(b); i += 2 {
		u = append(u, binary.LittleEndian.Uint16(b[i:]))
	}
	return string(utf16.Decode(u))
}

var runSchtasks = schtasks

// schtasks runs the scheduler CLI without flashing a console window, and with
// no stdin, so that a prompt fails immediately instead of hanging an installer.
func schtasks(args ...string) ([]byte, error) {
	cmd := exec.Command("schtasks.exe", args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = &buf
	err := cmd.Run()
	return buf.Bytes(), err
}
