[CmdletBinding()]
param(
    [string]$InstallDirectory = "$env:LOCALAPPDATA\SuperCLI\bin",
    [string]$ConfigPath = "$env:LOCALAPPDATA\SuperCLI\config.yaml",
    [string]$PluginDirectory = "$env:LOCALAPPDATA\SuperCLI\plugins"
)

$ErrorActionPreference = 'Stop'
$projectRoot = Split-Path -Parent $PSScriptRoot
$binaryPath = Join-Path $InstallDirectory 'supercli.exe'

New-Item -ItemType Directory -Force -Path $InstallDirectory | Out-Null
New-Item -ItemType Directory -Force -Path $PluginDirectory | Out-Null
$bundledPlugin = Join-Path $projectRoot 'plugins\git-insights'
$installedPlugin = Join-Path $PluginDirectory 'git-insights'
if ((Test-Path -LiteralPath $bundledPlugin) -and -not (Test-Path -LiteralPath $installedPlugin)) {
    Copy-Item -Recurse -LiteralPath $bundledPlugin -Destination $installedPlugin
}
Push-Location $projectRoot
try {
    go test ./...
    go build -trimpath -ldflags '-s -w -X main.version=0.3.1-alpha' -o $binaryPath ./cmd/supercli
} finally {
    Pop-Location
}

if (-not (Test-Path -LiteralPath $ConfigPath)) {
    New-Item -ItemType Directory -Force -Path (Split-Path -Parent $ConfigPath) | Out-Null
    Copy-Item -LiteralPath (Join-Path $projectRoot 'internal\bootstrap\example.yaml') -Destination $ConfigPath
}

$userPath = [Environment]::GetEnvironmentVariable('Path', 'User')
$pathEntries = @($userPath -split ';' | Where-Object { $_ })
if ($InstallDirectory -notin $pathEntries) {
    $newPath = (@($pathEntries) + $InstallDirectory) -join ';'
    [Environment]::SetEnvironmentVariable('Path', $newPath, 'User')
}
[Environment]::SetEnvironmentVariable('SUPERCLI_CONFIG', $ConfigPath, 'User')
[Environment]::SetEnvironmentVariable('SUPERCLI_PLUGIN_DIR', $PluginDirectory, 'User')

Write-Host "SuperCLI installed at $binaryPath" -ForegroundColor Green
Write-Host "Configuration: $ConfigPath"
Write-Host "Community plugins: $PluginDirectory"
Write-Host 'Open a new terminal and run: supercli'
