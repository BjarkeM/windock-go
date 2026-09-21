# windock-go

A Go reimplementation of [WinDock](https://www.ivanyu.ca/windock) (by Ivan Yu), which
snaps windows into user-defined zones when you drag them to a screen edge,
corner or area.

## Why a rewrite

Because I thought it was interesting, and wanted to learn some more Go with win32.
WinDock didn't work with sandboxed processes, and doesn't deal well with display rebuilds (e.g. RDP sessions).

## Features added

**Preview instead of live resize.** The original resized the real window during
the drag, which an in-process hook can do by handling `WM_MOVING`. Out of
process that is not possible, so this shows a translucent preview over the
destination and snaps on release - similar to what FancyZones does.

**Guides while dragging.** This marks every edge trigger of the active
profile during a drag, with a few colors to make everything more legible.

**Configuration preview** The configuration of (new) zones in WinDock was a bit complicated as a new user, so windock-go brings a bit of a visual upgrade.

## Install

```
go build -o windock.exe ./cmd/windock
```

`cmd/windock/rsrc.syso` carries the manifest and the icon; see [Icons](#icons)
to regenerate it. Without the manifest the settings window cannot create its
widgets.

On first run, profiles are imported from the original's
`%LOCALAPPDATA%\WinDock\profile.json` if present. That file is only ever read,
so both can be installed side by side. With nothing to import, four starter
layouts are created - Halves, Quarters, Thirds and Ultrawide 20/60/20 - also
available later from **From layout**.

Configuration lives in `%LOCALAPPDATA%\WinDock-Go\profile.json`. Edits to it are
picked up within a couple of seconds.

## Installer

`build.ps1` runs the tests, builds the executable and compiles
`installer/windock.iss` into `dist\windock-setup-<version>.exe`:

```
.\build.ps1
.\build.ps1 -Version 0.2.0   # stamp a version other than the one in the script
.\build.ps1 -ExeOnly         # just windock.exe
```

It needs [Inno Setup](https://jrsoftware.org/isinfo.php), from
`winget install JRSoftware.InnoSetup`. Its compiler is not put on PATH and
winget installs it per-user, so the script looks where it actually lands;
`$env:ISCC` overrides that.

It installs to `C:\Program Files\WinDock-Go`, so it asks for elevation. The
configuration and the logon entry stay per-user, so the steps that write them
run as the user who started setup rather than as the administrator who
approved it. Only that user gets a logon entry; anyone else on the machine
turns it on themselves, from the tray menu or `windock autostart on`.

## Use

No arguments gives a notification-area icon which can be right-clicked for turning docking on/off, viewing the
profile list, settings, run-at-logon and exit; left-click opens settings.

The settings window replaces the rule table with a map of
your screens, drawn in the same colours the guides use.

- **Drag along a screen edge** to add an edge zone spanning what you dragged.
  Click without dragging to get the whole edge.
- **Drag a box in open space** to add a zone you drop a window into; the box is
  both the trigger and the destination.
- **Click a zone** to select it. The fields below show its numbers and redraw
  the map as you type, with `Maximize`, `Left half`, `Right half` and
  `Match trigger` for the common cases.

Edits are staged: the window works on a copy and writes nothing until OK or
Apply. Tray-menu actions are immediate.

```
windock                # tray icon and settings window (default)
windock settings       # the same, with settings already open
windock run            # watch for drags and snap, with no UI
windock diag           # print the monitors and how each rule resolves
windock probe          # follow the cursor and report which trigger it is in
windock exit           # ask a running copy to shut down
windock profiles       # list profiles
windock use <n|name>   # switch active profile
windock import <file>  # add an exported profile
windock export <n> <file>
windock enable | disable
windock autostart [on|elevated|off]
windock where          # print the config path
```

`windock diag` is the tool for a misbehaving snap: monitor order, the pixel
rectangle of every trigger and dock, and which rules are inactive and why.

`windock probe` answers whether the pointer is actually reaching a trigger -
"nothing happens" can mean the rule is wrong or that the pointer never got
there. It does not take the single-instance lock, so run it in a second terminal
while dragging, then Ctrl+C and read the extremes line.

`-config` also accepts a single exported profile
(`{"name": ..., "rules": [...]}`), used as a one-profile configuration. Such a
file is never written back to. Use `windock import` to add it properly.

## Configuration

The schema is one subsumed from [WinDock](https://www.ivanyu.ca/windock), with slight modifications. A rule pairs a `trigger` (where the cursor has
to go) with a `dock` (where the window lands), both as percentages:

```json
{
  "dock":    { "monitor": 0, "values": [0, 0, 20, 50] },
  "trigger": { "monitor": 0, "pos": "left", "type": "edge", "values": [0, 50] }
}
```

- `dock.values` is `[left, top, right, bottom]`, 0-100, of the monitor work area.
- `dock.use_work_area` is per zone and defaults to true, keeping the window
  clear of the taskbar and any other appbars. Set it `false` for a zone that
  should measure against the full monitor bounds instead.
- `trigger.type` is `edge`, `corner` or `area`.
  - `edge`: `pos` of `left`/`right`/`top`/`bottom`, `values` of `[start, end]`
    along that edge.
  - `corner`: `pos` of `top_left`/`top_right`/`bottom_left`/`bottom_right`, no
    values.
  - `area`: `values` of `[left, top, right, bottom]`.

Overlapping triggers resolve corners before edges before areas, regardless of
profile order; within a class the earlier rule wins.

Optional additions under `global_settings`:

| Key | Default | Meaning |
| --- | --- | --- |
| `edge_thickness_px` | 3 | How close to an edge the cursor must be. A 1px band is unreachable on the inner edges of a multi-monitor desktop. |
| `corner_size_px` | 120 | Side of the hit box at each corner. Corner triggers carry no values, so this has to come from a setting. |
| `show_preview` | true | Draw a translucent fill over the rectangle the window will snap to. |
| `preview_color` | `0x0078D7` | Preview fill, `0xRRGGBB`. |
| `preview_alpha` | 150 | Preview opacity, 0-255. |
| `show_trigger_guides` | true | Mark every trigger while dragging, highlighting the one under the cursor. |
| `guide_thickness_px` | 10 | How thick guides are drawn. Presentation only. |
| `categorical_colors` | true | A colour per trigger. `false` uses `preview_color` everywhere. |

`use_work_area` has been moved to individual zones. It is still read: on
load it is applied onto each `dock` that does not set its own and then dropped,
so an older configuration behaves exactly as before.

## Starting at logon

`windock autostart on`, or the tray menu, registers the executable under
`HKCU\Software\Microsoft\Windows\CurrentVersion\Run`. A stale entry pointing at an
older copy is repointed on next start.

That entry always starts windock at medium integrity, and a medium-integrity
process cannot call `SetWindowPos` on a window owned by an elevated one. The
drag is still seen and the guides are still drawn; the snap silently does
nothing. `windock autostart elevated`, from an elevated prompt, instead
registers a scheduled task with the highest privileges, which starts windock
elevated at logon without a UAC prompt each time. The installer offers it as a
tick box.

## Limitations

- Windows owned by an elevated process cannot be repositioned unless windock is
  also elevated. That is a Windows integrity-level rule; see [Starting at
  logon](#starting-at-logon) for how to start it elevated.
- One instance at a time, enforced with a named mutex.

## License

The original WinDock is distributed under a EULA by Ivan Yu that permits free
redistribution. This is an independent reimplementation from observed
behaviour; no original code is included.
