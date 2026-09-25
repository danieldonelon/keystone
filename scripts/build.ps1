$ErrorActionPreference = "Stop"
$root = Split-Path -Parent (Split-Path -Parent $MyInvocation.MyCommand.Path)
Set-Location $root
New-Item -ItemType Directory -Force -Path "$root\dist" | Out-Null
$env:CGO_ENABLED = "0"

function Build($goos, $goarch, $goarm, $out) {
    $env:GOOS = $goos
    $env:GOARCH = $goarch
    $env:GOARM = $goarm
    Write-Output "building $out"
    go build -trimpath -ldflags "-s -w" -o $out ./cmd/keystone
    if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }
}

Build "windows" "amd64" "" "$root\dist\keystone.exe"
Build "linux" "amd64" "" "$root\dist\keystone-linux-amd64"
Build "linux" "arm64" "" "$root\dist\keystone-linux-arm64"
Build "linux" "arm" "7" "$root\dist\keystone-linux-armv7"
Remove-Item Env:GOOS, Env:GOARCH, Env:GOARM -ErrorAction SilentlyContinue
Write-Output "done"
