[CmdletBinding()]
param(
    [Parameter(Mandatory)]
    [ValidateNotNullOrEmpty()]
    [string]$ApplicationDirectory,
    [string]$LogPath = ""
)

# Run by the installer, not by the application. Artifact choices stay here so
# production Go code carries neither machine-specific model paths nor URLs.
$ErrorActionPreference = "Stop"
$ProgressPreference = "SilentlyContinue"
$ApplicationDirectory = [IO.Path]::GetFullPath($ApplicationDirectory)
if (-not $LogPath) { $LogPath = Join-Path $ApplicationDirectory "model-install.log" }
$LogPath = [IO.Path]::GetFullPath($LogPath)

function Write-InstallLog([string]$Message) {
    Add-Content -LiteralPath $LogPath -Value ("{0} {1}" -f (Get-Date).ToUniversalTime().ToString("o"), $Message) -Encoding UTF8
}

function Invoke-SourceRequest([string]$Uri) {
    Invoke-RestMethod -Uri $Uri -Headers @{ "User-Agent" = "memplua-installer"; "Accept" = "application/json" } -UseBasicParsing
}

function Get-GitHubReleaseAsset([string]$Repository, [string]$NamePattern) {
    # Stable llama.cpp releases do not always include Windows builds. The
    # official feed is ordered newest-first, so choose its first attested CPU asset.
    foreach ($release in @(Invoke-SourceRequest "https://api.github.com/repos/$Repository/releases?per_page=100")) {
        $asset = @($release.assets | Where-Object { $_.name -match $NamePattern } | Select-Object -First 1)
        if ($asset.Count -eq 1 -and $asset[0].digest -match "^sha256:([0-9a-fA-F]{64})$") {
            return [PSCustomObject]@{ Name = $asset[0].name; URL = $asset[0].browser_download_url; SHA256 = $Matches[1].ToLowerInvariant() }
        }
    }
    throw "No SHA-256 verified release asset matching $NamePattern was found in $Repository."
}

function Get-GitHubLatestReleaseAsset([string]$Repository, [string]$Name) {
    $release = Invoke-SourceRequest "https://api.github.com/repos/$Repository/releases/latest"
    $asset = @($release.assets | Where-Object { $_.name -eq $Name } | Select-Object -First 1)
    if ($asset.Count -ne 1) {
        throw "The latest release of $Repository does not contain $Name."
    }
    if ($asset[0].digest -notmatch "^sha256:([0-9a-fA-F]{64})$") {
        throw "The latest $Name release asset does not expose a SHA-256 digest."
    }
    [PSCustomObject]@{ Name = $asset[0].name; URL = $asset[0].browser_download_url; SHA256 = $Matches[1].ToLowerInvariant() }
}

function Get-HuggingFaceFile([string]$Repository, [string]$Path) {
    $file = @(@(Invoke-SourceRequest "https://huggingface.co/api/models/$Repository/tree/main?recursive=true&expand=true") | Where-Object { $_.path -eq $Path } | Select-Object -First 1)
    if ($file.Count -ne 1 -or $file[0].lfs.oid -notmatch "^[0-9a-fA-F]{64}$") {
        throw "No SHA-256 model metadata was returned for $Repository/$Path."
    }
    [PSCustomObject]@{ Name = $Path; URL = "https://huggingface.co/$Repository/resolve/main/$Path?download=true"; SHA256 = $file[0].lfs.oid.ToLowerInvariant() }
}

function Download-VerifiedFile([PSCustomObject]$Artifact, [string]$Destination) {
    if (-not ([Uri]$Artifact.URL).Scheme.Equals("https", [StringComparison]::OrdinalIgnoreCase)) { throw "Refusing a non-HTTPS download for $($Artifact.Name)." }
    New-Item -ItemType Directory -Path (Split-Path -Parent $Destination) -Force | Out-Null
    $temporary = "$Destination.part"
    $lastError = $null
    for ($attempt = 1; $attempt -le 3; $attempt++) {
        try {
            Remove-Item -LiteralPath $temporary -Force -ErrorAction SilentlyContinue
            Write-InstallLog "Downloading $($Artifact.Name), attempt $attempt of 3."
            Invoke-WebRequest -Uri $Artifact.URL -OutFile $temporary -MaximumRedirection 10 -UseBasicParsing -Headers @{ "User-Agent" = "memplua-installer" }
            $actual = (Get-FileHash -LiteralPath $temporary -Algorithm SHA256).Hash.ToLowerInvariant()
            if ($actual -ne $Artifact.SHA256) { throw "SHA-256 mismatch for $($Artifact.Name)." }
            Move-Item -LiteralPath $temporary -Destination $Destination -Force
            return
        } catch {
            $lastError = $_
            Remove-Item -LiteralPath $temporary -Force -ErrorAction SilentlyContinue
            Write-InstallLog "Download attempt $attempt failed for $($Artifact.Name): $($_.Exception.Message)"
        }
    }
    throw "Unable to download $($Artifact.Name): $($lastError.Exception.Message)"
}

