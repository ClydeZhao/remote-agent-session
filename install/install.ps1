[CmdletBinding()]
param()
$ErrorActionPreference = 'Stop'
Set-StrictMode -Version Latest

function Invoke-Checked {
    param([string]$Program, [string[]]$Arguments)
    & $Program @Arguments
    if ($LASTEXITCODE -ne 0) { throw "$Program failed with exit code $LASTEXITCODE" }
}

$Identity = [Security.Principal.WindowsIdentity]::GetCurrent()
$Principal = [Security.Principal.WindowsPrincipal]::new($Identity)
if ($Principal.IsInRole([Security.Principal.WindowsBuiltInRole]::Administrator)) {
    throw 'Use an ordinary, non-elevated PowerShell terminal.'
}
$Root = Split-Path -Parent $PSScriptRoot
$Destination = $env:REMOTE_AGENT_SESSION_INSTALL_DIR
if (-not $Destination) { $Destination = Join-Path $env:LOCALAPPDATA 'remote-agent-session' }
$Destination = [IO.Path]::GetFullPath($Destination)
if (Test-Path -LiteralPath $Destination) { throw 'Installation path already exists. Choose a fresh REMOTE_AGENT_SESSION_INSTALL_DIR.' }
if ($Destination.StartsWith($Root + [IO.Path]::DirectorySeparatorChar, [StringComparison]::OrdinalIgnoreCase)) {
    throw 'Choose an installation path outside the source or extracted release tree.'
}
$Python = $env:REMOTE_AGENT_SESSION_PYTHON
if (-not $Python) { $Python = 'python' }
Invoke-Checked -Program $Python -Arguments @('-c', 'import os,sys; assert os.name == "nt", "Native Windows Python required"; assert sys.version_info >= (3,11), "Python 3.11+ required"')
$Created = $false
try {
    $Directory = New-Item -ItemType Directory -Path $Destination
    $Created = $true
    $Acl = [Security.AccessControl.DirectorySecurity]::new()
    $Acl.SetOwner($Identity.User)
    $Acl.SetAccessRuleProtection($true, $false)
    $Rule = [Security.AccessControl.FileSystemAccessRule]::new($Identity.User, 'FullControl', 'ContainerInherit,ObjectInherit', 'None', 'Allow')
    $Acl.AddAccessRule($Rule)
    Set-Acl -LiteralPath $Directory.FullName -AclObject $Acl
    $CopyProgram = @'
import pathlib,shutil,sys
source,dest=map(pathlib.Path,sys.argv[1:])
for part in ('server','tools','integrations','skills','docs','config','install','third_party','share'):
    if (source/part).is_dir():
        shutil.copytree(source/part,dest/part,ignore=shutil.ignore_patterns('.venv','__pycache__','.pytest_cache','node_modules','*.pyc'))
for name in ('README.md','THIRD_PARTY_NOTICES.md','LICENSE'):
    shutil.copy2(source/name,dest/name)
(dest/'bin').mkdir()
'@
    Invoke-Checked -Program $Python -Arguments @('-c', $CopyProgram, $Root, $Destination)
    $Binary = Join-Path $Destination 'bin\remote-agent-session.exe'
    $Prebuilt = Join-Path $Root 'bin\remote-agent-session.exe'
    if (Test-Path -LiteralPath $Prebuilt) {
        Copy-Item -LiteralPath $Prebuilt -Destination $Binary
    } else {
        $GoOS = & go env GOOS
        if ($LASTEXITCODE -ne 0 -or $GoOS.Trim() -ne 'windows') { throw 'Source installation requires a native Windows Go target; remove cross-build GOOS settings.' }
        $Digest = & $Python (Join-Path $Root 'tools\package_plugin.py') (Join-Path $Root 'integrations\deepseek-harness') (Join-Path $Destination 'share\remote-agent-session\deepseek-harness.tgz')
        if ($LASTEXITCODE -ne 0) { throw 'Plugin packaging failed' }
        Push-Location (Join-Path $Root 'cli')
        try {
            Invoke-Checked -Program 'go' -Arguments @('build', '-trimpath', '-ldflags', "-X github.com/ClydeZhao/remote-agent-session/cli/internal/buildinfo.Version=0.1.0 -X github.com/ClydeZhao/remote-agent-session/cli/internal/buildinfo.DSHPluginSHA256=$Digest", '-o', $Binary, './cmd/remote-agent-session')
        } finally { Pop-Location }
    }
    Invoke-Checked -Program $Python -Arguments @('-m', 'venv', (Join-Path $Destination '.venv'))
    $InstalledPython = Join-Path $Destination '.venv\Scripts\python.exe'
    Invoke-Checked -Program $InstalledPython -Arguments @('-m', 'pip', 'install', '--disable-pip-version-check', '--require-hashes', '-r', (Join-Path $Destination 'install\requirements.lock'))
    Invoke-Checked -Program $InstalledPython -Arguments @('-m', 'pip', 'install', '--disable-pip-version-check', '--no-deps', (Join-Path $Destination 'server'))
    Invoke-Checked -Program $InstalledPython -Arguments @((Join-Path $Destination 'tools\lifecycle.py'), 'doctor', '--mode', 'offline')
    [IO.File]::WriteAllText((Join-Path $Destination '.installation-complete'), "remote-agent-session-install-v1`n", [Text.UTF8Encoding]::new($false))
    Write-Output "Installed: $Binary"
    Write-Output 'Use the absolute executable path or add its bin directory to this terminal PATH. No profiles, services or existing installations were changed.'
} catch {
    if ($Created -and -not (Test-Path -LiteralPath (Join-Path $Destination '.installation-complete'))) {
        Remove-Item -LiteralPath $Destination -Recurse -Force
    }
    throw
}
