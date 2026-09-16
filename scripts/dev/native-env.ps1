param(
    [Parameter(Mandatory = $true)]
    [string]$WhisperRoot,

    [ValidateSet("Release", "Debug")]
    [string]$Configuration = "Release"
)

$ErrorActionPreference = "Stop"
$KcWhisperRoot = [IO.Path]::GetFullPath($WhisperRoot)
$KcWhisperInclude = Join-Path $KcWhisperRoot "include"
$KcGgmlInclude = Join-Path $KcWhisperRoot "ggml/include"
$KcWhisperHeader = Join-Path $KcWhisperInclude "whisper.h"

if (-not (Test-Path -LiteralPath $KcWhisperHeader -PathType Leaf)) {
    throw "whisper.h was not found at $KcWhisperHeader"
}

$KcLibraryCandidates = @(
    (Join-Path $KcWhisperRoot "build/src/$Configuration"),
    (Join-Path $KcWhisperRoot "build/ggml/src/$Configuration"),
    (Join-Path $KcWhisperRoot "build/ggml/src/ggml-cpu/$Configuration"),
    (Join-Path $KcWhisperRoot "build/src"),
    (Join-Path $KcWhisperRoot "build/ggml/src"),
    (Join-Path $KcWhisperRoot "build/ggml/src/ggml-cpu")
)
$KcLibraryPaths = @($KcLibraryCandidates | Where-Object { Test-Path -LiteralPath $_ -PathType Container } | Select-Object -Unique)
if ($KcLibraryPaths.Count -eq 0) {
    throw "No built whisper.cpp libraries were found below $KcWhisperRoot"
}

$env:CGO_ENABLED = "1"
$env:CGO_CFLAGS = @("-O2", "-I$KcWhisperInclude", "-I$KcGgmlInclude") -join " "
$KcLinkerFlags = @("-O2")
foreach ($KcLibraryPath in $KcLibraryPaths) {
    $KcLinkerFlags += "-L$KcLibraryPath"
}
$KcLinkerFlags += @("-lwhisper", "-lggml", "-lggml-base", "-lggml-cpu", "-fopenmp", "-lm", "-lstdc++")
$env:CGO_LDFLAGS = $KcLinkerFlags -join " "

$KcRuntimePaths = @($KcLibraryPaths + (Join-Path $KcWhisperRoot "build/bin") | Where-Object { Test-Path -LiteralPath $_ -PathType Container } | Select-Object -Unique)
$env:PATH = (($KcRuntimePaths -join [IO.Path]::PathSeparator) + [IO.Path]::PathSeparator + $env:PATH)
