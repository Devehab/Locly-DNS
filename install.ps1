# LocalDNS installer for Windows.
#
#   irm https://github.com/Devehab/Locly-DNS/releases/latest/download/install.ps1 | iex
#
# Downloads localdns.exe for your CPU from GitHub Releases, verifies its
# SHA-256 checksum, copies it to %LOCALAPPDATA%\Programs\LocalDNS and adds that
# folder to your user PATH. No DNS settings, no administrator rights needed to
# install.
#
# It also turns on port-free URLs (`localdns router enable`): a small router
# listening on 127.0.0.1:80 only, started at login for your user, so
# http://app.local opens your app without typing :3000. Turn it off any time
# with `localdns router disable`.
#
# Environment variables:
#   LOCALDNS_VERSION      version tag to install, e.g. v0.1.0 (default: latest)
#   LOCALDNS_INSTALL_DIR  install directory
#   LOCALDNS_REPO         GitHub repository (default: Devehab/Locly-DNS)
#   LOCALDNS_BASE_URL     download from this URL instead of GitHub Releases
#   LOCALDNS_NO_ROUTER    set to 1 to skip turning on port-free URLs

& {
    $ErrorActionPreference = 'Stop'
    $ProgressPreference = 'SilentlyContinue'
    [Net.ServicePointManager]::SecurityProtocol = [Net.ServicePointManager]::SecurityProtocol -bor [Net.SecurityProtocolType]::Tls12

    $repo = if ($env:LOCALDNS_REPO) { $env:LOCALDNS_REPO } else { 'Devehab/Locly-DNS' }
    $version = if ($env:LOCALDNS_VERSION) { $env:LOCALDNS_VERSION } else { 'latest' }
    if ($env:LOCALDNS_BASE_URL) {
        $base = $env:LOCALDNS_BASE_URL.TrimEnd('/')
    } elseif ($version -eq 'latest') {
        $base = "https://github.com/$repo/releases/latest/download"
    } else {
        $base = "https://github.com/$repo/releases/download/$version"
    }

    $cpu = $env:PROCESSOR_ARCHITECTURE
    if ($env:PROCESSOR_ARCHITEW6432) { $cpu = $env:PROCESSOR_ARCHITEW6432 }
    switch ($cpu) {
        'AMD64' { $arch = 'amd64' }
        'ARM64' { $arch = 'arm64' }
        default { throw "LocalDNS install: unsupported CPU architecture '$cpu'" }
    }

    $asset = "localdns_windows_$arch.zip"
    $installDir = if ($env:LOCALDNS_INSTALL_DIR) { $env:LOCALDNS_INSTALL_DIR } else { Join-Path $env:LOCALAPPDATA 'Programs\LocalDNS' }
    $tmp = Join-Path ([IO.Path]::GetTempPath()) ("localdns-install-" + [Guid]::NewGuid().ToString('N'))
    New-Item -ItemType Directory -Path $tmp | Out-Null

    try {
        Write-Host "Downloading LocalDNS (windows/$arch)..."
        Invoke-WebRequest -UseBasicParsing -Uri "$base/$asset" -OutFile (Join-Path $tmp $asset)
        Invoke-WebRequest -UseBasicParsing -Uri "$base/checksums.txt" -OutFile (Join-Path $tmp 'checksums.txt')

        $expected = $null
        foreach ($line in Get-Content (Join-Path $tmp 'checksums.txt')) {
            $parts = $line -split '\s+'
            if ($parts.Count -ge 2 -and $parts[1].TrimStart('*') -eq $asset) { $expected = $parts[0].ToLower() }
        }
        if (-not $expected) { throw "LocalDNS install: no checksum for $asset in checksums.txt" }
        $actual = (Get-FileHash -Algorithm SHA256 -Path (Join-Path $tmp $asset)).Hash.ToLower()
        if ($expected -ne $actual) { throw "LocalDNS install: checksum mismatch for $asset (expected $expected, got $actual)" }
        Write-Host 'Checksum verified.'

        Expand-Archive -Path (Join-Path $tmp $asset) -DestinationPath (Join-Path $tmp 'x') -Force
        New-Item -ItemType Directory -Path $installDir -Force | Out-Null
        $exe = Join-Path $installDir 'localdns.exe'
        Copy-Item -Path (Join-Path $tmp 'x\localdns.exe') -Destination $exe -Force

        $userPath = [Environment]::GetEnvironmentVariable('Path', 'User')
        $entries = @()
        if ($userPath) { $entries = $userPath -split ';' | Where-Object { $_ } }
        if (-not ($entries | Where-Object { $_.TrimEnd('\') -eq $installDir.TrimEnd('\') })) {
            [Environment]::SetEnvironmentVariable('Path', (($entries + $installDir) -join ';'), 'User')
            Write-Host "Added $installDir to your user PATH."
        }
        if (-not (($env:Path -split ';') | Where-Object { $_.TrimEnd('\') -eq $installDir.TrimEnd('\') })) {
            $env:Path = "$env:Path;$installDir"
        }

        $installed = & $exe version
        if ($LASTEXITCODE -ne 0) { throw "LocalDNS install: installed binary does not run: $exe" }

        Write-Host ''
        Write-Host "OK: $installed installed to $exe"
        Write-Host ''
        Write-Host 'Get started (open a new terminal first if localdns is not found):'
        Write-Host '  localdns info'
        Write-Host '  localdns add app.local 127.0.0.1:3000'
        Write-Host '  localdns ui            # dashboard at http://127.0.0.1:7357'
        Write-Host ''
        Write-Host 'Adding or removing names edits the hosts file, so run localdns from a terminal'
        Write-Host 'opened with "Run as administrator" (reading works in any terminal).'

        if (-not $env:CI -and -not $env:LOCALDNS_NO_ROUTER -and [Environment]::UserInteractive) {
            Write-Host ''
            Write-Host 'Turning on port-free URLs (http://app.local instead of http://app.local:3000)...'
            & $exe router enable
            if ($LASTEXITCODE -ne 0) { Write-Host 'Skipped. You can turn them on later with: localdns router enable' }
        }
    } finally {
        Remove-Item -Recurse -Force -Path $tmp -ErrorAction SilentlyContinue
    }
}
