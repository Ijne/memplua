[CmdletBinding()]
param(
    [Parameter(Mandatory)]
    [ValidatePattern('^v?\d+\.\d+\.\d+')]
    [string]$Tag,
    [string]$OutputDirectory = "",
    [string]$Title = "",
    [string]$Notes = ""
)

$ErrorActionPreference = "Stop"
$ProjectRoot = [IO.Path]::GetFullPath((Join-Path $PSScriptRoot "../.."))
if (-not $OutputDirectory) { $OutputDirectory = Join-Path $ProjectRoot "dist/windows" }
$OutputDirectory = [IO.Path]::GetFullPath($OutputDirectory)
$version = $Tag.TrimStart('v')
$installer = Join-Path $OutputDirectory "memplua-$version-windows-x64-offline-setup.exe"
$portable = Join-Path $OutputDirectory "memplua-$version-windows-x64-portable.zip"
$checksums = Join-Path $OutputDirectory "SHA256SUMS.txt"
foreach ($file in @($installer, $portable, $checksums)) {
    if (-not (Test-Path -LiteralPath $file -PathType Leaf)) { throw "Release artifact was not found: $file" }
}
$checksumLines = @(Get-Content -LiteralPath $checksums)
foreach ($artifact in @($installer, $portable)) {
    $expected = "{0} *{1}" -f (Get-FileHash -LiteralPath $artifact -Algorithm SHA256).Hash.ToLowerInvariant(), (Split-Path -Leaf $artifact)
    if ($expected -notin $checksumLines) { throw "SHA256SUMS.txt does not match $artifact" }
}

$gh = Get-Command "gh.exe" -ErrorAction SilentlyContinue
if (-not $gh) { $gh = Get-Command "gh" -ErrorAction SilentlyContinue }
if (-not $gh) { throw "GitHub CLI (gh) is required. Install it and authenticate with 'gh auth login'." }
if (-not $Title) { $Title = "memplua $Tag" }
if (-not $Notes) {
    $Notes = "Windows x64 release. Download the full offline installer using https://memplua.space/ (file hosted on Hugging Face). The portable ZIP is attached here; it includes runtime dependencies and a checksum-verified model download script. SHA256SUMS.txt also lists the installer's checksum."
}

& $gh.Source release view $Tag 2>$null
if ($LASTEXITCODE -eq 0) {
    & $gh.Source release upload $Tag $portable $checksums --clobber
} else {
    & $gh.Source release create $Tag $portable $checksums --title $Title --notes $Notes
}
if ($LASTEXITCODE -ne 0) { throw "GitHub Release publication failed with exit code $LASTEXITCODE." }
