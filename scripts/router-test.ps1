# Starts the real port-free router as a Windows login item, opens a name
# through it without typing a port, then removes it with `router disable`
# and with `uninstall`. The real hosts file is never touched; entries live in
# a sandbox.
#
#   ./scripts/router-test.ps1 -Bin <path-to-localdns.exe> -Sandbox <dir>
param(
    [Parameter(Mandatory = $true)][string]$Bin,
    [Parameter(Mandatory = $true)][string]$Sandbox
)
$ErrorActionPreference = 'Stop'
$appPort = 3457
$marker = 'hello through the LocalDNS router'

function Step($text) { Write-Host ''; Write-Host "== $text" }

function Get-Through($hostName) {
    $out = & curl.exe -fsS -m 3 -H "Host: $hostName" http://127.0.0.1/ 2>$null
    if ($LASTEXITCODE -ne 0) { return $null }
    return ($out -join "`n").Trim()
}

function Test-Answering {
    & curl.exe -s -o NUL -m 2 http://127.0.0.1/__localdns/router 2>$null
    return $LASTEXITCODE -eq 0
}

function Wait-Router {
    for ($i = 0; $i -lt 40; $i++) {
        if ((Get-Through 'app.test') -eq $marker) { return }
        Start-Sleep -Milliseconds 500
    }
    & $Bin router
    throw 'http://app.test (no port) did not reach the app'
}

function Invoke-Localdns {
    & $Bin @args
    if ($LASTEXITCODE -ne 0) { throw "localdns $($args -join ' ') failed with exit code $LASTEXITCODE" }
}

Remove-Item -Recurse -Force $Sandbox -ErrorAction SilentlyContinue
New-Item -ItemType Directory -Force -Path (Join-Path $Sandbox 'www') | Out-Null
Set-Content -Path (Join-Path $Sandbox 'hosts') -Value '127.0.0.1 localhost'
Set-Content -Path (Join-Path $Sandbox 'www\index.html') -Value $marker -NoNewline
$paths = @("--hosts-file=$(Join-Path $Sandbox 'hosts')", "--config-dir=$(Join-Path $Sandbox 'cfg')")

$busy = Get-NetTCPConnection -LocalPort 80 -State Listen -ErrorAction SilentlyContinue
if ($busy) {
    $busy | Format-Table -AutoSize | Out-String | Write-Host
    throw 'something already listens on port 80'
}

$app = Start-Process -FilePath python -ArgumentList '-m', 'http.server', "$appPort", '--bind', '127.0.0.1', '--directory', (Join-Path $Sandbox 'www') -PassThru -WindowStyle Hidden
try {
    Step "add app.test -> 127.0.0.1:$appPort"
    Invoke-Localdns add app.test "127.0.0.1:$appPort" @paths

    Step 'localdns router enable'
    Invoke-Localdns router enable @paths
    Wait-Router
    Write-Host "OK http://app.test -> $(Get-Through 'app.test')"

    Step 'unknown names are not forwarded'
    $code = & curl.exe -s -o NUL -w '%{http_code}' -m 3 -H 'Host: other.test' http://127.0.0.1/
    if ($code -ne '404') { throw "other.test answered $code, want 404" }

    Step 'list shows the port-free URL'
    $list = (& $Bin list --json @paths) -join "`n" | ConvertFrom-Json
    if ($list.entries[0].short_url -ne 'http://app.test') { throw "short_url = '$($list.entries[0].short_url)'" }
    Invoke-Localdns router

    Step 'localdns router disable'
    Invoke-Localdns router disable
    Start-Sleep -Seconds 2
    if (Test-Answering) { throw 'the router still answers after disable' }
    Write-Host 'OK removed'

    Step 'uninstall removes the router too'
    Invoke-Localdns router enable @paths
    Wait-Router
    Invoke-Localdns uninstall --yes --keep-binary @paths
    Start-Sleep -Seconds 2
    if (Test-Answering) { throw 'the router still answers after uninstall' }
    Write-Host 'OK the router works end to end'
} finally {
    Stop-Process -Id $app.Id -ErrorAction SilentlyContinue
    & $Bin router disable *> $null
}
