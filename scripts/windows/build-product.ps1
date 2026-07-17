# Build Windows product artifacts (must run on Windows with CGO + MinGW/MSVC).
# Produces: spice-viewer.exe (GUI), zip, optional NSIS installer.
# Usage: .\scripts\windows\build-product.ps1 -Version v0.2.0
param(
    [string]$Version = "dev"
)

$ErrorActionPreference = "Stop"
$Root = Resolve-Path (Join-Path $PSScriptRoot "..\..")
Set-Location $Root

$VerClean = $Version.TrimStart("v")
$Dist = Join-Path $Root "dist\windows"
New-Item -ItemType Directory -Force -Path $Dist | Out-Null

$env:CGO_ENABLED = "1"
$ldflags = "-s -w -H windowsgui -X main.Version=$Version"

# Optional: embed icon/VERSIONINFO via go-winres when available.
$winresDir = Join-Path $Root "packaging\windows"
Push-Location $winresDir
try {
    if (Get-Command go -ErrorAction SilentlyContinue) {
        Write-Host "==> go-winres (optional)"
        go run github.com/tc-hib/go-winres@v0.3.3 make --in winres.json --out (Join-Path $Root "cmd\spice-viewer\rsrc") --arch amd64 2>$null
    }
} catch {
    Write-Host "go-winres skipped: $_"
} finally {
    Pop-Location
}

Write-Host "==> go build windows/amd64 (cgo + windowsgui)"
$exe = Join-Path $Dist "spice-viewer.exe"
go build -trimpath -ldflags $ldflags -o $exe ./cmd/spice-viewer

Copy-Item (Join-Path $Root "LICENSE") $Dist -ErrorAction SilentlyContinue
Copy-Item (Join-Path $Root "README.md") $Dist -ErrorAction SilentlyContinue
Copy-Item (Join-Path $Root "CHANGELOG.md") $Dist -ErrorAction SilentlyContinue
Copy-Item (Join-Path $Root "docs\proxmox.md") $Dist -ErrorAction SilentlyContinue
Copy-Item (Join-Path $Root "packaging\windows\associate-hkcu.ps1") $Dist -ErrorAction SilentlyContinue

$zip = Join-Path $Dist "spice-viewer_${VerClean}_windows_amd64.zip"
if (Test-Path $zip) { Remove-Item $zip }
Compress-Archive -Path (Join-Path $Dist "spice-viewer.exe"), (Join-Path $Dist "LICENSE"), (Join-Path $Dist "README.md"), (Join-Path $Dist "associate-hkcu.ps1") `
    -DestinationPath $zip -Force

# NSIS installer if makensis is on PATH
$nsis = Get-Command makensis -ErrorAction SilentlyContinue
if ($nsis) {
    Write-Host "==> NSIS installer"
    $nsi = Join-Path $Root "packaging\windows\installer.nsi"
    & makensis "/DVERSION=$VerClean" "/DSOURCE_DIR=$Dist" $nsi
    $setup = Join-Path $Root "spice-viewer-setup-$VerClean-amd64.exe"
    if (Test-Path $setup) {
        Move-Item -Force $setup (Join-Path $Dist "spice-viewer-setup-$VerClean-amd64.exe")
    }
} else {
    Write-Host "makensis not found; zip-only (install NSIS for setup.exe)"
}

Write-Host "Windows product artifacts in $Dist"
Get-ChildItem $Dist
