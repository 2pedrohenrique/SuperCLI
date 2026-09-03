[CmdletBinding()]
param(
    [string]$InstallDirectory = "$env:LOCALAPPDATA\SuperCLI\bin",
    [string]$ConfigPath = "$env:LOCALAPPDATA\SuperCLI\config.yaml",
    [string]$PluginDirectory = "$env:LOCALAPPDATA\SuperCLI\plugins"
)

$ErrorActionPreference = 'Stop'
$projectRoot = Split-Path -Parent $PSScriptRoot
$binaryPath = Join-Path $InstallDirectory 'supercli.exe'

$stagedBinary = $null
Push-Location $projectRoot
try {
    go test ./...
    if ($LASTEXITCODE -ne 0) {
        throw "Tests failed (exit code $LASTEXITCODE). Installation was not changed."
    }

    # Build outside the installation so a compiler failure cannot replace it.
    $stagedBinary = [System.IO.Path]::GetTempFileName()
    go build -trimpath -ldflags '-s -w -X main.version=0.3.1-alpha' -o $stagedBinary ./cmd/supercli
    if ($LASTEXITCODE -ne 0) {
        throw "Build failed (exit code $LASTEXITCODE). Installation was not changed."
    }
    New-Item -ItemType Directory -Force -Path $InstallDirectory | Out-Null
    Copy-Item -LiteralPath $stagedBinary -Destination $binaryPath -Force
} finally {
    Pop-Location
    if ($null -ne $stagedBinary) {
        Remove-Item -LiteralPath $stagedBinary -Force
    }
}

New-Item -ItemType Directory -Force -Path $PluginDirectory | Out-Null
$bundledPlugin = Join-Path $projectRoot 'plugins\git-insights'
$installedPlugin = Join-Path $PluginDirectory 'git-insights'
if ((Test-Path -LiteralPath $bundledPlugin) -and -not (Test-Path -LiteralPath $installedPlugin)) {
    Copy-Item -Recurse -LiteralPath $bundledPlugin -Destination $installedPlugin
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
