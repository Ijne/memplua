[CmdletBinding()]
param(
    [string]$AssetsDirectory = ""
)

$ErrorActionPreference = "Stop"
$ApplicationDirectory = [IO.Path]::GetFullPath($PSScriptRoot)
if (-not $AssetsDirectory) { $AssetsDirectory = $ApplicationDirectory }

& (Join-Path $PSScriptRoot "download-windows-models.ps1") `
    -ApplicationDirectory $ApplicationDirectory `
    -AssetsDirectory $AssetsDirectory `
    -ModelsOnly

exit 0
