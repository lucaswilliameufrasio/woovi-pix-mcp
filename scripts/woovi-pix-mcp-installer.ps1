param(
    [string]$Tag = 'latest',
    [string]$BinDir = (Join-Path $env:LOCALAPPDATA 'woovi-pix-mcp\bin'),
    [switch]$Force
)

$ErrorActionPreference = 'Stop'
$repo = 'lucaswilliameufrasio/woovi-pix-mcp'
$arch = switch ([System.Runtime.InteropServices.RuntimeInformation]::OSArchitecture) {
    'Arm64' { 'arm64' }
    'X64' { 'amd64' }
    default { throw "Unsupported architecture: $([System.Runtime.InteropServices.RuntimeInformation]::OSArchitecture)" }
}

if ($Tag -eq 'latest') {
    $release = Invoke-RestMethod -Uri "https://api.github.com/repos/$repo/releases/latest"
    $Tag = $release.tag_name
}
if ($Tag -notmatch '^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$') {
    throw "Invalid stable release tag: $Tag"
}

$version = $Tag.Substring(1)
$asset = "woovi-pix-mcp_${version}_windows_${arch}.zip"
$baseUrl = "https://github.com/$repo/releases/download/$Tag"
$tempDir = Join-Path ([System.IO.Path]::GetTempPath()) ([System.IO.Path]::GetRandomFileName())
$archive = Join-Path $tempDir $asset
$checksumFile = Join-Path $tempDir 'checksums.txt'

try {
    New-Item -ItemType Directory -Path $tempDir | Out-Null
    Write-Host "Downloading $baseUrl/$asset"
    Invoke-WebRequest -Uri "$baseUrl/$asset" -OutFile $archive
    Invoke-WebRequest -Uri "$baseUrl/checksums.txt" -OutFile $checksumFile

    $entry = Get-Content $checksumFile | Where-Object { $_ -match "\s\*?$([regex]::Escape($asset))$" }
    if (@($entry).Count -ne 1) { throw "Missing or duplicate checksum for $asset" }
    $expected = ($entry -split '\s+')[0]
    if ($expected -notmatch '^[a-fA-F0-9]{64}$') { throw "Invalid checksum for $asset" }
    $actual = (Get-FileHash -Algorithm SHA256 -Path $archive).Hash.ToLowerInvariant()
    if ($expected.ToLowerInvariant() -ne $actual) { throw "Checksum mismatch for $asset" }
    Write-Host 'Checksum verified'

    Expand-Archive -Path $archive -DestinationPath $tempDir -Force
    $binary = Join-Path $tempDir 'woovi-pix-mcp.exe'
    if (-not (Test-Path $binary)) { throw 'Archive does not contain woovi-pix-mcp.exe' }
    $destination = Join-Path $BinDir 'woovi-pix-mcp.exe'
    if ((Test-Path $destination) -and -not $Force) {
        $answer = Read-Host "Replace $destination? [y/N]"
        if ($answer -notmatch '^[Yy]$') { throw 'Installation cancelled' }
    }
    New-Item -ItemType Directory -Path $BinDir -Force | Out-Null
    Copy-Item $binary $destination -Force
    Write-Host "Installed woovi-pix-mcp $Tag to $destination"
    Write-Host "If needed, add this directory to PATH: $BinDir"
} finally {
    Remove-Item $tempDir -Recurse -Force -ErrorAction SilentlyContinue
}
