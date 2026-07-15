<#
.SYNOPSIS
    Brings up the full Ses'fikile local dev stack: Postgres (Docker), the Go
    backend, and all three Vite frontend apps (driver/commuter/owner).

.DESCRIPTION
    Replaces manually juggling ~5 terminals. Starts Postgres via
    infra/docker-compose.yml and waits for it to report healthy, runs
    cmd/seed (migrations run automatically on startup), then launches the
    backend server and the three frontend dev servers each in their own
    PowerShell window so logs stay readable and each can be closed/Ctrl+C'd
    independently.

.PARAMETER WithCatalogue
    Also runs cmd/importcatalogue after seeding, loading the real ~1447-route
    City of Cape Town catalogue on top of the clean 8-route/12-stop baseline.
    Requires backend/data/taxi_routes.json locally (see backend/data/README.md)
    - this file is large and gitignored, not fetched automatically.

.PARAMETER SkipSeed
    Skip running cmd/seed. Useful on a second run against a DB that's already
    seeded, to save a few seconds.

.EXAMPLE
    .\scripts\dev-up.ps1
.EXAMPLE
    .\scripts\dev-up.ps1 -WithCatalogue
#>

param(
    [switch]$WithCatalogue,
    [switch]$SkipSeed
)

# Not "Stop": native commands (docker, go, npm) routinely write informational
# text to stderr, and under $ErrorActionPreference = "Stop" Windows PowerShell
# 5.1 treats that as a terminating NativeCommandError even on exit code 0.
# Every native call below is checked explicitly via $LASTEXITCODE instead.
$ErrorActionPreference = "Continue"

$RepoRoot = Split-Path -Parent $PSScriptRoot
$BackendDir = Join-Path $RepoRoot "backend"
$InfraDir = Join-Path $RepoRoot "infra"
$Apps = @(
    @{ Name = "driver";   Dir = Join-Path $RepoRoot "apps\driver";   Port = 5174 }
    @{ Name = "commuter"; Dir = Join-Path $RepoRoot "apps\commuter"; Port = 5175 }
    @{ Name = "owner";    Dir = Join-Path $RepoRoot "apps\owner";    Port = 5176 }
)
$BackendPort = 8080

function Write-Step($msg) {
    Write-Host ""
    Write-Host "==> $msg" -ForegroundColor Cyan
}

function Write-ErrorAndExit($msg) {
    Write-Host ""
    Write-Host "ERROR: $msg" -ForegroundColor Red
    exit 1
}

# ---------------------------------------------------------------------------
# 1. Docker sanity checks
# ---------------------------------------------------------------------------
Write-Step "Checking Docker is running"
docker info *> $null
if ($LASTEXITCODE -ne 0) {
    Write-ErrorAndExit "Docker does not appear to be running. Start Docker Desktop and try again."
}

