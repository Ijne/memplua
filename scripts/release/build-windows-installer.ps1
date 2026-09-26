param(
    [string]$Version = "0.2.0-alpha.4",
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

function Assert-NoPrivateFiles([string]$Directory) {
    $privateFiles = Get-ChildItem -LiteralPath $Directory -Recurse -File | Where-Object {
        $_.Name -match '(?i)(\.db(?:-(?:wal|shm))?$|\.sqlite(?:3)?$|^api\.token$|^config\.toml$|^ui-state\.json$|\.log$)'
    }
    if ($privateFiles) {
        throw "Private runtime files found in release payload: $($privateFiles.FullName -join ', ')"
    }
}

function Assert-ExecutableRuntimeDependencies([string]$Executable, [string]$Objdump, [string[]]$BundledDLLs) {
    $imports = @(& $Objdump -p $Executable | Select-String 'DLL Name:' | ForEach-Object {
        ($_ -replace '^.*DLL Name:\s*', '').Trim().ToLowerInvariant()
    })
    $mingwImports = @($imports | Where-Object { $_ -match '^lib.+\.dll$' })
    $missing = @($mingwImports | Where-Object { $_ -notin $BundledDLLs })
    if ($missing) { throw "The application needs unbundled native DLLs: $($missing -join ', ')" }
}

function Assert-PortableArchive([string]$ArchivePath, [string]$PortableDirectoryName) {
    Add-Type -AssemblyName System.IO.Compression.FileSystem
    $archive = [IO.Compression.ZipFile]::OpenRead($ArchivePath)
    try {
        $entries = @($archive.Entries | ForEach-Object { $_.FullName.Replace('\', '/') })
        $required = @(
            "$PortableDirectoryName/memplua.exe",
            "$PortableDirectoryName/libgomp-1.dll",
            "$PortableDirectoryName/libstdc++-6.dll",
            "$PortableDirectoryName/libwinpthread-1.dll",
            "$PortableDirectoryName/runtime/onnxruntime.dll",
            "$PortableDirectoryName/runtime/llama/llama-server.exe",
            "$PortableDirectoryName/download-models.ps1"
        )
        $missing = @($required | Where-Object { $_ -notin $entries })
        if ($missing) { throw "Portable archive is missing: $($missing -join ', ')" }
        $models = @($entries | Where-Object { $_ -match '(?i)(^|/)(models/|.*\.(gguf|bin|onnx)$)' })
        if ($models) { throw "Portable archive must not include models: $($models -join ', ')" }
        $private = @($entries | Where-Object { $_ -match '(?i)(\.db(?:-(?:wal|shm))?$|\.sqlite(?:3)?$|(^|/)api\.token$|(^|/)config\.toml$|(^|/)ui-state\.json$|\.log$)' })
        if ($private) { throw "Portable archive contains private files: $($private -join ', ')" }
    } finally {
        $archive.Dispose()
    }
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

Assert-NoPrivateFiles $PayloadDirectory

$gcc = Get-Command gcc.exe -ErrorAction SilentlyContinue
if (-not $gcc) { throw "gcc.exe was not found; the native runtime DLL directory cannot be resolved." }
$runtimeDirectory = Split-Path $gcc.Source -Parent
$libGomp = Require-File "libgomp-1.dll" (Join-Path $runtimeDirectory "libgomp-1.dll")
$libWinPThread = Require-File "libwinpthread-1.dll" (Join-Path $runtimeDirectory "libwinpthread-1.dll")
$libStdCpp = Require-File "libstdc++-6.dll" (Join-Path $runtimeDirectory "libstdc++-6.dll")
$objdump = Get-Command "objdump.exe" -ErrorAction SilentlyContinue
if ($objdump) { $objdump = $objdump.Source } else { $objdump = Join-Path $runtimeDirectory "objdump.exe" }
$objdump = Require-File "objdump.exe" $objdump
Assert-ExecutableRuntimeDependencies $ApplicationExecutable $objdump @("libgomp-1.dll", "libwinpthread-1.dll", "libstdc++-6.dll")

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
$portableDirectory = Join-Path $OutputDirectory "memplua-$Version-windows-x64-portable"
$portableArchive = Join-Path $OutputDirectory "memplua-$Version-windows-x64-portable.zip"
if (Test-Path -LiteralPath $portableDirectory) { Remove-Item -LiteralPath $portableDirectory -Recurse -Force }
if (Test-Path -LiteralPath $portableArchive) { Remove-Item -LiteralPath $portableArchive -Force }
New-Item -ItemType Directory -Path (Join-Path $portableDirectory "runtime") -Force | Out-Null
Copy-Item -LiteralPath $ApplicationExecutable -Destination (Join-Path $portableDirectory "memplua.exe")
Copy-Item -LiteralPath $libGomp,$libWinPThread,$libStdCpp -Destination $portableDirectory
Copy-Item -LiteralPath $llamaRuntime -Destination (Join-Path $portableDirectory "runtime/llama") -Recurse
Copy-Item -LiteralPath $ONNXRuntime -Destination (Join-Path $portableDirectory "runtime/onnxruntime.dll")
Copy-Item -LiteralPath (Join-Path $ProjectRoot "scripts/release/download-windows-models.ps1") -Destination (Join-Path $portableDirectory "download-windows-models.ps1")
Copy-Item -LiteralPath (Join-Path $ProjectRoot "scripts/release/download-portable-models.ps1") -Destination (Join-Path $portableDirectory "download-models.ps1")
Copy-Item -LiteralPath (Join-Path $ProjectRoot "scripts/release/PORTABLE-README.txt") -Destination (Join-Path $portableDirectory "README.txt")
Assert-NoPrivateFiles $portableDirectory
Compress-Archive -LiteralPath $portableDirectory -DestinationPath $portableArchive -CompressionLevel Optimal
$portableArchive = Get-Item -LiteralPath $portableArchive
Assert-PortableArchive $portableArchive.FullName $portableDirectory.Name
$checksums = Join-Path $OutputDirectory "SHA256SUMS.txt"
@($installer, $portableArchive) | ForEach-Object {
    "{0} *{1}" -f (Get-FileHash -LiteralPath $_.FullName -Algorithm SHA256).Hash.ToLowerInvariant(), $_.Name
} | Set-Content -LiteralPath $checksums -Encoding ASCII
Remove-Item -LiteralPath $PayloadDirectory -Recurse -Force
Write-Output "Installer: $($installer.FullName)"
Write-Output "Size: $([math]::Round($installer.Length / 1MB, 1)) MB"
Write-Output "Portable archive: $($portableArchive.FullName)"
Write-Output "Checksums: $checksums"
