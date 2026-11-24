# Build the Windows desktop executable and CLI from one place.
# Dev and CI both call this script; the release zip is assembled afterwards by
# scripts/package-windows-zip.sh (which only packs the desktop exe + notes).
#
# Wails v3 on Windows is built with the pure-Go WebView2 bridge, so no mingw
# toolchain is required (CGO_ENABLED=0).
#
# Env: VERSION / COMMIT / BUILD_DATE / OUT / GOARCH
$ErrorActionPreference = "Stop"

$Root = Split-Path -Parent $PSScriptRoot
Set-Location $Root

$Version = if ($env:VERSION) { $env:VERSION } else {
    try { (git describe --tags --always --dirty) } catch { "0.1.0-dev" }
}
$Commit = if ($env:COMMIT) { $env:COMMIT } else {
    try { (git rev-parse --short HEAD) } catch { "unknown" }
}
$BuildDate = if ($env:BUILD_DATE) { $env:BUILD_DATE } else {
    (Get-Date).ToUniversalTime().ToString("yyyy-MM-ddTHH:mm:ssZ")
}
$Out = if ($env:OUT) { $env:OUT } else { "dist" }

$VersionLdFlags = "-X github.com/LuSrackhall/tengshou/internal/version.Version=$Version " +
    "-X github.com/LuSrackhall/tengshou/internal/version.Commit=$Commit " +
    "-X github.com/LuSrackhall/tengshou/internal/version.BuildDate=$BuildDate"

# 桌面壳用 Windows GUI 子系统；CLI 必须保留控制台子系统，否则
# `tengshou-cli-windows-amd64.exe version` 在 cmd/PowerShell 里看不到输出。
$LdFlags = "-s -w -H windowsgui " + $VersionLdFlags
$CliLdFlags = "-s -w " + $VersionLdFlags

New-Item -ItemType Directory -Force -Path $Out | Out-Null

$env:CGO_ENABLED = "0"
$env:GOOS = "windows"
$GoArch = if ($env:GOARCH) { $env:GOARCH } else { "amd64" }
$env:GOARCH = $GoArch

Write-Host "构建 tengshou-windows-$GoArch.exe ..."
go build -trimpath -ldflags $LdFlags -o "$Out/tengshou-windows-$GoArch.exe" ./cmd/tengshou
# $ErrorActionPreference does not cover native exit codes, so check both the
# exit code and the artifact. Keep this file UTF-8 with BOM: Windows PowerShell
# 5.1 reads BOM-less files as ANSI, and the Chinese comments below then corrupt
# the parse so the script silently builds nothing and still exits 0.
if ($LASTEXITCODE -ne 0 -or -not (Test-Path "$Out/tengshou-windows-$GoArch.exe")) {
    throw "构建桌面 exe 失败（exit $LASTEXITCODE）：$Out/tengshou-windows-$GoArch.exe 不存在"
}

Write-Host "构建 tengshou-cli-windows-$GoArch.exe ..."
go build -trimpath -ldflags $CliLdFlags -o "$Out/tengshou-cli-windows-$GoArch.exe" ./cmd/tengshou-cli
if ($LASTEXITCODE -ne 0 -or -not (Test-Path "$Out/tengshou-cli-windows-$GoArch.exe")) {
    throw "构建 CLI exe 失败（exit $LASTEXITCODE）：$Out/tengshou-cli-windows-$GoArch.exe 不存在"
}

Write-Host "完成：$Out/tengshou-windows-$GoArch.exe、$Out/tengshou-cli-windows-$GoArch.exe"
Write-Host "打发布 zip：bash scripts/package-windows-zip.sh $Out（需 python3）"
Write-Host "说明：v0.1 预发布版未签名；正式上架 Microsoft Store 前需补充打包与签名（B10）。"
