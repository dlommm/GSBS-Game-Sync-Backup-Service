; GSBS server — Inno Setup script
; Build via script/packaging/windows/build-server-installer.sh

#define MyAppName "GSBS Server"
#define MyAppPublisher "GSBS"
#define MyAppURL "https://github.com/dlommm/GSBS-Game-Sync-Backup-Service"
#define MyAppExeName "gsbs-server-windows-amd64.exe"
#define MyLauncherName "gsbs-server-launcher.cmd"

[Setup]
AppId={{B8E1B12D-1E74-4E7D-915D-B71F8A6B9C34}
AppName={#MyAppName}
AppVersion={#MyAppVersion}
AppPublisher={#MyAppPublisher}
AppPublisherURL={#MyAppURL}
AppSupportURL={#MyAppURL}
AppUpdatesURL={#MyAppURL}/releases
DefaultDirName={autopf}\GSBS
DefaultGroupName=GSBS
OutputDir=.
OutputBaseFilename=gsbs-server-setup-{#MyAppVersion}-windows-amd64
Compression=lzma2/ultra64
SolidCompression=yes
WizardStyle=modern
PrivilegesRequired=admin
ArchitecturesAllowed=x64compatible
ArchitecturesInstallIn64BitMode=x64compatible
DisableDirPage=yes

[Languages]
Name: "english"; MessagesFile: "compiler:Default.isl"

[Tasks]
Name: "service"; Description: "Install and run GSBS Server as a Windows Service (recommended)"; GroupDescription: "Server startup:"; Flags: checkedonce

[Files]
Source: "{#SourceDir}\gsbs-server-windows-amd64.exe"; DestDir: "{app}"; DestName: "{#MyAppExeName}"; Flags: ignoreversion
Source: "{#SourceDir}\gsbs-server-launcher.cmd"; DestDir: "{app}"; DestName: "{#MyLauncherName}"; Flags: ignoreversion

[Icons]
Name: "{group}\GSBS Server (foreground)"; Filename: "{app}\{#MyLauncherName}"; Parameters: "run"
Name: "{group}\Open GSBS Admin"; Filename: "{app}\{#MyLauncherName}"; Parameters: "open-admin"
Name: "{group}\Service - Start"; Filename: "{app}\{#MyLauncherName}"; Parameters: "service-start"
Name: "{group}\Service - Stop"; Filename: "{app}\{#MyLauncherName}"; Parameters: "service-stop"
Name: "{group}\Service - Restart"; Filename: "{app}\{#MyLauncherName}"; Parameters: "service-restart"
Name: "{group}\Service - Status"; Filename: "{app}\{#MyLauncherName}"; Parameters: "service-status"
Name: "{group}\Edit GSBS Server config"; Filename: "{sys}\notepad.exe"; Parameters: """{commonappdata}\GSBS\server.env"""
Name: "{group}\Open GSBS config folder"; Filename: "{sys}\explorer.exe"; Parameters: """{commonappdata}\GSBS"""
Name: "{group}\Open GSBS logs folder"; Filename: "{app}\{#MyLauncherName}"; Parameters: "open-logs"
Name: "{group}\Uninstall {#MyAppName}"; Filename: "{uninstallexe}"

[Run]
Filename: "{app}\{#MyLauncherName}"; Parameters: "open-admin"; Description: "Open GSBS Admin in browser"; Flags: nowait postinstall skipifsilent unchecked

[Code]
const
  SecretCharset = 'abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789-_';

var
  CorePage: TWizardPage;
  RuntimePage: TWizardPage;
  PCGWPage: TWizardPage;
  RateLimitPage: TWizardPage;

  AddrEdit: TNewEdit;
  DbEdit: TNewEdit;
  SaveRootEdit: TNewEdit;
  SessionSecretEdit: TNewEdit;
  AdminUserEdit: TNewEdit;
  AllowRegisterCheck: TNewCheckBox;
  GenerateSecretButton: TNewButton;

  MaxStorageEdit: TNewEdit;
  ReadOnlyCheck: TNewCheckBox;
  LogLevelEdit: TNewEdit;
  TokenMaxAgeEdit: TNewEdit;
  MetricsCheck: TNewCheckBox;
  MetricsTokenEdit: TNewEdit;
  TrustProxyCheck: TNewCheckBox;

  PCGWCronEdit: TNewEdit;
  PCGWFullCronEdit: TNewEdit;
  PCGWRateLimitEdit: TNewEdit;
  PCGWUserAgentEdit: TNewEdit;
  PCGWStoreFullWikitextCheck: TNewCheckBox;
  PCGWMaxPagesEdit: TNewEdit;

  RateLimitAuthEdit: TNewEdit;
  RateLimitPushEdit: TNewEdit;
  RateLimitPullEdit: TNewEdit;
  RateLimitManifestEdit: TNewEdit;
  RateLimitGeneralEdit: TNewEdit;

function AddHeading(Page: TWizardPage; const Text: String; var Top: Integer): TNewStaticText;
begin
  Result := TNewStaticText.Create(Page.Surface);
  Result.Parent := Page.Surface;
  Result.Left := 0;
  Result.Top := Top;
  Result.Width := Page.SurfaceWidth;
  Result.WordWrap := True;
  Result.AutoSize := False;
  Result.Height := ScaleY(34);
  Result.Caption := Text;
  Top := Result.Top + Result.Height + ScaleY(6);
end;

function AddLabeledEdit(Page: TWizardPage; const LabelText: String; const DefaultValue: String; const HintText: String; var Top: Integer; IsPassword: Boolean): TNewEdit;
var
  LabelControl: TNewStaticText;
begin
  LabelControl := TNewStaticText.Create(Page.Surface);
  LabelControl.Parent := Page.Surface;
  LabelControl.Left := 0;
  LabelControl.Top := Top;
  LabelControl.Caption := LabelText;

  Result := TNewEdit.Create(Page.Surface);
  Result.Parent := Page.Surface;
  Result.Left := 0;
  Result.Top := LabelControl.Top + LabelControl.Height + ScaleY(2);
  Result.Width := Page.SurfaceWidth;
  Result.Text := DefaultValue;
  Result.Hint := HintText;
  Result.ShowHint := True;
  if IsPassword then
    Result.PasswordChar := '*';

  Top := Result.Top + Result.Height + ScaleY(8);
end;

function AddLabeledCheck(Page: TWizardPage; const LabelText: String; const HintText: String; var Top: Integer): TNewCheckBox;
begin
  Result := TNewCheckBox.Create(Page.Surface);
  Result.Parent := Page.Surface;
  Result.Left := 0;
  Result.Top := Top;
  Result.Width := Page.SurfaceWidth;
  Result.Caption := LabelText;
  Result.Hint := HintText;
  Result.ShowHint := True;

  Top := Result.Top + Result.Height + ScaleY(8);
end;

function BoolToEnv(Value: Boolean): String;
begin
  if Value then
    Result := 'true'
  else
    Result := 'false';
end;

function EnabledOrEmpty(Value: Boolean): String;
begin
  if Value then
    Result := 'true'
  else
    Result := '';
end;

function GenerateSecret(LengthValue: Integer): String;
var
  I: Integer;
begin
  Result := '';
  for I := 1 to LengthValue do
    Result := Result + SecretCharset[Random(Length(SecretCharset)) + 1];
end;

procedure GenerateSecretClick(Sender: TObject);
begin
  SessionSecretEdit.Text := GenerateSecret(64);
end;

function ConfigDir(): String;
begin
  Result := ExpandConstant('{commonappdata}\GSBS');
end;

function ConfigPath(): String;
begin
  Result := ConfigDir() + '\server.env';
end;

function BuildEnvContent(): String;
var
  NowValue: String;
begin
  NowValue := GetDateTimeString('yyyy-mm-dd hh:nn:ss', '-', ':');
  Result :=
    '# GSBS Server environment configuration' + #13#10 +
    '# Generated by GSBS Server Installer on ' + NowValue + #13#10 +
    '# This file is stored in ProgramData so it survives app upgrades/uninstall.' + #13#10 +
    '# Keep GSBS_SESSION_SECRET and GSBS_METRICS_TOKEN private.' + #13#10 +
    '' + #13#10 +
    'GSBS_ADDR=' + Trim(AddrEdit.Text) + #13#10 +
    'GSBS_DB=' + Trim(DbEdit.Text) + #13#10 +
    'GSBS_SAVE_ROOT=' + Trim(SaveRootEdit.Text) + #13#10 +
    'GSBS_SESSION_SECRET=' + Trim(SessionSecretEdit.Text) + #13#10 +
    'GSBS_ADMIN_USERNAME=' + Trim(AdminUserEdit.Text) + #13#10 +
    'GSBS_ALLOW_REGISTER=' + BoolToEnv(AllowRegisterCheck.Checked) + #13#10 +
    'GSBS_MAX_STORAGE_BYTES=' + Trim(MaxStorageEdit.Text) + #13#10 +
    'GSBS_READ_ONLY=' + BoolToEnv(ReadOnlyCheck.Checked) + #13#10 +
    'GSBS_LOG_LEVEL=' + Trim(LogLevelEdit.Text) + #13#10 +
    'GSBS_SERVICE_LOG_PATH=' + ExpandConstant('{commonappdata}\GSBS\logs\server.log') + #13#10 +
    'GSBS_TOKEN_MAX_AGE=' + Trim(TokenMaxAgeEdit.Text) + #13#10 +
    '' + #13#10 +
    'GSBS_PCGW_CRON=' + Trim(PCGWCronEdit.Text) + #13#10 +
    'GSBS_PCGW_FULL_CRON=' + Trim(PCGWFullCronEdit.Text) + #13#10 +
    'GSBS_PCGW_RATE_LIMIT=' + Trim(PCGWRateLimitEdit.Text) + #13#10 +
    'GSBS_PCGW_USER_AGENT=' + Trim(PCGWUserAgentEdit.Text) + #13#10 +
    'GSBS_PCGW_STORE_FULL_WIKITEXT=' + BoolToEnv(PCGWStoreFullWikitextCheck.Checked) + #13#10 +
    'GSBS_PCGW_MAX_PAGES_PER_RUN=' + Trim(PCGWMaxPagesEdit.Text) + #13#10 +
    '' + #13#10 +
    'GSBS_METRICS=' + BoolToEnv(MetricsCheck.Checked) + #13#10 +
    'GSBS_METRICS_TOKEN=' + Trim(MetricsTokenEdit.Text) + #13#10 +
    'GSBS_TRUST_PROXY=' + EnabledOrEmpty(TrustProxyCheck.Checked) + #13#10 +
    '' + #13#10 +
    'GSBS_RATE_LIMIT_AUTH=' + Trim(RateLimitAuthEdit.Text) + #13#10 +
    'GSBS_RATE_LIMIT_PUSH=' + Trim(RateLimitPushEdit.Text) + #13#10 +
    'GSBS_RATE_LIMIT_PULL=' + Trim(RateLimitPullEdit.Text) + #13#10 +
    'GSBS_RATE_LIMIT_MANIFEST=' + Trim(RateLimitManifestEdit.Text) + #13#10 +
    'GSBS_RATE_LIMIT_GENERAL=' + Trim(RateLimitGeneralEdit.Text) + #13#10;
end;

function WriteServerEnv(): Boolean;
var
  ExistingPath: String;
  Response: Integer;
begin
  Result := True;

  ExistingPath := ConfigPath();
  if FileExists(ExistingPath) and (not WizardSilent) then begin
    Response := MsgBox(
      'An existing server config was found at:' + #13#10 + ExistingPath + #13#10 + #13#10 +
      'Do you want to overwrite it with values from this wizard?' + #13#10 +
      'Choose No to keep the existing config.',
      mbConfirmation,
      MB_YESNO
    );
    if Response = IDNO then begin
      Log('Keeping existing config file: ' + ExistingPath);
      Exit;
    end;
  end;

  if not ForceDirectories(ConfigDir()) then begin
    MsgBox('Failed to create config directory: ' + ConfigDir(), mbCriticalError, MB_OK);
    Result := False;
    Exit;
  end;

  if not SaveStringToFile(ExistingPath, BuildEnvContent(), False) then begin
    MsgBox('Failed to write config file: ' + ExistingPath, mbCriticalError, MB_OK);
    Result := False;
    Exit;
  end;
end;

function RunLauncherAction(const Action: String; IgnoreExitCode: Boolean): Boolean;
var
  ResultCode: Integer;
  Params: String;
begin
  Params := '/C ""' + ExpandConstant('{app}\{#MyLauncherName}') + '" ' + Action + '"';
  Result := Exec(ExpandConstant('{cmd}'), Params, '', SW_HIDE, ewWaitUntilTerminated, ResultCode);
  if not Result then
    Exit;
  if IgnoreExitCode then
    Result := True
  else
    Result := (ResultCode = 0);
end;

function InstallAndStartService(): Boolean;
var
  ResultCode: Integer;
begin
  Result := RunLauncherAction('service-install', False);
  if not Result then begin
    MsgBox(
      'GSBS was installed, but service installation failed.' + #13#10 + #13#10 +
      'The installer expects gsbs-server service commands to be available. ' +
      'Open an elevated Command Prompt and run:' + #13#10 +
      '"' + ExpandConstant('{app}\{#MyLauncherName}') + '" service-install',
      mbError,
      MB_OK
    );
    Exit;
  end;

  if not RunLauncherAction('service-start', False) then begin
    MsgBox(
      'GSBS service was installed but failed to start automatically.' + #13#10 + #13#10 +
      'Use Start Menu shortcut "Service - Start" or run:' + #13#10 +
      '"' + ExpandConstant('{app}\{#MyLauncherName}') + '" service-start',
      mbError,
      MB_OK
    );
    Result := False;
    Exit;
  end;

  Exec(
    ExpandConstant('{cmd}'),
    '/C sc description GSBSServer "Game Sync and Backup Service server" >nul 2>&1',
    '',
    SW_HIDE,
    ewWaitUntilTerminated,
    ResultCode
  );
  Exec(
    ExpandConstant('{cmd}'),
    '/C sc config GSBSServer start= auto >nul 2>&1',
    '',
    SW_HIDE,
    ewWaitUntilTerminated,
    ResultCode
  );
  Result := True;
end;

procedure StopAndRemoveService();
var
  ResultCode: Integer;
begin
  RunLauncherAction('service-stop', True);
  if not RunLauncherAction('service-remove', True) then begin
    Exec(
      ExpandConstant('{cmd}'),
      '/C sc stop GSBSServer >nul 2>&1',
      '',
      SW_HIDE,
      ewWaitUntilTerminated,
      ResultCode
    );
    Exec(
      ExpandConstant('{cmd}'),
      '/C sc delete GSBSServer >nul 2>&1',
      '',
      SW_HIDE,
      ewWaitUntilTerminated,
      ResultCode
    );
  end;
end;

function NextButtonClick(CurPageID: Integer): Boolean;
begin
  Result := True;

  if CurPageID = CorePage.ID then begin
    if Trim(AddrEdit.Text) = '' then begin
      MsgBox('GSBS_ADDR is required (for example: :8080).', mbError, MB_OK);
      Result := False;
      Exit;
    end;

    if Trim(DbEdit.Text) = '' then begin
      MsgBox('GSBS_DB is required (for example: C:\ProgramData\GSBS\gsbs.db).', mbError, MB_OK);
      Result := False;
      Exit;
    end;

    if Trim(SessionSecretEdit.Text) = '' then begin
      MsgBox('GSBS_SESSION_SECRET is required. Use "Generate" or enter a secure value manually.', mbError, MB_OK);
      Result := False;
      Exit;
    end;
  end;

  if (CurPageID = RuntimePage.ID) and MetricsCheck.Checked and (Trim(MetricsTokenEdit.Text) = '') then begin
    MsgBox('GSBS_METRICS_TOKEN is required when metrics are enabled.', mbError, MB_OK);
    Result := False;
    Exit;
  end;
end;

procedure InitializeWizard();
var
  Top: Integer;
begin
  CorePage := CreateCustomPage(
    wpSelectTasks,
    'Server basics',
    'Core GSBS settings used at startup'
  );
  Top := ScaleY(4);
  AddHeading(
    CorePage,
    'Configure listen address, database path, storage root, and authentication defaults. ' +
    'Hover fields for format tips. Use ProgramData paths unless you need custom locations.',
    Top
  );

  AddrEdit := AddLabeledEdit(CorePage, 'GSBS_ADDR (required)', ':8080', 'Listen address. Example: :8080 or 0.0.0.0:8080', Top, False);
  DbEdit := AddLabeledEdit(CorePage, 'GSBS_DB (required)', ExpandConstant('{commonappdata}\GSBS\gsbs.db'), 'SQLite database file path', Top, False);
  SaveRootEdit := AddLabeledEdit(CorePage, 'GSBS_SAVE_ROOT', ExpandConstant('{commonappdata}\GSBS\gamesaves'), 'Optional filesystem save store directory', Top, False);
  SessionSecretEdit := AddLabeledEdit(CorePage, 'GSBS_SESSION_SECRET (required)', '', 'Use a long random value for production', Top, True);

  GenerateSecretButton := TNewButton.Create(CorePage.Surface);
  GenerateSecretButton.Parent := CorePage.Surface;
  GenerateSecretButton.Left := 0;
  GenerateSecretButton.Top := SessionSecretEdit.Top + SessionSecretEdit.Height + ScaleY(2);
  GenerateSecretButton.Caption := 'Generate secure secret';
  GenerateSecretButton.OnClick := @GenerateSecretClick;
  Top := GenerateSecretButton.Top + GenerateSecretButton.Height + ScaleY(8);

  AdminUserEdit := AddLabeledEdit(CorePage, 'GSBS_ADMIN_USERNAME', '', 'Optional admin username restriction for /admin', Top, False);
  AllowRegisterCheck := AddLabeledCheck(CorePage, 'GSBS_ALLOW_REGISTER (allow new registrations)', 'Uncheck in production after creating accounts', Top);
  AllowRegisterCheck.Checked := True;

  SessionSecretEdit.Text := GenerateSecret(64);

  RuntimePage := CreateCustomPage(
    CorePage.ID,
    'Runtime and security',
    'Storage caps, logging, token age, metrics, and proxy handling'
  );
  Top := ScaleY(4);
  AddHeading(
    RuntimePage,
    'Choose runtime limits and security controls. Enable read-only mode for maintenance windows. ' +
    'Metrics token is mandatory when metrics are enabled.',
    Top
  );

  MaxStorageEdit := AddLabeledEdit(RuntimePage, 'GSBS_MAX_STORAGE_BYTES', '0', '0 or empty means unlimited', Top, False);
  ReadOnlyCheck := AddLabeledCheck(RuntimePage, 'GSBS_READ_ONLY (disable push/delete writes)', 'Allows pulls and read endpoints only', Top);
  ReadOnlyCheck.Checked := False;
  LogLevelEdit := AddLabeledEdit(RuntimePage, 'GSBS_LOG_LEVEL', 'info', 'debug, info, warn, error', Top, False);
  TokenMaxAgeEdit := AddLabeledEdit(RuntimePage, 'GSBS_TOKEN_MAX_AGE', '2160h', 'Client token max age. Example: 2160h (90 days)', Top, False);
  MetricsCheck := AddLabeledCheck(RuntimePage, 'GSBS_METRICS (enable /metrics endpoint)', 'Enable metrics endpoint exposure', Top);
  MetricsCheck.Checked := False;
  MetricsTokenEdit := AddLabeledEdit(RuntimePage, 'GSBS_METRICS_TOKEN', '', 'Required when metrics are enabled', Top, True);
  TrustProxyCheck := AddLabeledCheck(RuntimePage, 'GSBS_TRUST_PROXY (trust X-Forwarded-For)', 'Enable only behind a trusted reverse proxy', Top);
  TrustProxyCheck.Checked := False;

  PCGWPage := CreateCustomPage(
    RuntimePage.ID,
    'PCGamingWiki sync settings',
    'Background manifest refresh schedule and fetch behavior'
  );
  Top := ScaleY(4);
  AddHeading(
    PCGWPage,
    'Tune how GSBS syncs PCGamingWiki data. Leave defaults for most installs. ' +
    'Blank GSBS_PCGW_FULL_CRON disables full periodic resync.',
    Top
  );

  PCGWCronEdit := AddLabeledEdit(PCGWPage, 'GSBS_PCGW_CRON', '0 3 * * 0', 'Incremental sync cron schedule', Top, False);
  PCGWFullCronEdit := AddLabeledEdit(PCGWPage, 'GSBS_PCGW_FULL_CRON', '', 'Optional full sync cron schedule', Top, False);
  PCGWRateLimitEdit := AddLabeledEdit(PCGWPage, 'GSBS_PCGW_RATE_LIMIT', '2s', 'Delay between PCGW requests', Top, False);
  PCGWUserAgentEdit := AddLabeledEdit(PCGWPage, 'GSBS_PCGW_USER_AGENT', 'GSBS Windows Installer (+https://github.com/dlommm/GSBS-Game-Sync-Backup-Service)', 'Custom User-Agent for PCGW requests', Top, False);
  PCGWStoreFullWikitextCheck := AddLabeledCheck(PCGWPage, 'GSBS_PCGW_STORE_FULL_WIKITEXT', 'Stores compressed full-page wikitext for debugging/forensics', Top);
  PCGWStoreFullWikitextCheck.Checked := True;
  PCGWMaxPagesEdit := AddLabeledEdit(PCGWPage, 'GSBS_PCGW_MAX_PAGES_PER_RUN', '5000', 'Ingest budget per sync run', Top, False);

  RateLimitPage := CreateCustomPage(
    PCGWPage.ID,
    'API rate limits',
    'Request budgets by endpoint class (format: requests,window)'
  );
  Top := ScaleY(4);
  AddHeading(
    RateLimitPage,
    'Rate limit format is "count,window". Examples: 20,1m or 300,5m. ' +
    'Use stricter auth limits on internet-exposed deployments.',
    Top
  );

  RateLimitAuthEdit := AddLabeledEdit(RateLimitPage, 'GSBS_RATE_LIMIT_AUTH', '20,1m', 'Per-IP auth attempts', Top, False);
  RateLimitPushEdit := AddLabeledEdit(RateLimitPage, 'GSBS_RATE_LIMIT_PUSH', '120,1m', 'Per-user push requests', Top, False);
  RateLimitPullEdit := AddLabeledEdit(RateLimitPage, 'GSBS_RATE_LIMIT_PULL', '60,1m', 'Per-user pull requests', Top, False);
  RateLimitManifestEdit := AddLabeledEdit(RateLimitPage, 'GSBS_RATE_LIMIT_MANIFEST', '60,1m', 'Manifest endpoint rate limit', Top, False);
  RateLimitGeneralEdit := AddLabeledEdit(RateLimitPage, 'GSBS_RATE_LIMIT_GENERAL', '300,1m', 'Fallback API limit per user', Top, False);
end;

procedure CurStepChanged(CurStep: TSetupStep);
begin
  if CurStep = ssPostInstall then begin
    if not WriteServerEnv() then
      RaiseException('Could not write server configuration.');

    if WizardIsTaskSelected('service') then
      InstallAndStartService();
  end;
end;

procedure CurUninstallStepChanged(CurUninstallStep: TUninstallStep);
begin
  if CurUninstallStep = usUninstall then
    StopAndRemoveService();
end;
