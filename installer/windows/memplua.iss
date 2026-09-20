#ifndef AppVersion
  #define AppVersion "0.2.0-alpha.2"
#endif
#ifndef NumericVersion
  #define NumericVersion "0.2.0.0"
#endif
#define AppName "memplua"
#define AppPublisher "memplua contributors"
#define AppExeName "memplua.exe"
#ifndef OfflinePayload
  #error OfflinePayload must point to the prepared offline application payload.
#endif

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
OutputBaseFilename=memplua-{#AppVersion}-windows-x64-offline-setup
Compression=lzma2/fast
SolidCompression=no
WizardStyle=modern
SetupLogging=yes
CloseApplications=yes
RestartApplications=no
SetupIconFile=memplua.ico
UninstallDisplayIcon={app}\memplua.ico

[Languages]
Name: "english"; MessagesFile: "compiler:Default.isl"
Name: "russian"; MessagesFile: "compiler:Languages\Russian.isl"

[Tasks]
Name: "desktopicon"; Description: "{cm:CreateDesktopIcon}"; GroupDescription: "{cm:AdditionalIcons}"; Flags: unchecked

[Files]
Source: "memplua.ico"; DestDir: "{app}"; Flags: ignoreversion
Source: "{#LibGomp}"; DestDir: "{app}"; Flags: ignoreversion
Source: "{#LibWinPThread}"; DestDir: "{app}"; Flags: ignoreversion
Source: "{#LibStdCpp}"; DestDir: "{app}"; Flags: ignoreversion
Source: "{#OfflinePayload}\*"; DestDir: "{app}"; Flags: ignoreversion recursesubdirs createallsubdirs

#ifdef VCRedist
Source: "{#VCRedist}"; DestDir: "{tmp}"; DestName: "vc_redist.x64.exe"; Flags: deleteafterinstall
#endif
#ifdef WebView2Bootstrapper
Source: "{#WebView2Bootstrapper}"; DestDir: "{tmp}"; DestName: "MicrosoftEdgeWebview2Setup.exe"; Flags: deleteafterinstall
#endif

[Icons]
Name: "{autoprograms}\memplua"; Filename: "{app}\{#AppExeName}"; IconFilename: "{app}\memplua.ico"
Name: "{autodesktop}\memplua"; Filename: "{app}\{#AppExeName}"; IconFilename: "{app}\memplua.ico"; Tasks: desktopicon

[Run]
#ifdef VCRedist
Filename: "{tmp}\vc_redist.x64.exe"; Parameters: "/install /quiet /norestart"; StatusMsg: "Installing Microsoft Visual C++ Runtime..."; Flags: runhidden waituntilterminated
#endif
#ifdef WebView2Bootstrapper
Filename: "{tmp}\MicrosoftEdgeWebview2Setup.exe"; Parameters: "/silent /install"; StatusMsg: "Installing Microsoft Edge WebView2 Runtime..."; Flags: runhidden waituntilterminated
#endif
Filename: "{app}\{#AppExeName}"; Description: "{cm:LaunchProgram,memplua}"; Flags: nowait postinstall skipifsilent; Check: CanLaunchMemplua

[UninstallDelete]
Type: filesandordirs; Name: "{app}\models"
Type: filesandordirs; Name: "{app}\runtime"

[Code]
var
  ApplicationInstalled: Boolean;

function CanLaunchMemplua: Boolean;
begin
  Result := ApplicationInstalled and FileExists(ExpandConstant('{app}\{#AppExeName}'));
end;

procedure InitializeWizard;
begin
end;

procedure CurStepChanged(CurStep: TSetupStep);
begin
  if CurStep = ssPostInstall then
    ApplicationInstalled := True;
end;
