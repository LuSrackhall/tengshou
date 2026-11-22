# Build the Windows desktop executable and CLI, then package a zip.
# Wails v3 on Windows is built with the pure-Go WebView2 bridge, so no mingw
# toolchain is required (CGO_ENABLED=0).
$ErrorActionPreference = "Stop"

$Root = Split-Path -Parent $PSScriptRoot
Set-Location $Root

$Version = if ($env:VERSION) { $env:VERSION } else {
    try { (git describe --tags --always --dirty) } catch { "0.1.0-dev" }
}
$Commit = if ($env:COMMIT) { $env:COMMIT } else {
    try { (git rev-parse --short HEAD) } catch { "unknown" }
}
$BuildDate = (Get-Date).ToUniversalTime().ToString("yyyy-MM-ddTHH:mm:ssZ")
$Out = if ($env:OUT) { $env:OUT } else { "dist" }

$LdFlags = "-s -w -H windowsgui " +
    "-X github.com/LuSrackhall/tengshou/internal/version.Version=$Version " +
    "-X github.com/LuSrackhall/tengshou/internal/version.Commit=$Commit " +
    "-X github.com/LuSrackhall/tengshou/internal/version.BuildDate=$BuildDate"

New-Item -ItemType Directory -Force -Path $Out | Out-Null

$env:CGO_ENABLED = "0"
$env:GOOS = "windows"
$GoArch = if ($env:GOARCH) { $env:GOARCH } else { "amd64" }
$env:GOARCH = $GoArch

Write-Host "构建 tengshou.exe ..."
go build -trimpath -ldflags $LdFlags -o "$Out/tengshou.exe" ./cmd/tengshou

Write-Host "构建 tengshou-cli.exe ..."
go build -trimpath -ldflags $LdFlags -o "$Out/tengshou-cli.exe" ./cmd/tengshou-cli

$Zip = "$Out/tengshou-windows-$GoArch.zip"
if (Test-Path $Zip) { Remove-Item $Zip }
Compress-Archive -Path "$Out/tengshou.exe", "$Out/tengshou-cli.exe", "README.md" -DestinationPath $Zip

Write-Host "完成：$Zip"
Write-Host "说明：v0.1 预发布版未签名；正式上架 Microsoft Store 前需补充打包与签名（B10）。"
