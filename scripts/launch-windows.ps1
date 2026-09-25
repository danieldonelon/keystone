# Open Keystone. Starts it when it is not already running, otherwise opens the window.
$ErrorActionPreference = "Stop"
$root = Split-Path -Parent (Split-Path -Parent $MyInvocation.MyCommand.Path)
$exe = Join-Path $root "dist\keystone.exe"
if (-not (Test-Path $exe)) {
    Add-Type -AssemblyName System.Windows.Forms
    [System.Windows.Forms.MessageBox]::Show("Keystone.exe was not found at $exe", "Keystone")
    exit 1
}

$runtime = Join-Path $env:APPDATA "Keystone\runtime.json"
if (Test-Path $runtime) {
    try {
        $info = Get-Content $runtime -Raw | ConvertFrom-Json
        $running = Get-Process -Id $info.pid -ErrorAction SilentlyContinue
        if ($running -and $info.web) {
            Start-Process $info.web
            exit 0
        }
    } catch {
    }
}

Start-Process -FilePath $exe -ArgumentList "up" -WorkingDirectory $root
