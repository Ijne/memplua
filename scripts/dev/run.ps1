param(
    [string]$Config = "",
    [switch]$Native,
    [switch]$CoreOnly,
    [string]$WhisperRoot = "",
    [ValidateSet("Release", "Debug")]
    [string]$Configuration = "Release"
)

$ErrorActionPreference = "Stop"
$ProjectRoot = [IO.Path]::GetFullPath((Join-Path $PSScriptRoot "../.."))
$LocalConfig = Join-Path $ProjectRoot "config.toml"
if (-not $Config -and (Test-Path -LiteralPath $LocalConfig -PathType Leaf)) {
    $Config = $LocalConfig
}
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
$Arguments = @("run")
if ($Native) {
    if (-not $WhisperRoot) {
        throw "-WhisperRoot is required with -Native so CGO can link Whisper libraries"
    }
    . (Join-Path $PSScriptRoot "native-env.ps1") -WhisperRoot $WhisperRoot -Configuration $Configuration
    $Arguments += @("-tags", "native")
    Write-Output "Native audio: enabled ($WhisperRoot)"
} else {
    Write-Warning "Native audio is disabled. microphone and loopback will be unavailable; provide -WhisperRoot or MEMPLUA_WHISPER_ROOT."
}
$Arguments += @("./cmd/memplua", "serve")
if ($Config) {
    $Arguments += @("--config", [IO.Path]::GetFullPath($Config))
}

Push-Location $ProjectRoot
try {
    & go @Arguments
    exit $LASTEXITCODE
} finally {
    Pop-Location
}
