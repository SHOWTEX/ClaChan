# Cross-compile Clachan for the Pi. Tagged releases are built by the GitHub
# workflow; this script is the local linux/arm64 build. A bare `go build`
# produces a Windows binary that fails on the Pi with "Exec format error".
#
#   .\build.ps1
#
# Checks gofmt and vet first, then builds linux/arm64 with CGO disabled.
# Cross-compile variables are cleared on exit so `go run .` works afterwards.

param(
    [switch]$SkipChecks
)

$ErrorActionPreference = "Stop"
Set-Location $PSScriptRoot

# A previously exported GOOS/GOARCH would leak into the checks and make them
# try to run ARM test binaries on this machine.
Remove-Item Env:GOOS, Env:GOARCH, Env:CGO_ENABLED -ErrorAction SilentlyContinue

try {
    if (-not $SkipChecks) {
        Write-Host "==> gofmt"
        $unformatted = gofmt -l .
        if ($unformatted) {
            $unformatted | ForEach-Object { Write-Host "    needs formatting: $_" -ForegroundColor Red }
            throw "gofmt found unformatted files"
        }

        Write-Host "==> go vet"
        go vet ./...
        if ($LASTEXITCODE -ne 0) { throw "go vet failed" }

        Write-Host "==> go test"
        go test ./...
        if ($LASTEXITCODE -ne 0) { throw "go test failed" }
    }

    $buildVersion = "dev"
    try {
        $described = git describe --tags --always --dirty 2>$null
        if ($LASTEXITCODE -eq 0 -and $described) {
            $buildVersion = $described.Trim() -replace '^v', ''
        }
    } catch {
        # A source tarball without git metadata still builds as "dev".
    }

    Write-Host "==> build linux/arm64 ($buildVersion)"
    $env:GOOS = "linux"
    $env:GOARCH = "arm64"
    $env:CGO_ENABLED = "0"

    go build -trimpath -ldflags "-s -w -X main.version=$buildVersion" -o clachan .
    if ($LASTEXITCODE -ne 0) { throw "go build failed" }

    $meta = (go version -m .\clachan) -join "`n"
    if ($meta -notmatch "GOOS=linux" -or $meta -notmatch "GOARCH=arm64") {
        throw "built binary is not linux/arm64:`n$meta"
    }

    $size = [math]::Round((Get-Item .\clachan).Length / 1MB, 1)
    Write-Host ""
    Write-Host "Built .\clachan ($size MB, linux/arm64, $buildVersion, CGO_ENABLED=0)" -ForegroundColor Green
    Write-Host "Deploy: use install.sh, or scp .\clachan pi@<host>:/opt/clachan/clachan for a manual install"
} finally {
    # Leave the caller's shell on the host toolchain. This is what makes
    # `.\build.ps1` followed by `go run .` work.
    Remove-Item Env:GOOS, Env:GOARCH, Env:CGO_ENABLED -ErrorAction SilentlyContinue
}
