$ErrorActionPreference = "Stop"

$sdkRoot = $env:PLAYDATE_SDK_PATH
if (-not $sdkRoot) {
    $candidates = @()
    if ($env:LOCALAPPDATA) { $candidates += Join-Path $env:LOCALAPPDATA "Programs\PlaydateSDK" }
    if ($env:USERPROFILE) {
        $candidates += Join-Path $env:USERPROFILE "Documents\PlaydateSDK"
        $candidates += Join-Path $env:USERPROFILE "PlaydateSDK"
    }
    if ($env:ProgramFiles) { $candidates += Join-Path $env:ProgramFiles "PlaydateSDK" }

    foreach ($candidate in $candidates) {
        if ((Test-Path (Join-Path $candidate "bin\pdc.exe")) -and
            (Test-Path (Join-Path $candidate "bin\PlaydateSimulator.exe"))) {
            $sdkRoot = $candidate
            break
        }
    }
}

if (-not $sdkRoot -or
    -not (Test-Path (Join-Path $sdkRoot "bin\pdc.exe")) -or
    -not (Test-Path (Join-Path $sdkRoot "bin\PlaydateSimulator.exe"))) {
    throw "Playdate SDK not found. Set PLAYDATE_SDK_PATH or install it in the default Documents\PlaydateSDK location."
}
$env:PLAYDATE_SDK_PATH = $sdkRoot

if (-not (Get-Command go -ErrorAction SilentlyContinue)) {
    throw "Go is not on PATH. Install Go 1.26.5 or newer before running this test."
}

$repoRoot = Split-Path -Parent $PSScriptRoot
Push-Location $repoRoot
try {
    Write-Host "Testing Windows native MCP against SDK at $sdkRoot"

    & go test ./...
    if ($LASTEXITCODE -ne 0) { throw "go test failed with exit code $LASTEXITCODE" }

    & go vet ./...
    if ($LASTEXITCODE -ne 0) { throw "go vet failed with exit code $LASTEXITCODE" }

    & go test -tags=native ./internal/contracttest -run TestWindowsNativeMCP -count=1 -v
    if ($LASTEXITCODE -ne 0) { throw "Windows native MCP contract test failed with exit code $LASTEXITCODE" }
}
finally {
    Pop-Location
}
