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

Write-Host "构建 tengshou-cli-windows-$GoArch.exe ..."
go build -trimpath -ldflags $CliLdFlags -o "$Out/tengshou-cli-windows-$GoArch.exe" ./cmd/tengshou-cli

$Zip = "$Out/tengshou-windows-$GoArch.zip"
if (Test-Path $Zip) { Remove-Item $Zip }
# 与 CI 发布包保持一致：桌面应用 + MCP 连接器 + 接入说明，
# 三个文件都放在 zip 根目录（先汇总到暂存目录再压缩）。
$Stage = Join-Path $Out "package-windows-$GoArch"
Remove-Item -Recurse -Force $Stage -ErrorAction SilentlyContinue
New-Item -ItemType Directory -Force -Path $Stage | Out-Null
Copy-Item "$Out/tengshou-windows-$GoArch.exe" $Stage
Copy-Item "$Out/tengshou-cli-windows-$GoArch.exe" $Stage
# 用通配符而不是中文文件名：Windows PowerShell 5.1 会把无 BOM 脚本按 ANSI 读取，
# 中文路径字面量会失效。该目录只有接入说明一个 .txt。
Copy-Item "packaging/windows/*.txt" $Stage
Compress-Archive -Path (Join-Path $Stage "*") -DestinationPath $Zip
Remove-Item -Recurse -Force $Stage

Write-Host "完成：$Zip"
Write-Host "说明：v0.1 预发布版未签名；正式上架 Microsoft Store 前需补充打包与签名（B10）。"
