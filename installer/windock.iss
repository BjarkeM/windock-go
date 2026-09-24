; Inno Setup script for windock-go.
;
; Installs per-machine, under Program Files, so it needs elevation.
;
; The program's own state stays per-user regardless: the configuration under
; %LOCALAPPDATA% and the logon entry under HKCU. Anything below that touches
; either therefore runs as the user who started setup rather than as the
; administrator who approved it, or it lands in the wrong hive.
;
; Build with:  iscc installer\windock.iss
; (windock.exe must already be built: go build -o windock.exe .\cmd\windock)

#define AppName    "WinDock-Go"
#ifndef AppVersion
  #define AppVersion "0.1.0"
#endif
#define AppExe     "windock.exe"

[Setup]
; Never change this: it is the uninstall key name, so a new value would make
; Windows treat the next build as an unrelated product and orphan this one.
AppId=windock-go
AppName={#AppName}
AppVersion={#AppVersion}
AppPublisher=BjarkeM
AppPublisherURL=https://github.com/BjarkeM/windock-go
; {autopf} follows PrivilegesRequired: Program Files while that is admin.
DefaultDirName={autopf}\{#AppName}
DefaultGroupName={#AppName}
DisableProgramGroupPage=yes
PrivilegesRequired=admin
ArchitecturesAllowed=x64compatible
ArchitecturesInstallIn64BitMode=x64compatible
OutputDir=..\dist
OutputBaseFilename=windock-setup-{#AppVersion}
SetupIconFile=..\cmd\windock\windock.ico
UninstallDisplayIcon={app}\{#AppExe}
SolidCompression=yes
WizardStyle=modern

; Deliberately no AppMutex. Setting it would make Inno show its own
; "these applications must be closed" box, whose only choices are close-or-abort.
; A running copy is handled below.

[Files]
Source: "..\{#AppExe}"; DestDir: "{app}"; Flags: ignoreversion

[Icons]
Name: "{group}\{#AppName}"; Filename: "{app}\{#AppExe}"

[Tasks]
Name: autostart; Description: "Start {#AppName} when I log on"
; Elevation is a visible choice, declining leaves the ordinary Run entry, which starts
; windock at medium integrity. It will arrange ordinary windows and silently do
; nothing to windows owned by programs running as administrator.
Name: autostart\elevated; Description: "...with administrator rights, so it can also arrange windows of programs that run as administrator"

[Run]
; Startup is configured in CurStepChanged so failures are reported.

; Start it now the same way it will start from now on, so that what the user
; tries immediately after installing behaves like what they will get at logon.
Filename: "{app}\{#AppExe}"; Description: "Start {#AppName} now"; \
    Tasks: not autostart\elevated; Check: NotAlreadyRunning; \
    Flags: postinstall nowait skipifsilent runasoriginaluser
Filename: "{sys}\schtasks.exe"; Parameters: "/Run /TN ""WinDock-Go"""; \
    Description: "Start {#AppName} now"; \
    Tasks: autostart\elevated; Check: NotAlreadyRunning; \
    Flags: postinstall nowait skipifsilent runhidden runascurrentuser

[UninstallRun]
; Removing the program implies closing it, so this one does not ask.
; These run elevated, which is what removing the scheduled task needs anyway
; ([UninstallRun] does not accept runasoriginaluser). "autostart off" clears
; both mechanisms. It clears the right HKCU when an administrator uninstalls
; their own copy, since elevation keeps the same profile; under over-the-
; shoulder elevation it clears the elevating account's instead, leaving the
; original user a Run entry pointing at a deleted file.
Filename: "{app}\{#AppExe}"; Parameters: "exit"; Flags: runhidden; RunOnceId: "StopWinDock"
Filename: "{app}\{#AppExe}"; Parameters: "autostart off"; Flags: runhidden; RunOnceId: "ClearAutostart"

[Code]
var
  OldCopyLeftRunning: Boolean;
  StartupConfigured: Boolean;

function InstalledExe: String;
begin
  Result := ExpandConstant('{app}\{#AppExe}');
end;

// A running executable cannot be written to, but it can be renamed: the running
// process keeps its image through the rename and setup gets the name back. This
// is what lets "No" above still produce a complete installation.
procedure MoveOldExeAside;
var
  Exe: String;
begin
  Exe := InstalledExe;
  if not FileExists(Exe) then
    Exit;
  DeleteFile(Exe + '.old');        // left by a previous upgrade that was declined
  RenameFile(Exe, Exe + '.old');
  DeleteFile(Exe + '.old');        // succeeds unless that copy is still running
end;

procedure CurStepChanged(CurStep: TSetupStep);
var
  ResultCode: Integer;
begin
  if CurStep = ssPostInstall then
  begin
    StartupConfigured := True;
    if WizardIsTaskSelected('autostart\elevated') then
    begin
      if not Exec(InstalledExe, 'autostart elevated', '', SW_HIDE,
                  ewWaitUntilTerminated, ResultCode) then
        ResultCode := -1;
      StartupConfigured := ResultCode = 0;
    end
    else if WizardIsTaskSelected('autostart') then
    begin
      // Remove a previous elevated task before selecting ordinary startup.
      if not Exec(InstalledExe, 'autostart off', '', SW_HIDE,
                  ewWaitUntilTerminated, ResultCode) then
        ResultCode := -1;
      StartupConfigured := ResultCode = 0;
      if StartupConfigured then
      begin
        if not ExecAsOriginalUser(InstalledExe, 'autostart on', '', SW_HIDE,
                                  ewWaitUntilTerminated, ResultCode) then
          ResultCode := -1;
        StartupConfigured := ResultCode = 0;
      end;
    end;
    if not StartupConfigured then
      MsgBox('WinDock-Go was installed, but its logon startup could not be configured.' +
             '' + #13#10#13#10 + 'To see the error, open an administrator terminal and run:' +
             '' + #13#10 + '"' + InstalledExe + '" autostart elevated', mbError, MB_OK);
    Exit;
  end;

  if CurStep <> ssInstall then
    Exit;

  if CheckForMutexes('Local\WinDockGoSingleInstance') then
  begin
    if MsgBox('{#AppName} is running.' + #13#10#13#10 +
              'Close it now so that the new version starts straight away?' + #13#10#13#10 +
              'If you choose No the update still installs, but the running copy ' +
              'keeps the old version until you restart it or log on again.',
              mbConfirmation, MB_YESNO) = IDYES then
    begin
      if not Exec(InstalledExe, 'exit', '', SW_HIDE, ewWaitUntilTerminated, ResultCode) then
        ResultCode := -1;
      // A non-zero code means it did not stop within its grace period. Nothing
      // to do about that here; the rename below copes either way.
      OldCopyLeftRunning := ResultCode <> 0;
    end
    else
      OldCopyLeftRunning := True;
  end;

  MoveOldExeAside;
end;

// Offering "Start WinDock-Go now" while a copy is still running would only
// produce the single-instance error.
function NotAlreadyRunning: Boolean;
begin
  Result := not OldCopyLeftRunning and StartupConfigured;
end;

procedure CurPageChanged(CurPageID: Integer);
begin
  if (CurPageID = wpFinished) and OldCopyLeftRunning then
    WizardForm.FinishedLabel.Caption :=
      WizardForm.FinishedLabel.Caption + #13#10#13#10 +
      'The copy that was already running still has the previous version. ' +
      'It will pick up the new one when you restart it, or at your next logon.';
end;