function Get-GitHubBlob([string]$Repository, [string]$Path) {
    $encodedPath = ($Path -split "/" | ForEach-Object { [Uri]::EscapeDataString($_) }) -join "/"
    $file = Invoke-SourceRequest "https://api.github.com/repos/$Repository/contents/$encodedPath"
    if ($file.sha -notmatch "^[0-9a-fA-F]{40}$" -or -not $file.download_url) { throw "No Git blob checksum was returned for $Repository/$Path." }
    [PSCustomObject]@{ Name = $Path; URL = $file.download_url; GitSHA1 = $file.sha.ToLowerInvariant() }
}

function Get-GitBlobSHA1([string]$Path) {
    [byte[]]$content = [IO.File]::ReadAllBytes($Path)
    [byte[]]$header = [Text.Encoding]::UTF8.GetBytes("blob $($content.Length)$([char]0)")
    [byte[]]$payload = New-Object byte[] ($header.Length + $content.Length)
    [Buffer]::BlockCopy($header, 0, $payload, 0, $header.Length)
    [Buffer]::BlockCopy($content, 0, $payload, $header.Length, $content.Length)
    $sha1 = [Security.Cryptography.SHA1]::Create()
    try { -join ($sha1.ComputeHash($payload) | ForEach-Object { $_.ToString("x2") }) } finally { $sha1.Dispose() }
}

function Download-VerifiedGitBlob([PSCustomObject]$Artifact, [string]$Destination) {
    if (-not ([Uri]$Artifact.URL).Scheme.Equals("https", [StringComparison]::OrdinalIgnoreCase)) { throw "Refusing a non-HTTPS download for $($Artifact.Name)." }
    New-Item -ItemType Directory -Path (Split-Path -Parent $Destination) -Force | Out-Null
    $temporary = "$Destination.part"
    $lastError = $null
    for ($attempt = 1; $attempt -le 3; $attempt++) {
        try {
            Remove-Item -LiteralPath $temporary -Force -ErrorAction SilentlyContinue
            Write-InstallLog "Downloading $($Artifact.Name), attempt $attempt of 3."
            Invoke-WebRequest -Uri $Artifact.URL -OutFile $temporary -MaximumRedirection 10 -UseBasicParsing -Headers @{ "User-Agent" = "memplua-installer" }
            if ((Get-GitBlobSHA1 $temporary) -ne $Artifact.GitSHA1) { throw "Git blob checksum mismatch for $($Artifact.Name)." }
            Move-Item -LiteralPath $temporary -Destination $Destination -Force
            return
        } catch {
            $lastError = $_
            Remove-Item -LiteralPath $temporary -Force -ErrorAction SilentlyContinue
            Write-InstallLog "Download attempt $attempt failed for $($Artifact.Name): $($_.Exception.Message)"
        }
    }
    throw "Unable to download $($Artifact.Name): $($lastError.Exception.Message)"
}

function Install-StagedItems([object[]]$Items) {
    $records = @()
    try {
        foreach ($item in $Items) {
            New-Item -ItemType Directory -Path (Split-Path -Parent $item.Destination) -Force | Out-Null
            $record = [PSCustomObject]@{
                Source = $item.Source
                Destination = $item.Destination
                Backup = "$($item.Destination).previous-$([Guid]::NewGuid().ToString('N'))"
                HadExisting = Test-Path -LiteralPath $item.Destination
                BackupMade = $false
                Installed = $false
            }
            $records += $record
            if ($record.HadExisting) {
                Move-Item -LiteralPath $record.Destination -Destination $record.Backup -Force
                $record.BackupMade = $true
            }
        }
        foreach ($record in $records) {
            Move-Item -LiteralPath $record.Source -Destination $record.Destination -Force
            $record.Installed = $true
        }
    } catch {
        foreach ($record in @($records | Sort-Object -Descending Destination)) {
            if ($record.Installed) { Remove-Item -LiteralPath $record.Destination -Recurse -Force -ErrorAction SilentlyContinue }
            if ($record.BackupMade -and (Test-Path -LiteralPath $record.Backup)) { Move-Item -LiteralPath $record.Backup -Destination $record.Destination -Force }
        }
        throw
    }
    foreach ($record in $records) {
        if ($record.BackupMade) { Remove-Item -LiteralPath $record.Backup -Recurse -Force }
    }
}

