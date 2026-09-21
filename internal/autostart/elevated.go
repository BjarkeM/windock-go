package autostart

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"os/exec"
	"regexp"
	"strings"
	"syscall"
	"unicode/utf16"
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
	cmd, err := Command()
	if err != nil {
		return err
	}
	// ONLOGON with no /RU makes the task run as whoever creates it, with an
	// interactive token. Naming a user with /RU would risk schtasks asking for
	// a password, which has nowhere to go from inside an installer.
	out, err := schtasks("/Create", "/TN", taskName, "/TR", cmd,
		"/SC", "ONLOGON", "/RL", "HIGHEST", "/F")
	if err != nil {
		return fmt.Errorf("creating the scheduled task: %w (%s)", err, strings.TrimSpace(string(out)))
	}
	return nil
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
	out, err := schtasks("/Delete", "/TN", taskName, "/F")
	if err != nil {
		return fmt.Errorf("removing the scheduled task: %w (%s)", err, strings.TrimSpace(string(out)))
	}
	return nil
}

// ElevatedEnabled reports whether the scheduled task exists, and what it runs.
func ElevatedEnabled() (bool, string, error) {
	out, err := schtasks("/Query", "/TN", taskName, "/XML", "ONE")
	if err != nil {
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
