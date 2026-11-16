# Installs this already-extracted autoOr-simg distribution for the current
# user. No admin rights required — copies one file into a per-user
# directory and creates Start Menu + Desktop shortcuts, same shape as
# packaging/linux/install.sh's launcher registration.
#
# Run this from inside the extracted zip's folder (PowerShell):
#   .\install.ps1
# If execution policy blocks it: powershell -ExecutionPolicy Bypass -File install.ps1

$ErrorActionPreference = "Stop"

$ScriptDir = Split-Path -Parent $MyInvocation.MyCommand.Path
$InstallDir = Join-Path $env:LOCALAPPDATA "Programs\autoOr-simg"

Write-Host "==> installing to $InstallDir"
New-Item -ItemType Directory -Force -Path $InstallDir | Out-Null
Copy-Item -Path (Join-Path $ScriptDir "autoOr-simg.exe") -Destination $InstallDir -Force
$ExePath = Join-Path $InstallDir "autoOr-simg.exe"

function New-AutoOrthoShortcut {
	param([string]$Path)
	$Shell = New-Object -ComObject WScript.Shell
	$Shortcut = $Shell.CreateShortcut($Path)
	$Shortcut.TargetPath = $ExePath
	$Shortcut.Arguments = "run"
	$Shortcut.WorkingDirectory = $InstallDir
	# The .exe has its own embedded icon (internal/tray/icon.ico, baked in
	# at build time via rsrc) — ",0" picks that first/only icon resource.
	$Shortcut.IconLocation = "$ExePath,0"
	$Shortcut.Description = "AutoOrtho /simg/ - unofficial Go rewrite of kubilus1's AutoOrtho"
	$Shortcut.Save()
}

$StartMenuDir = Join-Path $env:APPDATA "Microsoft\Windows\Start Menu\Programs"
New-AutoOrthoShortcut (Join-Path $StartMenuDir "autoOr-simg.lnk")
New-AutoOrthoShortcut (Join-Path ([Environment]::GetFolderPath("Desktop")) "autoOr-simg.lnk")

Write-Host "==> installed."
Write-Host "    binary:    $ExePath"
Write-Host "    shortcuts: Start Menu and Desktop (autoOr-simg)"
Write-Host ""
Write-Host "Needs WinFsp installed once (https://winfsp.dev/rel/) before scenery"
Write-Host "mounting will work — see README.txt."
Write-Host ""
Write-Host "Run via either new shortcut, or '$ExePath run' directly."
