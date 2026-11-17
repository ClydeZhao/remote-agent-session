[CmdletBinding()]
param([Parameter(ValueFromRemainingArguments = $true)][string[]]$LifecycleArguments)
$ErrorActionPreference = 'Stop'
Set-StrictMode -Version Latest
$Root = Split-Path -Parent $PSScriptRoot
$Marker = Join-Path $Root '.installation-complete'
if (-not (Test-Path -LiteralPath $Marker) -or [IO.File]::ReadAllText($Marker).Trim() -ne 'remote-agent-session-install-v1') {
    throw 'Refusing to remove an unowned directory.'
}
& (Join-Path $Root '.venv\Scripts\python.exe') (Join-Path $Root 'tools\lifecycle.py') stop-owned @LifecycleArguments
if ($LASTEXITCODE -ne 0) { throw 'Owned service cleanup failed; installation and private state retained.' }
Write-Output "Reviewed installation cleanup: $Root"
Write-Output 'After detaching owned sessions and removing owned hooks, explicitly delete only this installation directory. Client and service state are retained. No login task or Windows service was installed.'
