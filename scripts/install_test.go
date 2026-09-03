package scripts

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestInstallerStopsAfterNativeCommandFailure(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows installer")
	}
	shell, err := exec.LookPath("powershell.exe")
	if err != nil {
		t.Fatal(err)
	}
	installer, err := filepath.Abs("install.ps1")
	if err != nil {
		t.Fatal(err)
	}
	for _, stage := range []string{"test", "build"} {
		t.Run(stage, func(t *testing.T) {
			root := t.TempDir()
			binary := filepath.Join(root, "bin", "supercli.exe")
			if err := os.MkdirAll(filepath.Dir(binary), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(binary, []byte("existing installation"), 0o600); err != nil {
				t.Fatal(err)
			}
			// Guards prevent a broken installer from reaching persistent user
			// environment changes. Commands execute only against temporary paths.
			script := `
$ErrorActionPreference = 'Stop'
$global:buildFailed = $false
function go {
    if ($args[0] -eq 'test') {
        $global:LASTEXITCODE = $(if ($env:FAIL_STAGE -eq 'test') { 7 } else { 0 })
        return
    }
    if ($env:FAIL_STAGE -eq 'test') { throw 'BUILD_WAS_ATTEMPTED' }
    $global:buildFailed = $true
    $global:LASTEXITCODE = 9
}
function Test-Path {
    param([string]$LiteralPath)
    if ($global:buildFailed) { throw 'CONTINUED_AFTER_BUILD_FAILURE' }
    Microsoft.PowerShell.Management\Test-Path -LiteralPath $LiteralPath
}
& $env:TEST_INSTALLER -InstallDirectory "$env:TEST_ROOT\bin" -ConfigPath "$env:TEST_ROOT\config.yaml" -PluginDirectory "$env:TEST_ROOT\plugins"
`
			cmd := exec.Command(shell, "-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass", "-Command", script)
			cmd.Env = append(os.Environ(), "FAIL_STAGE="+stage, "TEST_ROOT="+root, "TEST_INSTALLER="+installer)
			output, err := cmd.CombinedOutput()
			want := "Tests failed"
			if stage == "build" {
				want = "Build failed"
			}
			if err == nil || !strings.Contains(string(output), want) {
				t.Fatalf("installer must stop with %q: %v\n%s", want, err, output)
			}
			got, err := os.ReadFile(binary)
			if err != nil || string(got) != "existing installation" {
				t.Fatal("failed install changed existing executable")
			}
			for _, name := range []string{"config.yaml", "plugins"} {
				if _, err := os.Stat(filepath.Join(root, name)); !os.IsNotExist(err) {
					t.Errorf("failed install created %s", name)
				}
			}
		})
	}
}
