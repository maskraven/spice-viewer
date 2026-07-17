# Associate *.vv with a portable remote-viewer.exe for the current user (no admin).
# Usage: powershell -ExecutionPolicy Bypass -File associate-hkcu.ps1 -ExePath C:\path\remote-viewer.exe
param(
    [Parameter(Mandatory = $true)]
    [string]$ExePath
)

$ErrorActionPreference = "Stop"
$exe = (Resolve-Path $ExePath).Path
$progId = "remote-viewer.vv"
$mime = "application/x-virt-viewer"

New-Item -Path "HKCU:\Software\Classes\.vv" -Force | Out-Null
Set-ItemProperty -Path "HKCU:\Software\Classes\.vv" -Name "(default)" -Value $progId
Set-ItemProperty -Path "HKCU:\Software\Classes\.vv" -Name "Content Type" -Value $mime

New-Item -Path "HKCU:\Software\Classes\$progId" -Force | Out-Null
Set-ItemProperty -Path "HKCU:\Software\Classes\$progId" -Name "(default)" -Value "virt-viewer connection file"
New-Item -Path "HKCU:\Software\Classes\$progId\DefaultIcon" -Force | Out-Null
Set-ItemProperty -Path "HKCU:\Software\Classes\$progId\DefaultIcon" -Name "(default)" -Value "$exe,0"
New-Item -Path "HKCU:\Software\Classes\$progId\shell\open\command" -Force | Out-Null
Set-ItemProperty -Path "HKCU:\Software\Classes\$progId\shell\open\command" -Name "(default)" -Value "`"$exe`" `"%1`""

# Refresh shell associations
$signature = @'
[DllImport("shell32.dll")] public static extern void SHChangeNotify(int wEventId, int uFlags, IntPtr dwItem1, IntPtr dwItem2);
'@
$type = Add-Type -MemberDefinition $signature -Name ShellNotify -Namespace Native -PassThru
$type::SHChangeNotify(0x08000000, 0, [IntPtr]::Zero, [IntPtr]::Zero)

Write-Host "Associated .vv with $exe (HKCU)."