New-Item -ItemType Directory -Path $ApplicationDirectory -Force | Out-Null
Set-Content -LiteralPath $LogPath -Value "" -Encoding UTF8
$staging = Join-Path $ApplicationDirectory ".model-download-$([Guid]::NewGuid().ToString('N'))"
try {
    New-Item -ItemType Directory -Path $staging -Force | Out-Null
    Write-InstallLog "Resolving the latest memplua release and model artifacts from their official publishers."
    $application = Get-GitHubLatestReleaseAsset "Ijne/Crawler" "memplua.exe"
    $llama = Get-GitHubReleaseAsset "ggml-org/llama.cpp" "^llama-.*-bin-win-cpu-x64\\.zip$"
    $onnx = Get-GitHubReleaseAsset "microsoft/onnxruntime" "^onnxruntime-win-x64-[0-9.]+\\.zip$"
    $llm = Get-HuggingFaceFile "Qwen/Qwen3-4B-GGUF" "Qwen3-4B-Q4_K_M.gguf"
    $whisper = Get-HuggingFaceFile "ggerganov/whisper.cpp" "ggml-small-q5_1.bin"
    $silero = Get-GitHubBlob "snakers4/silero-vad" "src/silero_vad/data/silero_vad.onnx"
    $llamaArchive = Join-Path $staging "llama.zip"
    $onnxArchive = Join-Path $staging "onnxruntime.zip"
    Download-VerifiedFile $application (Join-Path $staging "application/memplua.exe")
    Download-VerifiedFile $llama $llamaArchive
    Download-VerifiedFile $onnx $onnxArchive
    Download-VerifiedFile $llm (Join-Path $staging "models/llm.gguf")
    Download-VerifiedFile $whisper (Join-Path $staging "models/whisper.bin")
    Download-VerifiedGitBlob $silero (Join-Path $staging "models/silero.onnx")
    $llamaExtract = Join-Path $staging "llama-extract"
    $onnxExtract = Join-Path $staging "onnx-extract"
    Expand-Archive -LiteralPath $llamaArchive -DestinationPath $llamaExtract -Force
    Expand-Archive -LiteralPath $onnxArchive -DestinationPath $onnxExtract -Force
    $llamaServer = @(Get-ChildItem -LiteralPath $llamaExtract -Filter "llama-server.exe" -File -Recurse)
    if ($llamaServer.Count -ne 1) { throw "The llama.cpp archive did not contain exactly one llama-server.exe." }
    $onnxRuntime = @(Get-ChildItem -LiteralPath $onnxExtract -Filter "onnxruntime.dll" -File -Recurse)
    if ($onnxRuntime.Count -ne 1) { throw "The ONNX Runtime archive did not contain exactly one onnxruntime.dll." }
    Install-StagedItems @(
        [PSCustomObject]@{ Source = Join-Path $staging "application/memplua.exe"; Destination = Join-Path $ApplicationDirectory "memplua.exe" }
        [PSCustomObject]@{ Source = Split-Path -Parent $llamaServer[0].FullName; Destination = Join-Path $ApplicationDirectory "runtime/llama" }
        [PSCustomObject]@{ Source = $onnxRuntime[0].FullName; Destination = Join-Path $ApplicationDirectory "runtime/onnxruntime.dll" }
        [PSCustomObject]@{ Source = Join-Path $staging "models/llm.gguf"; Destination = Join-Path $ApplicationDirectory "models/llm.gguf" }
        [PSCustomObject]@{ Source = Join-Path $staging "models/whisper.bin"; Destination = Join-Path $ApplicationDirectory "models/whisper.bin" }
        [PSCustomObject]@{ Source = Join-Path $staging "models/silero.onnx"; Destination = Join-Path $ApplicationDirectory "models/silero.onnx" }
    )
    Write-InstallLog "All required models were installed successfully."
    Remove-Item -LiteralPath $LogPath -Force
} catch {
    Write-InstallLog "Installation failed: $($_.Exception.Message)"
    throw
} finally {
    Remove-Item -LiteralPath $staging -Recurse -Force -ErrorAction SilentlyContinue
}
