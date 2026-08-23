#ifndef MyVersion
  #define MyVersion "0.9.0"
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

[UninstallRun]
Filename: "{app}\FlipAi.exe"; Parameters: "--quit"; Flags: runhidden waituntilterminated skipifdoesntexist; RunOnceId: "StopFlipAi"

[UninstallDelete]
Type: filesandordirs; Name: "{localappdata}\AISMSBridge"

[Code]
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

function PrepareToInstall(var NeedsRestart: Boolean): String;
begin
  { Stop old portable/background builds regardless of where their EXE was run. }
  SignalBridgeToQuit('installer upgrade');
  Sleep(3000);

  { Remove both the old and new per-user startup values before writing the one
    selected on this install. This prevents duplicate watchdogs on upgrades. }
  RegDeleteValue(HKCU, 'Software\Microsoft\Windows\CurrentVersion\Run', 'AISMSBridge');
  RegDeleteValue(HKCU, 'Software\Microsoft\Windows\CurrentVersion\Run', 'FlipAi');
  Result := '';
end;

procedure CurUninstallStepChanged(CurUninstallStep: TUninstallStep);
begin
  if CurUninstallStep = usUninstall then
  begin
    SignalBridgeToQuit('uninstall');
    Sleep(1500);
    RegDeleteValue(HKCU, 'Software\Microsoft\Windows\CurrentVersion\Run', 'AISMSBridge');
    RegDeleteValue(HKCU, 'Software\Microsoft\Windows\CurrentVersion\Run', 'FlipAi');
  end;
end;
