#ifndef AppVersion
  #define AppVersion "0.2.0-alpha.4"
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
OutputBaseFilename=memplua-{#AppVersion}-windows-x64-offline-setup
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
Source: "{#OfflinePayload}\memplua.exe"; DestDir: "{app}"; Flags: ignoreversion
Source: "{#OfflinePayload}\runtime\onnxruntime.dll"; DestDir: "{app}\runtime"; Flags: ignoreversion
Source: "{#OfflinePayload}\runtime\llama\*"; DestDir: "{app}\runtime\llama"; Flags: ignoreversion recursesubdirs createallsubdirs
Source: "{#OfflinePayload}\models\llm.gguf"; DestDir: "{app}\models"; Flags: ignoreversion
Source: "{#OfflinePayload}\models\whisper.bin"; DestDir: "{app}\models"; Flags: ignoreversion
Source: "{#OfflinePayload}\models\silero.onnx"; DestDir: "{app}\models"; Flags: ignoreversion
Source: "{#LibGomp}"; DestDir: "{app}"; Flags: ignoreversion
Source: "{#LibWinPThread}"; DestDir: "{app}"; Flags: ignoreversion
Source: "{#LibStdCpp}"; DestDir: "{app}"; Flags: ignoreversion

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
