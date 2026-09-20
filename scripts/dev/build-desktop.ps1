param(
    [switch]$Native,
    [switch]$CoreOnly,
    [string]$WhisperRoot = "",
    [ValidateSet("Release", "Debug")]
    [string]$Configuration = "Release",
    [string]$Output = "memplua.exe",
    [string]$Version = "",
    [switch]$SkipFrontend
)

$ErrorActionPreference = "Stop"
$ProjectRoot = [IO.Path]::GetFullPath((Join-Path $PSScriptRoot "../.."))
$FrontendRoot = Join-Path $ProjectRoot "frontend/desktop"

if ($Native -and $CoreOnly) {
    throw "-Native and -CoreOnly cannot be used together"
}
if (-not $WhisperRoot -and $env:MEMPLUA_WHISPER_ROOT) {
    $WhisperRoot = $env:MEMPLUA_WHISPER_ROOT
}
if (-not $WhisperRoot -and $env:KNOWLEDGECRAWLER_WHISPER_ROOT) {
    $WhisperRoot = $env:KNOWLEDGECRAWLER_WHISPER_ROOT
}
if (-not $WhisperRoot) {
    $WhisperCandidate = [IO.Path]::GetFullPath((Join-Path $ProjectRoot "../../whisper.cpp"))
    if (Test-Path -LiteralPath (Join-Path $WhisperCandidate "include/whisper.h") -PathType Leaf) {
        $WhisperRoot = $WhisperCandidate
    }
}
if (-not $CoreOnly -and $WhisperRoot) {
    $Native = $true
}

if ($Native) {
    if ([string]::IsNullOrWhiteSpace($WhisperRoot)) {
        throw "-Native requires -WhisperRoot or MEMPLUA_WHISPER_ROOT pointing to the external Whisper build."
    }
    . (Join-Path $PSScriptRoot "native-env.ps1") -WhisperRoot $WhisperRoot -Configuration $Configuration
    Write-Output "Native audio: enabled ($WhisperRoot)"
} else {
    Write-Warning "Native audio is disabled. microphone and loopback will be unavailable; provide -WhisperRoot or MEMPLUA_WHISPER_ROOT, or omit -CoreOnly."
}

if (-not $SkipFrontend) {
    Push-Location $FrontendRoot
    try {
        npm.cmd ci
        if ($LASTEXITCODE -ne 0) { throw "npm ci failed with exit code $LASTEXITCODE" }
        npm.cmd run build
        if ($LASTEXITCODE -ne 0) { throw "frontend build failed with exit code $LASTEXITCODE" }
    } finally { Pop-Location }
}

$DesktopIndex = Join-Path $ProjectRoot "internal/desktop/assets/index.html"
if (-not (Test-Path -LiteralPath $DesktopIndex)) { throw "Build the desktop frontend before compiling Go." }
if ((Get-Content -LiteralPath $DesktopIndex -Raw) -notmatch '<script[^>]+src=') {
    throw "Desktop assets are a placeholder. Run this script without -SkipFrontend."
}

$BuildTags = "desktop,production"
if ($Native) { $BuildTags += ",native" }
$ResourceScript = Join-Path $ProjectRoot "installer/windows/memplua.rc"
$ResourceObject = Join-Path $ProjectRoot "cmd/memplua/memplua_windows_amd64.syso"
$WindRes = Get-Command windres.exe -ErrorAction SilentlyContinue
if (-not $WindRes) {
    throw "windres.exe was not found; it is required to embed the branded Windows icon."
}
& $WindRes.Source "--target=pe-x86-64" "--input=$ResourceScript" "--output=$ResourceObject" "--output-format=coff" "--include-dir=$(Split-Path $ResourceScript -Parent)"
if ($LASTEXITCODE -ne 0) { throw "windres failed with exit code $LASTEXITCODE" }

Push-Location $ProjectRoot
try {
    # Preserve the existing native-env.ps1 Whisper linking configuration.
    # The release subsystem avoids a console window for ordinary desktop launch.
    $BuildArguments = @("build", "-tags", $BuildTags, "-o", $Output)
    $LinkerFlags = @()
    if ($Configuration -eq "Release") { $LinkerFlags += "-H windowsgui" }
    if (-not [string]::IsNullOrWhiteSpace($Version)) { $LinkerFlags += "-X main.version=$Version" }
    if ($Configuration -eq "Release") { $BuildArguments += "-trimpath" }
    if ($LinkerFlags.Count -gt 0) { $BuildArguments += @("-ldflags", ($LinkerFlags -join " ")) }
    $BuildArguments += "./cmd/memplua"
    try {
        & go @BuildArguments
        if ($LASTEXITCODE -ne 0) { throw "desktop build failed with exit code $LASTEXITCODE" }
    } finally {
        if (Test-Path -LiteralPath $ResourceObject) { Remove-Item -LiteralPath $ResourceObject -Force }
    }
} finally { Pop-Location }
