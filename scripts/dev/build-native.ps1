param(
    [Parameter(Mandatory = $true)]
    [string]$WhisperRoot,
	[ValidateSet("Release", "Debug")]
	[string]$Configuration = "Release",

    [string]$Output = "knowledgecrawler.exe"
)

$ErrorActionPreference = "Stop"
$ProjectRoot = [IO.Path]::GetFullPath((Join-Path $PSScriptRoot "../.."))
. (Join-Path $PSScriptRoot "native-env.ps1") -WhisperRoot $WhisperRoot -Configuration $Configuration

Push-Location $ProjectRoot
try {
    go build -tags native -o $Output ./cmd/knowledgecrawler
    if ($LASTEXITCODE -ne 0) {
        throw "go build failed with exit code $LASTEXITCODE"
    }
} finally {
    Pop-Location
}
