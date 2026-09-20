#ifndef AppVersion
  #define AppVersion "0.2.0-alpha.2"
#endif
#ifndef NumericVersion
  #define NumericVersion "0.2.0.0"
#endif
#define AppName "memplua"
#define AppPublisher "memplua contributors"
#define AppExeName "memplua.exe"

[Setup]
AppId={{8BF0957B-84D8-4C4E-B85A-C3BBC8D0C01E}
AppName={#AppName}
AppVersion={#AppVersion}
AppPublisher={#AppPublisher}
VersionInfoVersion={#NumericVersion}
DefaultDirName={localappdata}\Programs\memplua
DefaultGroupName=memplua
DisableProgramGroupPage=yes
PrivilegesRequired=lowest
ArchitecturesAllowed=x64compatible
ArchitecturesInstallIn64BitMode=x64compatible
MinVersion=10.0.17763
OutputDir={#OutputDir}
OutputBaseFilename=memplua-{#AppVersion}-windows-x64-setup
Compression=lzma2/fast
SolidCompression=no
WizardStyle=modern
SetupLogging=yes
CloseApplications=yes
RestartApplications=no
UninstallDisplayIcon={app}\{#AppExeName}

[Languages]
Name: "english"; MessagesFile: "compiler:Default.isl"
Name: "russian"; MessagesFile: "compiler:Languages\Russian.isl"

[Tasks]
Name: "desktopicon"; Description: "{cm:CreateDesktopIcon}"; GroupDescription: "{cm:AdditionalIcons}"; Flags: unchecked

[Files]
Source: "{#LibGomp}"; DestDir: "{app}"; Flags: ignoreversion
Source: "{#LibWinPThread}"; DestDir: "{app}"; Flags: ignoreversion
Source: "{#LibStdCpp}"; DestDir: "{app}"; Flags: ignoreversion
Source: "..\..\scripts\release\download-windows-models.ps1"; DestDir: "{tmp}"; DestName: "download-models.ps1"; Flags: deleteafterinstall

#ifdef VCRedist
Source: "{#VCRedist}"; DestDir: "{tmp}"; DestName: "vc_redist.x64.exe"; Flags: deleteafterinstall
#endif
#ifdef WebView2Bootstrapper
Source: "{#WebView2Bootstrapper}"; DestDir: "{tmp}"; DestName: "MicrosoftEdgeWebview2Setup.exe"; Flags: deleteafterinstall
#endif

[Icons]
Name: "{autoprograms}\memplua"; Filename: "{app}\{#AppExeName}"
Name: "{autodesktop}\memplua"; Filename: "{app}\{#AppExeName}"; Tasks: desktopicon

[Run]
#ifdef VCRedist
Filename: "{tmp}\vc_redist.x64.exe"; Parameters: "/install /quiet /norestart"; StatusMsg: "Installing Microsoft Visual C++ Runtime..."; Flags: runhidden waituntilterminated
#endif
#ifdef WebView2Bootstrapper
Filename: "{tmp}\MicrosoftEdgeWebview2Setup.exe"; Parameters: "/silent /install"; StatusMsg: "Installing Microsoft Edge WebView2 Runtime..."; Flags: runhidden waituntilterminated
#endif
Filename: "{app}\{#AppExeName}"; Description: "{cm:LaunchProgram,memplua}"; Flags: nowait postinstall skipifsilent

[UninstallDelete]
Type: filesandordirs; Name: "{app}\models"
Type: filesandordirs; Name: "{app}\runtime"

[CustomMessages]
english.DownloadingModels=Downloading the latest memplua release and required local models. This can take several minutes.
english.ModelsDownloadFailed=memplua or required models could not be installed. Check model-install.log in the application directory, correct the network issue, and run the setup again.
russian.DownloadingModels=Загружается последний релиз memplua и необходимые локальные модели. Это может занять несколько минут.
russian.ModelsDownloadFailed=Не удалось установить memplua или необходимые модели. Откройте model-install.log в папке приложения, устраните проблему с сетью и запустите установщик снова.

[Code]
procedure CurStepChanged(CurStep: TSetupStep);
var
  ResultCode: Integer;
  Parameters: String;
begin
  if CurStep <> ssPostInstall then
    exit;

  WizardForm.StatusLabel.Caption := ExpandConstant('{cm:DownloadingModels}');
  Parameters := '-NoProfile -NonInteractive -ExecutionPolicy Bypass -File ' +
    AddQuotes(ExpandConstant('{tmp}\download-models.ps1')) + ' -ApplicationDirectory ' +
    AddQuotes(ExpandConstant('{app}')) + ' -LogPath ' +
    AddQuotes(ExpandConstant('{app}\model-install.log'));
  if not Exec(ExpandConstant('{sys}\WindowsPowerShell\v1.0\powershell.exe'), Parameters, '', SW_HIDE, ewWaitUntilTerminated, ResultCode) then
    RaiseException(ExpandConstant('{cm:ModelsDownloadFailed}'));
  if ResultCode <> 0 then
    RaiseException(ExpandConstant('{cm:ModelsDownloadFailed}'));
end;
