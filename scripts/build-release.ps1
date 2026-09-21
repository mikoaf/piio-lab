[CmdletBinding()]
param(
    [Parameter(Position = 0)]
    [ValidatePattern('^v\d+\.\d+\.\d+(?:[-+][0-9A-Za-z.-]+)?$')]
    [string]$Version
)

$ErrorActionPreference = 'Stop'

$repoRoot = Split-Path -Parent $PSScriptRoot
$distDir = Join-Path $repoRoot 'dist'
$stagingDirs = [System.Collections.Generic.List[string]]::new()
$archives = [System.Collections.Generic.List[string]]::new()
$originalEnvironment = @{}
foreach ($name in 'CGO_ENABLED', 'GOOS', 'GOARCH', 'GOARM') {
    $originalEnvironment[$name] = [Environment]::GetEnvironmentVariable($name, 'Process')
}

Push-Location $repoRoot
try {
    Get-Command go -ErrorAction Stop | Out-Null
    Get-Command tar -ErrorAction Stop | Out-Null

    if ([string]::IsNullOrWhiteSpace($Version)) {
        $Version = (& git describe --tags --exact-match HEAD 2>$null).Trim()
        if ($LASTEXITCODE -ne 0 -or [string]::IsNullOrWhiteSpace($Version)) {
            throw 'HEAD tidak memiliki tag. Berikan versi, misalnya: .\scripts\build-release.ps1 v0.2.0'
        }
    }

    if ($Version -notmatch '^v\d+\.\d+\.\d+(?:[-+][0-9A-Za-z.-]+)?$') {
        throw "Format versi tidak valid: $Version"
    }

    if (git status --porcelain) {
        Write-Warning 'Working tree memiliki perubahan. Archive akan memakai isi working tree saat ini.'
    }

    New-Item -ItemType Directory -Force -Path $distDir | Out-Null

    $targets = @(
        @{ Name = 'linux-arm64'; GOARCH = 'arm64'; GOARM = $null },
        @{ Name = 'linux-armv7'; GOARCH = 'arm'; GOARM = '7' },
        @{ Name = 'linux-amd64'; GOARCH = 'amd64'; GOARM = $null }
    )

    foreach ($target in $targets) {
        $name = $target.Name
        $stageDir = Join-Path $distDir ".staging-$name"
        $archive = Join-Path $distDir "piio-lab-$Version-$name.tar.gz"

        if (Test-Path -LiteralPath $stageDir) {
            Remove-Item -LiteralPath $stageDir -Recurse -Force
        }
        if (Test-Path -LiteralPath $archive) {
            Remove-Item -LiteralPath $archive -Force
        }

        New-Item -ItemType Directory -Path $stageDir | Out-Null
        $stagingDirs.Add($stageDir)

        $env:CGO_ENABLED = '0'
        $env:GOOS = 'linux'
        $env:GOARCH = $target.GOARCH
        if ($null -eq $target.GOARM) {
            Remove-Item Env:GOARM -ErrorAction SilentlyContinue
        } else {
            $env:GOARM = $target.GOARM
        }

        Write-Host "Building $name..."
        & go build -trimpath -ldflags='-s -w' -o (Join-Path $stageDir 'piio-lab') .
        if ($LASTEXITCODE -ne 0) {
            throw "Build gagal untuk $name"
        }

        Copy-Item -LiteralPath (Join-Path $repoRoot 'config.json') -Destination $stageDir
        Copy-Item -LiteralPath (Join-Path $repoRoot 'README.md') -Destination $stageDir

        & tar -C $stageDir -czf $archive piio-lab config.json README.md
        if ($LASTEXITCODE -ne 0) {
            throw "Pembuatan archive gagal untuk $name"
        }

        $archives.Add($archive)
    }

    $checksumFile = Join-Path $distDir 'SHA256SUMS.txt'
    $checksumLines = $archives |
        Sort-Object { Split-Path -Leaf $_ } |
        ForEach-Object {
            $hash = (Get-FileHash -Algorithm SHA256 -LiteralPath $_).Hash.ToLowerInvariant()
            "$hash  $(Split-Path -Leaf $_)"
        }
    $checksumLines | Set-Content -LiteralPath $checksumFile -Encoding ascii

    Write-Host ''
    Write-Host "Release assets tersedia di $distDir"
    $archives | Sort-Object | ForEach-Object { Write-Host "- $(Split-Path -Leaf $_)" }
    Write-Host '- SHA256SUMS.txt'
}
finally {
    foreach ($stageDir in $stagingDirs) {
        if (Test-Path -LiteralPath $stageDir) {
            Remove-Item -LiteralPath $stageDir -Recurse -Force
        }
    }

    foreach ($name in 'CGO_ENABLED', 'GOOS', 'GOARCH', 'GOARM') {
        $value = $originalEnvironment[$name]
        if ($null -eq $value) {
            Remove-Item "Env:$name" -ErrorAction SilentlyContinue
        } else {
            Set-Item "Env:$name" $value
        }
    }
    Pop-Location
}
