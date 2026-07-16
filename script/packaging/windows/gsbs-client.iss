; GSBS client — Inno Setup script
; Build via script/packaging/windows/build-installer.sh

#define MyAppName "GSBS Client"
#define MyAppPublisher "GSBS"
#define MyAppURL "https://github.com/dlommm/GSBS-Game-Sync-Backup-Service"
#define MyAppExeName "gsbs-client.exe"

[Setup]
AppId={{A1B2C3D4-E5F6-7890-ABCD-EF1234567890}
AppName={#MyAppName}
AppVersion={#MyAppVersion}
AppPublisher={#MyAppPublisher}
AppPublisherURL={#MyAppURL}
AppSupportURL={#MyAppURL}
AppUpdatesURL={#MyAppURL}/releases
DefaultDirName={autopf}\GSBS
DefaultGroupName=GSBS
DisableProgramGroupPage=yes
OutputDir=.
OutputBaseFilename=gsbs-client-setup-{#MyAppVersion}-windows-amd64
Compression=lzma2/ultra64
SolidCompression=yes
WizardStyle=modern
PrivilegesRequired=lowest
ArchitecturesAllowed=x64compatible
ArchitecturesInstallIn64BitMode=x64compatible
; Detect a running tray client before overwriting its exe. The client creates
; a session-Local mutex for the single-instance decision plus a Global one
; purely as this beacon (see client/single_instance_windows.go).
AppMutex=Local\GSBSClientSingleInstance,Global\GSBSClientSingleInstance
CloseApplications=yes
RestartApplications=no

; --- Branding (assets generated from assets/images/ by cmd/gen-branding) ---
SetupIconFile=..\..\..\client\icon.ico
WizardImageFile=branding\wizard-large.bmp
WizardSmallImageFile=branding\wizard-small.bmp
WizardImageStretch=yes
DisableWelcomePage=no

; --- Installer version metadata + Programs & Features entry ---
VersionInfoVersion={#MyAppVersion}
VersionInfoCompany={#MyAppPublisher}
VersionInfoProductName={#MyAppName}
VersionInfoProductVersion={#MyAppVersion}
VersionInfoDescription=GSBS game-save sync client setup
UninstallDisplayName={#MyAppName}
UninstallDisplayIcon={app}\{#MyAppExeName}

[Languages]
Name: "english"; MessagesFile: "compiler:Default.isl"

[Tasks]
Name: "startmenuicon"; Description: "Create a Start Menu shortcut"; GroupDescription: "Shortcuts:"
Name: "desktopicon"; Description: "Create a desktop shortcut"; GroupDescription: "Shortcuts:"; Flags: unchecked
Name: "autostart"; Description: "Run GSBS Client at Windows startup"; GroupDescription: "Startup:"

[Files]
Source: "{#SourceDir}\gsbs-client-windows-amd64.exe"; DestDir: "{app}"; DestName: "{#MyAppExeName}"; Flags: ignoreversion

[Icons]
; AppUserModelID must match beeep.AppName ("GSBS", set in client/main.go):
; toasts from an unregistered AppID can be silently dropped on Win10/11.
Name: "{group}\{#MyAppName}"; Filename: "{app}\{#MyAppExeName}"; Parameters: "--minimized"; AppUserModelID: "GSBS"; Tasks: startmenuicon
Name: "{group}\Uninstall {#MyAppName}"; Filename: "{uninstallexe}"; Tasks: startmenuicon
Name: "{autodesktop}\{#MyAppName}"; Filename: "{app}\{#MyAppExeName}"; Parameters: "--minimized"; AppUserModelID: "GSBS"; Tasks: desktopicon

[Registry]
Root: HKCU; Subkey: "Software\Microsoft\Windows\CurrentVersion\Run"; ValueType: string; ValueName: "GSBS"; ValueData: """{app}\{#MyAppExeName}"" --minimized"; Flags: uninsdeletevalue; Tasks: autostart

[Run]
Filename: "{app}\{#MyAppExeName}"; Description: "Launch {#MyAppName}"; Parameters: "--minimized"; Flags: nowait postinstall skipifsilent
