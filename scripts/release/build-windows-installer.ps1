param(
    [string]$Version = "0.2.0-alpha.2",
    [switch]$SkipApplicationBuild,
    [switch]$SkipFrontend,
    [string]$WhisperRoot = "",
    [string]$VCRedist = "",
    [string]$WebView2Bootstrapper = "",
    [string]$InnoCompiler = "",
    [string]$OutputDirectory = ""
)

$ErrorActionPreference = "Stop"
$ProjectRoot = [IO.Path]::GetFullPath((Join-Path $PSScriptRoot "../.."))
$InstallerScript = Join-Path $ProjectRoot "installer/windows/memplua.iss"
$BuildDirectory = Join-Path $ProjectRoot "build/windows"
$ApplicationExecutable = Join-Path $BuildDirectory "memplua.exe"
if (-not $OutputDirectory) { $OutputDirectory = Join-Path $ProjectRoot "dist/windows" }
$OutputDirectory = [IO.Path]::GetFullPath($OutputDirectory)
if ($Version -notmatch '^(\d+)\.(\d+)\.(\d+)') {
    throw "Version must start with three numeric components, for example 0.2.0-alpha.2."
}
$NumericVersion = "$($Matches[1]).$($Matches[2]).$($Matches[3]).0"

function Require-File([string]$Name, [string]$Path) {
    if ([string]::IsNullOrWhiteSpace($Path) -or -not (Test-Path -LiteralPath $Path -PathType Leaf)) {
        throw "$Name was not found: $Path"
    }
    return [IO.Path]::GetFullPath($Path)
}

function Require-Directory([string]$Name, [string]$Path) {
    if ([string]::IsNullOrWhiteSpace($Path) -or -not (Test-Path -LiteralPath $Path -PathType Container)) {
        throw "$Name was not found: $Path"
    }
    return [IO.Path]::GetFullPath($Path)
}

New-Item -ItemType Directory -Path $BuildDirectory -Force | Out-Null
New-Item -ItemType Directory -Path $OutputDirectory -Force | Out-Null
if (-not $SkipApplicationBuild) {
    if (-not $WhisperRoot) {
        $candidate = [IO.Path]::GetFullPath((Join-Path $ProjectRoot "../../whisper.cpp"))
        if (Test-Path -LiteralPath (Join-Path $candidate "include/whisper.h") -PathType Leaf) { $WhisperRoot = $candidate }
    }
    $WhisperRoot = Require-Directory "WhisperRoot" $WhisperRoot
    $buildArguments = @(
        "-NoProfile", "-ExecutionPolicy", "Bypass", "-File", (Join-Path $ProjectRoot "scripts/dev/build-desktop.ps1"),
        "-WhisperRoot", $WhisperRoot, "-Output", $ApplicationExecutable, "-Version", $Version
    )
    if ($SkipFrontend) { $buildArguments += "-SkipFrontend" }
    & powershell.exe @buildArguments
    if ($LASTEXITCODE -ne 0) { throw "Application build failed with exit code $LASTEXITCODE" }
}
$ApplicationExecutable = Require-File "ApplicationExecutable" $ApplicationExecutable

$gcc = Get-Command gcc.exe -ErrorAction SilentlyContinue
if (-not $gcc) { throw "gcc.exe was not found; the native runtime DLL directory cannot be resolved." }
$runtimeDirectory = Split-Path $gcc.Source -Parent
$libGomp = Require-File "libgomp-1.dll" (Join-Path $runtimeDirectory "libgomp-1.dll")
$libWinPThread = Require-File "libwinpthread-1.dll" (Join-Path $runtimeDirectory "libwinpthread-1.dll")
$libStdCpp = Require-File "libstdc++-6.dll" (Join-Path $runtimeDirectory "libstdc++-6.dll")

if (-not $InnoCompiler) {
    $command = Get-Command ISCC.exe -ErrorAction SilentlyContinue
    $candidates = @(
        $(if ($command) { $command.Source }),
        (Join-Path ${env:ProgramFiles(x86)} "Inno Setup 6/ISCC.exe"),
        (Join-Path $env:LOCALAPPDATA "Programs/Inno Setup 6/ISCC.exe")
    ) | Where-Object { $_ -and (Test-Path -LiteralPath $_ -PathType Leaf) } | Select-Object -First 1
    if ($candidates) { $InnoCompiler = $candidates }
}
$InnoCompiler = Require-File "InnoCompiler" $InnoCompiler

$defines = @(
    "/DAppVersion=$Version",
    "/DNumericVersion=$NumericVersion",
    "/DOutputDir=$OutputDirectory",
    "/DAppExecutable=$ApplicationExecutable",
    "/DLibGomp=$libGomp",
    "/DLibWinPThread=$libWinPThread",
    "/DLibStdCpp=$libStdCpp"
)
if ($VCRedist) { $defines += "/DVCRedist=$(Require-File 'VCRedist' $VCRedist)" }
if ($WebView2Bootstrapper) { $defines += "/DWebView2Bootstrapper=$(Require-File 'WebView2Bootstrapper' $WebView2Bootstrapper)" }

& $InnoCompiler @defines $InstallerScript
if ($LASTEXITCODE -ne 0) { throw "Inno Setup failed with exit code $LASTEXITCODE" }

$installer = Get-Item (Join-Path $OutputDirectory "memplua-$Version-windows-x64-setup.exe")
Write-Output "Installer: $($installer.FullName)"
Write-Output "Size: $([math]::Round($installer.Length / 1MB, 1)) MB"
