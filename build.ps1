#Requires -Version 5.1
<#
.SYNOPSIS
    Builds windock.exe and packages it into an installer.

.DESCRIPTION
    Runs the tests, builds the executable and compiles installer\windock.iss
    into dist\windock-setup-<version>.exe.

.PARAMETER Version
    Version to stamp on the installer. Defaults to the one in windock.iss.

.PARAMETER ExeOnly
    Build windock.exe and stop, without touching the installer.

.PARAMETER SkipTests
    Skip go test.

.EXAMPLE
    .\build.ps1
.EXAMPLE
    .\build.ps1 -Version 0.1.0
#>
[CmdletBinding()]
param(
    [string]$Version,
    [switch]$ExeOnly,
    [switch]$SkipTests
)

Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'

$Root = Split-Path -Parent $PSCommandPath
$Exe = Join-Path $Root 'windock.exe'
$Script = Join-Path $Root 'installer\windock.iss'
$Dist = Join-Path $Root 'dist'

# Native tools report failure through the exit code, which PowerShell will
# otherwise sail straight past.
function Invoke-Tool {
    param([string]$What, [scriptblock]$Command)
    & $Command
    if ($LASTEXITCODE -ne 0) {
        throw "$What failed with exit code $LASTEXITCODE"
    }
}

function Find-ISCC {
    if ($env:ISCC -and (Test-Path $env:ISCC)) { return $env:ISCC }

    $onPath = Get-Command iscc -ErrorAction SilentlyContinue
    if ($onPath) { return $onPath.Source }

    $candidates = @(
        "$env:LOCALAPPDATA\Programs\Inno Setup 6\ISCC.exe"
        "${env:ProgramFiles(x86)}\Inno Setup 6\ISCC.exe"
        "$env:ProgramFiles\Inno Setup 6\ISCC.exe"
    )
    foreach ($c in $candidates) {
        if (Test-Path $c) { return $c }
    }

    throw @"
Inno Setup's compiler (ISCC.exe) was not found.

Install it with:  winget install JRSoftware.InnoSetup
or point `$env:ISCC at an existing copy.
"@
}

Push-Location $Root
try {
    if (-not $SkipTests) {
        Write-Host '==> go test ./...' -ForegroundColor Cyan
        Invoke-Tool 'go test' { go test ./... }
    }

    Write-Host '==> go build' -ForegroundColor Cyan
    Invoke-Tool 'go build' { go build -o $Exe ./cmd/windock }
    Write-Host ("    {0} ({1:N1} MB)" -f $Exe, ((Get-Item $Exe).Length / 1MB))

    if ($ExeOnly) {
        Write-Host 'Done (executable only).' -ForegroundColor Green
        return
    }

    $iscc = Find-ISCC
    Write-Host "==> $iscc" -ForegroundColor Cyan

    # /Q leaves only warnings and errors; the summary below is friendlier than
    # the compiler's own hundred lines of progress.
    $isccArgs = @('/Q', $Script)
    if ($Version) {
        $isccArgs = @('/Q', "/DAppVersion=$Version", $Script)
    }
    Invoke-Tool 'ISCC' { & $iscc $isccArgs }

    $setup = Get-ChildItem (Join-Path $Dist 'windock-setup-*.exe') |
        Sort-Object LastWriteTime -Descending |
        Select-Object -First 1
    Write-Host ("    {0} ({1:N1} MB)" -f $setup.FullName, ($setup.Length / 1MB))
    Write-Host 'Done.' -ForegroundColor Green
}
finally {
    Pop-Location
}
