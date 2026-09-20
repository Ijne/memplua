param(
    [string]$Version = "0.2.0-alpha.2",
    [string]$VCRedist = "",
    [string]$WebView2Bootstrapper = "",
    [string]$InnoCompiler = "",
    [string]$OutputDirectory = "",
    [string]$PayloadDirectory = "",
    [string]$ApplicationExecutable = "",
    [string]$ModelsDirectory = "",
    [string]$ONNXRuntime = ""
)

$ErrorActionPreference = "Stop"
$ProjectRoot = [IO.Path]::GetFullPath((Join-Path $PSScriptRoot "../.."))
$InstallerScript = Join-Path $ProjectRoot "installer/windows/memplua.iss"
if (-not $OutputDirectory) { $OutputDirectory = Join-Path $ProjectRoot "dist/windows" }
$OutputDirectory = [IO.Path]::GetFullPath($OutputDirectory)
if (-not $PayloadDirectory) { $PayloadDirectory = Join-Path $ProjectRoot "dist/offline-payload" }
$PayloadDirectory = [IO.Path]::GetFullPath($PayloadDirectory)
if (-not $ApplicationExecutable) { $ApplicationExecutable = Join-Path $OutputDirectory "memplua.exe" }
if (-not $ModelsDirectory) { $ModelsDirectory = Join-Path $ProjectRoot "models_storage" }
if (-not $ONNXRuntime) { $ONNXRuntime = Join-Path $ProjectRoot "onnxruntime.dll" }
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

New-Item -ItemType Directory -Path $OutputDirectory -Force | Out-Null
$ApplicationExecutable = Require-File "ApplicationExecutable" $ApplicationExecutable
$ModelsDirectory = Require-Directory "ModelsDirectory" $ModelsDirectory
$ONNXRuntime = Require-File "ONNXRuntime" $ONNXRuntime
$llamaRuntime = Require-Directory "llama.cpp runtime" (Join-Path $ModelsDirectory "llama-b10549-bin-win-cuda-13.3-x64")
$llmModel = Require-File "Qwen model" (Join-Path $ModelsDirectory "Qwen3-4B-Q4_K_M.gguf")
$whisperModel = Require-File "Whisper model" (Join-Path $ModelsDirectory "ggml-small-q5_1.bin")
$sileroModel = Require-File "Silero model" (Join-Path $ModelsDirectory "silero_vad_v6.2.1.onnx")

if (Test-Path -LiteralPath $PayloadDirectory) {
    $expectedPayload = [IO.Path]::GetFullPath((Join-Path $ProjectRoot "dist/offline-payload"))
    if (-not [string]::Equals($PayloadDirectory, $expectedPayload, [StringComparison]::OrdinalIgnoreCase)) {
        throw "Refusing to replace a custom payload directory: $PayloadDirectory"
    }
    Remove-Item -LiteralPath $PayloadDirectory -Recurse -Force
}
New-Item -ItemType Directory -Path (Join-Path $PayloadDirectory "models") -Force | Out-Null
New-Item -ItemType Directory -Path (Join-Path $PayloadDirectory "runtime") -Force | Out-Null
Copy-Item -LiteralPath $ApplicationExecutable -Destination (Join-Path $PayloadDirectory "memplua.exe")
Copy-Item -LiteralPath $llamaRuntime -Destination (Join-Path $PayloadDirectory "runtime/llama") -Recurse
Copy-Item -LiteralPath $ONNXRuntime -Destination (Join-Path $PayloadDirectory "runtime/onnxruntime.dll")
Copy-Item -LiteralPath $llmModel -Destination (Join-Path $PayloadDirectory "models/llm.gguf")
Copy-Item -LiteralPath $whisperModel -Destination (Join-Path $PayloadDirectory "models/whisper.bin")
Copy-Item -LiteralPath $sileroModel -Destination (Join-Path $PayloadDirectory "models/silero.onnx")

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
    "/DLibGomp=$libGomp",
    "/DLibWinPThread=$libWinPThread",
    "/DLibStdCpp=$libStdCpp",
    "/DOfflinePayload=$PayloadDirectory"
)
if ($VCRedist) { $defines += "/DVCRedist=$(Require-File 'VCRedist' $VCRedist)" }
if ($WebView2Bootstrapper) { $defines += "/DWebView2Bootstrapper=$(Require-File 'WebView2Bootstrapper' $WebView2Bootstrapper)" }

& $InnoCompiler @defines $InstallerScript
if ($LASTEXITCODE -ne 0) { throw "Inno Setup failed with exit code $LASTEXITCODE" }

$installer = Get-Item (Join-Path $OutputDirectory "memplua-$Version-windows-x64-offline-setup.exe")
Remove-Item -LiteralPath $PayloadDirectory -Recurse -Force
Write-Output "Installer: $($installer.FullName)"
Write-Output "Size: $([math]::Round($installer.Length / 1MB, 1)) MB"
