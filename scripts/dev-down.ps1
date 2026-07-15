<#
.SYNOPSIS
    Tears down the Ses'fikile local dev stack started by scripts/dev-up.ps1.

.DESCRIPTION
    Stops the backend server and the three Vite dev servers (identified by
    the ports they listen on, not by tracked PIDs - robust even if a window
    was closed manually or restarted), then stops the Postgres container.

.PARAMETER WipeDb
    Also runs 'docker compose down -v', which DELETES the Postgres volume
    (all seeded/imported data, gone). Destructive. Requires typed
    confirmation unless -Force is also passed.

.PARAMETER Force
    Skip the confirmation prompt for -WipeDb. Has no effect without -WipeDb.

.EXAMPLE
    .\scripts\dev-down.ps1
.EXAMPLE
    .\scripts\dev-down.ps1 -WipeDb
#>

param(
    [switch]$WipeDb,
    [switch]$Force
)

# Not "Stop": docker routinely writes informational text to stderr, and
# under $ErrorActionPreference = "Stop" Windows PowerShell 5.1 treats that as
# a terminating NativeCommandError even on exit code 0.
$ErrorActionPreference = "Continue"

$RepoRoot = Split-Path -Parent $PSScriptRoot
$InfraDir = Join-Path $RepoRoot "infra"

function Write-Step($msg) {
    Write-Host ""
    Write-Host "==> $msg" -ForegroundColor Cyan
}

# ---------------------------------------------------------------------------
# 1. Stop dev servers by the ports they listen on
# ---------------------------------------------------------------------------
$ports = @(
    @{ Port = 8080; Label = "backend" }
    @{ Port = 5174; Label = "driver" }
    @{ Port = 5175; Label = "commuter" }
    @{ Port = 5176; Label = "owner" }
)

Write-Step "Stopping dev servers"
$stoppedAny = $false
foreach ($p in $ports) {
    $conns = Get-NetTCPConnection -LocalPort $p.Port -State Listen -ErrorAction SilentlyContinue
    if (-not $conns) {
        Write-Host "  $($p.Label): nothing listening on port $($p.Port), skipping."
        continue
    }
    $pids = $conns | Select-Object -ExpandProperty OwningProcess -Unique
    foreach ($procId in $pids) {
        $proc = Get-Process -Id $procId -ErrorAction SilentlyContinue
        if ($proc) {
            Write-Host "  $($p.Label): stopping PID $procId ($($proc.ProcessName)) on port $($p.Port)"
            Stop-Process -Id $procId -Force -ErrorAction SilentlyContinue
            $stoppedAny = $true
        }
    }
}
if (-not $stoppedAny) {
    Write-Host "  No dev-server processes were found running." -ForegroundColor DarkGray
}

# ---------------------------------------------------------------------------
# 2. Stop Postgres
# ---------------------------------------------------------------------------
if ($WipeDb) {
    Write-Host ""
    Write-Host "!!! -WipeDb requested: this will run 'docker compose down -v' !!!" -ForegroundColor Red
    Write-Host "!!! This PERMANENTLY DELETES the Postgres volume: every seeded/imported row is gone. !!!" -ForegroundColor Red
    if (-not $Force) {
        $confirmation = Read-Host "Type 'yes' to confirm you want to wipe the database volume"
        if ($confirmation -ne "yes") {
            Write-Host "Aborted. Postgres container will be stopped WITHOUT wiping data (plain 'docker compose down')." -ForegroundColor Yellow
            $WipeDb = $false
        }
    } else {
        Write-Host "(-Force passed, skipping confirmation prompt)" -ForegroundColor Yellow
    }
}

Write-Step $(if ($WipeDb) { "Stopping Postgres and wiping its volume (docker compose down -v)" } else { "Stopping Postgres (docker compose down)" })
Push-Location $InfraDir
try {
    if ($WipeDb) {
        docker compose down -v
    } else {
        docker compose down
    }
} finally {
    Pop-Location
}

Write-Host ""
Write-Host "Ses'fikile local stack stopped." -ForegroundColor Green
if ($WipeDb) {
    Write-Host "Database volume wiped - next dev-up.ps1 will start from a genuinely empty DB." -ForegroundColor Yellow
}
