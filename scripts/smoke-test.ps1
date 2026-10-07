# End-to-end check of an installed localdns.exe on Windows. Works on a
# temporary hosts file, so the real hosts file is never touched.
$ErrorActionPreference = 'Stop'
$PSNativeCommandUseErrorActionPreference = $false

$work = Join-Path ([IO.Path]::GetTempPath()) ("localdns-smoke-" + [Guid]::NewGuid().ToString('N'))
New-Item -ItemType Directory -Path $work | Out-Null
$env:LOCALDNS_HOSTS_FILE = Join-Path $work 'hosts'
$env:LOCALDNS_CONFIG_DIR = Join-Path $work 'config'
$env:NO_COLOR = '1'
$original = "# Copyright (c) 1993-2009 Microsoft Corp.`r`n127.0.0.1       localhost`r`n10.0.0.5        nas.lan`r`n"
[IO.File]::WriteAllText($env:LOCALDNS_HOSTS_FILE, $original)

function Step($msg) { Write-Host "`n==> $msg" }
function Fail($msg) { throw "FAIL: $msg" }
function Run([string[]]$cmdArgs, [int]$expect = 0) {
    $out = (& localdns @cmdArgs | Out-String)
    Write-Host $out
    if ($LASTEXITCODE -ne $expect) { Fail "localdns $($cmdArgs -join ' ') exited $LASTEXITCODE (expected $expect)" }
    return $out
}
function HostsContent { [IO.File]::ReadAllText($env:LOCALDNS_HOSTS_FILE) }

$bin = (Get-Command localdns -ErrorAction SilentlyContinue).Source
if (-not $bin) { Fail 'localdns is not on PATH' }
Step "localdns at $bin"

Step 'localdns info'
$out = Run @('info')
if ($out -notmatch 'COMMANDS') { Fail 'info output' }

Step 'localdns add test.local 127.0.0.1:3000'
$out = Run @('add', 'test.local', '127.0.0.1:3000')
if ($out -notmatch 'http://test.local:3000') { Fail 'add output' }
if ((HostsContent) -notmatch "127\.0\.0\.1 test\.local`r`n") { Fail 'hosts entry missing or wrong line ending' }

Step 'localdns list --json'
$list = Run @('list', '--json') | ConvertFrom-Json
if ($list.entries.Count -ne 1) { Fail 'expected one entry' }
$e = $list.entries[0]
if ($e.hostname -ne 'test.local' -or $e.port -ne 3000 -or $e.url -ne 'http://test.local:3000' -or $e.status -ne 'active') { Fail "unexpected entry: $($e | ConvertTo-Json)" }

Step 'localdns status --json'
$status = Run @('status', '--json') | ConvertFrom-Json
if (-not $status.healthy) { Fail 'status not healthy' }

Step 'localdns doctor'
Run @('doctor') | Out-Null

Step 'localdns remove test.local without --yes (must not prompt)'
$null = ('' | & localdns remove test.local)
if ($LASTEXITCODE -ne 7) { Fail "expected exit code 7, got $LASTEXITCODE" }

Step 'localdns remove test.local --yes'
Run @('remove', 'test.local', '--yes') | Out-Null
if ((HostsContent) -ne $original) { Fail 'hosts file not restored after remove' }

Step 'localdns uninstall --yes'
Run @('add', 'keep.local', '192.168.1.60') | Out-Null
Run @('uninstall', '--yes') | Out-Null
if ((HostsContent) -ne $original) { Fail 'uninstall changed entries it does not own' }
if (Test-Path $env:LOCALDNS_CONFIG_DIR) { Fail 'config directory still exists' }

# The running .exe is deleted by a helper process shortly after exit.
for ($i = 0; $i -lt 30 -and (Test-Path $bin); $i++) { Start-Sleep -Milliseconds 500 }
if (Test-Path $bin) { Fail "binary still exists at $bin" }
$dir = Split-Path $bin
$userPath = [Environment]::GetEnvironmentVariable('Path', 'User')
if ($userPath -and (($userPath -split ';') | Where-Object { $_.TrimEnd('\') -eq $dir.TrimEnd('\') })) { Fail 'install dir still in user PATH' }

Remove-Item -Recurse -Force $work
Write-Host "`nOK: smoke test passed"
