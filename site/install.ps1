# Installs skilus on Windows from its GitHub releases.
#
#   irm https://skilus.dev/install.ps1 | iex
#
# Environment variables:
#   SKILUS_VERSION      version to install, such as 1.0.0 (default: the latest release)
#   SKILUS_INSTALL_DIR  folder for skilus.exe (default: %LOCALAPPDATA%\Programs\skilus),
#                       added to your user PATH
#
# The archive is checked against the release's checksums.txt before anything
# is installed. On macOS and Linux, use https://skilus.dev/install.

# A script block keeps variables out of the caller's session, and errors are
# thrown rather than calling exit, which would close the window under iex.
& {
    $ErrorActionPreference = 'Stop'
    $ProgressPreference = 'SilentlyContinue'
    $repo = 'colybri/skilus'
    [Net.ServicePointManager]::SecurityProtocol = [Net.ServicePointManager]::SecurityProtocol -bor [Net.SecurityProtocolType]::Tls12

    if (-not [Environment]::Is64BitOperatingSystem) { throw 'skilus needs 64-bit Windows' }
    $cpu = if ($env:PROCESSOR_ARCHITEW6432) { $env:PROCESSOR_ARCHITEW6432 } else { $env:PROCESSOR_ARCHITECTURE }
    $arch = if ($cpu -eq 'ARM64') { 'arm64' } else { 'amd64' }

    $version = "$env:SKILUS_VERSION".TrimStart('v')
    if (-not $version) {
        # /releases/latest redirects to /releases/tag/vX.Y.Z.
        $resp = Invoke-WebRequest -UseBasicParsing -Method Head "https://github.com/$repo/releases/latest"
        $uri = if ($resp.BaseResponse.ResponseUri) { $resp.BaseResponse.ResponseUri } else { $resp.BaseResponse.RequestMessage.RequestUri }
        $tag = $uri.AbsoluteUri.Split('/')[-1]
        if ($tag -notmatch '^v\d') { throw "could not find the latest release of skilus" }
        $version = $tag.Substring(1)
    }

    $dir = if ($env:SKILUS_INSTALL_DIR) { $env:SKILUS_INSTALL_DIR } else { Join-Path $env:LOCALAPPDATA 'Programs\skilus' }
    $file = "skilus_${version}_windows_$arch.zip"
    $base = "https://github.com/$repo/releases/download/v$version"
    $tmp = Join-Path ([IO.Path]::GetTempPath()) ([IO.Path]::GetRandomFileName())
    New-Item -ItemType Directory -Path $tmp | Out-Null
    try {
        Write-Host "Downloading skilus $version for windows/$arch"
        Invoke-WebRequest -UseBasicParsing "$base/$file" -OutFile (Join-Path $tmp $file)
        Invoke-WebRequest -UseBasicParsing "$base/checksums.txt" -OutFile (Join-Path $tmp 'checksums.txt')

        $want = Get-Content (Join-Path $tmp 'checksums.txt') |
            ForEach-Object { $f = $_ -split '\s+'; if ($f[1] -eq $file) { $f[0] } } |
            Select-Object -First 1
        if (-not $want) { throw "$file is not listed in checksums.txt" }
        $got = (Get-FileHash -Algorithm SHA256 (Join-Path $tmp $file)).Hash.ToLower()
        if ($got -ne $want.ToLower()) { throw "checksum mismatch for ${file}: expected $want, got $got" }

        Expand-Archive -Path (Join-Path $tmp $file) -DestinationPath (Join-Path $tmp 'out') -Force
        New-Item -ItemType Directory -Force -Path $dir | Out-Null
        Copy-Item -Force (Join-Path $tmp 'out\skilus.exe') (Join-Path $dir 'skilus.exe')
    } finally {
        Remove-Item -Recurse -Force $tmp -ErrorAction SilentlyContinue
    }

    $userPath = [Environment]::GetEnvironmentVariable('Path', 'User')
    if (-not (($userPath -split ';') -contains $dir)) {
        $joined = if ($userPath) { "$userPath;$dir" } else { $dir }
        [Environment]::SetEnvironmentVariable('Path', $joined, 'User')
        Write-Host "Added $dir to your user PATH. Open a new terminal to use it everywhere."
    }
    if (-not (($env:Path -split ';') -contains $dir)) { $env:Path += ";$dir" }

    $installed = & (Join-Path $dir 'skilus.exe') --version
    Write-Host "Installed $installed in $(Join-Path $dir 'skilus.exe')"
    Write-Host "Run 'skilus agents' to see the agents it found."
}
