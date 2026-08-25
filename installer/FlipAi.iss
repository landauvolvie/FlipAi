#ifndef MyVersion
  #define MyVersion "0.13.0"
#endif
#ifndef SourceDir
  #define SourceDir "..\dist"
#endif

[Setup]
AppId={{9F6EC557-FA07-4E99-A06A-3D9F8C2F7D73}
AppName=FlipAi
AppVersion={#MyVersion}
AppVerName=FlipAi {#MyVersion}
AppPublisher=FlipAi
AppPublisherURL=https://github.com/landauvolvie/FlipAi
AppSupportURL=https://github.com/landauvolvie/FlipAi/issues
AppUpdatesURL=https://github.com/landauvolvie/FlipAi/releases
DefaultDirName={localappdata}\Programs\FlipAi
DefaultGroupName=FlipAi
DisableProgramGroupPage=yes
PrivilegesRequired=lowest
WizardStyle=modern
OutputDir=..\installer-output
OutputBaseFilename=FlipAi-Setup-v{#MyVersion}
SetupIconFile={#SourceDir}\FlipAi.ico
UninstallDisplayIcon={app}\FlipAi.ico
UninstallDisplayName=FlipAi
Compression=lzma2
SolidCompression=yes
CloseApplications=yes
RestartApplications=no
ArchitecturesAllowed=x64compatible
MinVersion=10.0.17763
UsePreviousAppDir=yes
UsePreviousTasks=yes

[Tasks]
Name: "startup"; Description: "Start FlipAi automatically when I sign in"; GroupDescription: "Windows startup:"; Flags: checkedonce

[Files]
Source: "{#SourceDir}\FlipAi.exe"; DestDir: "{app}"; Flags: ignoreversion
Source: "{#SourceDir}\FlipAi.ico"; DestDir: "{app}"; Flags: ignoreversion

[Icons]
Name: "{autoprograms}\FlipAi"; Filename: "{app}\FlipAi.exe"; WorkingDir: "{app}"; IconFilename: "{app}\FlipAi.ico"; Comment: "Open FlipAi settings"

[Registry]
Root: HKCU; Subkey: "Software\Microsoft\Windows\CurrentVersion\Run"; ValueType: string; ValueName: "FlipAi"; ValueData: """{app}\FlipAi.exe"" --watchdog"; Tasks: startup; Flags: uninsdeletevalue

[InstallDelete]
Type: filesandordirs; Name: "{localappdata}\Programs\AISMSBridge"

[Run]
Filename: "{app}\FlipAi.exe"; Description: "Launch FlipAi and complete setup"; WorkingDir: "{app}"; Flags: nowait postinstall skipifsilent
; A silent run is how FlipAi updates itself from inside the app: there is no
; finish page to click, so FlipAi has to be started again here. Only the app
; asks for that, so a plain silent install (packaging tests, scripted
; deployment) still leaves nothing running.
;
; /restartapp=1 is an update the user started from inside the app: run the
; launcher with no arguments so the watchdog comes back AND the FlipAi window
; opens again. Passing --watchdog here instead restored the tray and the
; background bridge but never reopened the window, which is why an in-app
; update looked like the app simply never came back.
Filename: "{app}\FlipAi.exe"; WorkingDir: "{app}"; Flags: nowait; Check: RestartWithWindow
; /restartapp=2 is an unattended automatic update. Nobody is waiting at the
; screen, so restore the background bridge and tray without stealing focus.
Filename: "{app}\FlipAi.exe"; Parameters: "--resume"; WorkingDir: "{app}"; Flags: nowait runhidden; Check: RestartBridgeOnly

[UninstallRun]
Filename: "{app}\FlipAi.exe"; Parameters: "--quit"; Flags: runhidden waituntilterminated skipifdoesntexist; RunOnceId: "StopFlipAi"

[UninstallDelete]
Type: filesandordirs; Name: "{localappdata}\AISMSBridge"

[Code]
const
  UninstallKey = 'Software\Microsoft\Windows\CurrentVersion\Uninstall\{9F6EC557-FA07-4E99-A06A-3D9F8C2F7D73}_is1';
  RunKey = 'Software\Microsoft\Windows\CurrentVersion\Run';

var
  PriorVersion: String;
  PriorStartup: String;
  IsUpdate: Boolean;

procedure SignalBridgeToQuit(const Reason: String);
var
  DataDir: String;
  QuitFile: String;
begin
  DataDir := ExpandConstant('{localappdata}\AISMSBridge');
  ForceDirectories(DataDir);
  QuitFile := AddBackslash(DataDir) + 'quit.flag';
  SaveStringToFile(QuitFile, Reason, False);
end;

{ Stop FlipAi and wait for it to really be gone. The flag alone only files a
  request; FlipAi.exe --quit waits until nothing is answering and the Google
  Voice window has closed. Without that wait Setup began replacing FlipAi.exe
  and deleting the data folder while both were still open. }
procedure StopFlipAiAndWait(const Reason: String);
var
  Exe: String;
  ResultCode: Integer;
begin
  SignalBridgeToQuit(Reason);
  Exe := ExpandConstant('{app}\FlipAi.exe');
  if FileExists(Exe) then
  begin
    if not Exec(Exe, '--quit', '', SW_HIDE, ewWaitUntilTerminated, ResultCode) then
      Sleep(4000);
  end
  else
    Sleep(3000);
end;

{ An existing install is an update, not a first run. Setup detects it up front
  so the wizard can skip every question the user already answered. }
function InitializeSetup(): Boolean;
begin
  IsUpdate := RegQueryStringValue(HKCU, UninstallKey, 'DisplayVersion', PriorVersion);
  if not RegQueryStringValue(HKCU, RunKey, 'FlipAi', PriorStartup) then
    PriorStartup := '';
  Result := True;
end;

{ True only when FlipAi itself launched this installer to update in place.
  1 = the user pressed Install in the app, so reopen the window too.
  2 = an automatic background update, so restore the bridge silently. }
function RestartWithWindow(): Boolean;
begin
  Result := ExpandConstant('{param:restartapp|0}') = '1';
end;

function RestartBridgeOnly(): Boolean;
begin
  Result := ExpandConstant('{param:restartapp|0}') = '2';
end;

{ Kept so a Setup EXE built from this script still answers the old name. }
function RestartRequested(): Boolean;
begin
  Result := RestartWithWindow() or RestartBridgeOnly();
end;

function ShouldSkipPage(PageID: Integer): Boolean;
begin
  Result := IsUpdate and
    ((PageID = wpWelcome) or (PageID = wpSelectDir) or (PageID = wpSelectProgramGroup) or
     (PageID = wpSelectTasks) or (PageID = wpReady));
end;

procedure CurPageChanged(CurPageID: Integer);
begin
  if (CurPageID = wpFinished) and IsUpdate then
    WizardForm.FinishedLabel.Caption :=
      'FlipAi was updated from ' + PriorVersion + ' to {#MyVersion}.' + #13#10#13#10 +
      'Your Gmail connection, allowed phone numbers, security code, and agent settings were kept. Nothing had to be set up again.';
end;

function PrepareToInstall(var NeedsRestart: Boolean): String;
begin
  { Stop the running bridge, whichever build started it, and wait for it. }
  StopFlipAiAndWait('installer upgrade');

  { Remove both the old and new per-user startup values before writing the one
    this install should have. PriorStartup remembers what was there so an
    update never silently turns off a startup the user had enabled. }
  RegDeleteValue(HKCU, RunKey, 'AISMSBridge');
  RegDeleteValue(HKCU, RunKey, 'FlipAi');
  Result := '';
end;

procedure CurStepChanged(CurStep: TSetupStep);
begin
  if CurStep = ssPostInstall then
  begin
    { Setup wrote this flag to stop the old build. Leaving it behind would tell
      the freshly installed one to stop as soon as it started. }
    DeleteFile(AddBackslash(ExpandConstant('{localappdata}\AISMSBridge')) + 'quit.flag');
    if (PriorStartup <> '') and not WizardIsTaskSelected('startup') then
      RegWriteStringValue(HKCU, RunKey, 'FlipAi',
        '"' + ExpandConstant('{app}\FlipAi.exe') + '" --watchdog');
  end;
end;

procedure CurUninstallStepChanged(CurUninstallStep: TUninstallStep);
begin
  if CurUninstallStep = usUninstall then
  begin
    StopFlipAiAndWait('uninstall');
    RegDeleteValue(HKCU, RunKey, 'AISMSBridge');
    RegDeleteValue(HKCU, RunKey, 'FlipAi');
  end;
end;