# Known local quirk (see CLAUDE.md / docs/PROGRESS.md): this dev machine also
# runs a native Windows PostgreSQL service bound to port 5432, which shadows
# the Docker container on localhost:5432 if both try to listen at once.
$listeners = Get-NetTCPConnection -LocalPort 5432 -State Listen -ErrorAction SilentlyContinue
foreach ($conn in $listeners) {
    $proc = Get-Process -Id $conn.OwningProcess -ErrorAction SilentlyContinue
    if ($proc -and $proc.ProcessName -notmatch "com\.docker|docker|vpnkit|wslrelay|wsl") {
        Write-ErrorAndExit @"
Port 5432 is already in use by '$($proc.ProcessName)' (PID $($proc.Id)), not Docker.
This machine has a native Windows PostgreSQL service that shadows the Docker
Postgres container on port 5432 (documented in CLAUDE.md). Stop that service
first, e.g.:
    Get-Service | Where-Object { `$_.Name -like 'postgresql*' }
    Stop-Service <service-name>
then re-run this script.
"@
    }
}

# ---------------------------------------------------------------------------
# 2. Start Postgres and wait for it to be healthy (poll, don't sleep blindly)
# ---------------------------------------------------------------------------
Write-Step "Starting Postgres (docker compose)"
Push-Location $InfraDir
try {
    docker compose up -d
    if ($LASTEXITCODE -ne 0) {
        Write-ErrorAndExit "docker compose up failed. Check the output above (often port 5432 already allocated)."
    }
} finally {
    Pop-Location
}

Write-Step "Waiting for Postgres to report healthy"
$healthy = $false
$timeoutSeconds = 60
$elapsed = 0
while ($elapsed -lt $timeoutSeconds) {
    $status = docker inspect --format='{{.State.Health.Status}}' sesfikile-postgres 2>$null
    if ($status -eq "healthy") {
        $healthy = $true
        break
    }
    Start-Sleep -Seconds 2
    $elapsed += 2
}
if (-not $healthy) {
    Write-ErrorAndExit "Postgres did not become healthy within $timeoutSeconds seconds. Run 'docker compose logs postgres' in infra/ to investigate."
}
Write-Host "Postgres is healthy." -ForegroundColor Green

# ---------------------------------------------------------------------------
# 3. Seed (migrations run automatically on cmd/seed startup)
# ---------------------------------------------------------------------------
if (-not $SkipSeed) {
    Write-Step "Seeding dev data (cmd/seed) - migrations apply automatically"
    Push-Location $BackendDir
    try {
        go run ./cmd/seed
        if ($LASTEXITCODE -ne 0) {
            Write-ErrorAndExit "cmd/seed failed. See output above."
        }
    } finally {
        Pop-Location
    }
} else {
    Write-Step "Skipping seed (-SkipSeed)"
}

if ($WithCatalogue) {
    $cataloguePath = Join-Path $BackendDir "data\taxi_routes.json"
    if (-not (Test-Path $cataloguePath)) {
        Write-Host ""
        Write-Host "WARNING: -WithCatalogue was requested but $cataloguePath is missing." -ForegroundColor Yellow
        Write-Host "See backend/data/README.md for how to obtain it. Continuing without the catalogue." -ForegroundColor Yellow
    } else {
        Write-Step "Importing the real route catalogue (cmd/importcatalogue)"
        Push-Location $BackendDir
        try {
            go run ./cmd/importcatalogue
            if ($LASTEXITCODE -ne 0) {
                Write-ErrorAndExit "cmd/importcatalogue failed. See output above."
            }
        } finally {
            Pop-Location
        }
    }
}

# ---------------------------------------------------------------------------
# 4. node_modules sanity check for each frontend app
# ---------------------------------------------------------------------------
Write-Step "Checking frontend dependencies are installed"
$missingDeps = @()
foreach ($app in $Apps) {
    $nodeModules = Join-Path $app.Dir "node_modules"
    if (-not (Test-Path $nodeModules)) {
        $missingDeps += $app.Name
    }
}
if ($missingDeps.Count -gt 0) {
    $lines = $missingDeps | ForEach-Object { "    cd apps\$_; npm install; cd ..\..\" }
    Write-ErrorAndExit "node_modules missing for: $($missingDeps -join ', '). Run:`n$($lines -join "`n")`nthen re-run this script."
}

# ---------------------------------------------------------------------------
# 5. Launch backend + all three frontends, each in its own window
# ---------------------------------------------------------------------------
Write-Step "Starting backend server (new window)"
Start-Process powershell -ArgumentList @(
    "-NoExit", "-Command",
    "`$host.UI.RawUI.WindowTitle = 'sesfikile: backend (go run ./cmd/server)'; Set-Location '$BackendDir'; go run ./cmd/server"
) | Out-Null

foreach ($app in $Apps) {
    Write-Step "Starting $($app.Name) dev server (new window, port $($app.Port))"
    Start-Process powershell -ArgumentList @(
        "-NoExit", "-Command",
        "`$host.UI.RawUI.WindowTitle = 'sesfikile: $($app.Name) (npm run dev)'; Set-Location '$($app.Dir)'; npm run dev"
    ) | Out-Null
}

# ---------------------------------------------------------------------------
# 6. Poll until everything responds
# ---------------------------------------------------------------------------
Write-Step "Waiting for services to come up"

function Wait-ForHttp($url, $label, $timeoutSeconds = 45) {
    $elapsed = 0
    while ($elapsed -lt $timeoutSeconds) {
        try {
            $resp = Invoke-WebRequest -Uri $url -UseBasicParsing -TimeoutSec 3 -ErrorAction Stop
            if ($resp.StatusCode -ge 200 -and $resp.StatusCode -lt 500) {
                return $true
            }
        } catch {
            # Not up yet (or 503 while DB reconnects) - keep polling.
        }
        Start-Sleep -Seconds 1
        $elapsed += 1
    }
    Write-Host "  WARNING: $label did not respond within $timeoutSeconds seconds - check its window for errors." -ForegroundColor Yellow
    return $false
}

Wait-ForHttp "http://localhost:$BackendPort/health" "backend" | Out-Null
foreach ($app in $Apps) {
    Wait-ForHttp "http://localhost:$($app.Port)/" "$($app.Name) app" | Out-Null
}

# ---------------------------------------------------------------------------
# 7. Summary
# ---------------------------------------------------------------------------
Write-Host ""
Write-Host "=============================================================" -ForegroundColor Green
Write-Host " Ses'fikile local stack is up" -ForegroundColor Green
Write-Host "=============================================================" -ForegroundColor Green
Write-Host "  Backend:  http://localhost:$BackendPort  (health: /health)"
foreach ($app in $Apps) {
    Write-Host "  $($app.Name.PadRight(9)) http://localhost:$($app.Port)"
}
Write-Host ""
Write-Host "Each service is running in its own PowerShell window - close the" -ForegroundColor DarkGray
Write-Host "window or Ctrl+C inside it to stop that service individually, or" -ForegroundColor DarkGray
Write-Host "run .\scripts\dev-down.ps1 to stop everything at once." -ForegroundColor DarkGray
Write-Host ""
Write-Host "See docs/TESTING.md for the manual click-through test checklist." -ForegroundColor DarkGray
