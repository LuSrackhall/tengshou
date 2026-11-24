# Release-form smoke for the Windows desktop exe (GUI subsystem).
#
# Drives `tengshou-windows-amd64.exe mcp` through a real stdin/stdout pipe the
# way an agent would, asserts initialize / tools/list, then starts the matching
# tengshou-cli serve and calls one write tool. This is the evidence that the
# -H windowsgui binary still speaks stdio MCP.
#
# Usage: scripts/smoke-mcp-desktop.ps1 -Desktop dist\tengshou-windows-amd64.exe `
#                                      -Cli dist\tengshou-cli-windows-amd64.exe
param(
    [Parameter(Mandatory = $true)][string]$Desktop,
    [Parameter(Mandatory = $true)][string]$Cli
)
$ErrorActionPreference = "Stop"

$Desktop = (Resolve-Path -LiteralPath $Desktop).Path
$Cli = (Resolve-Path -LiteralPath $Cli).Path

$Tmp = Join-Path ([System.IO.Path]::GetTempPath()) ("tengshou-mcp-smoke-" + [guid]::NewGuid().ToString("N"))
$Data = Join-Path $Tmp "data"
New-Item -ItemType Directory -Force -Path $Tmp | Out-Null

$Listener = [System.Net.Sockets.TcpListener]::new([System.Net.IPAddress]::Loopback, 0)
$Listener.Start()
$Port = $Listener.LocalEndpoint.Port
$Listener.Stop()

$Serve = $null

function Write-AsciiFile([string]$Path, [string]$Text) {
    [System.IO.File]::WriteAllText($Path, $Text, [System.Text.Encoding]::ASCII)
}

# Drives the GUI-subsystem exe through real OS stdin/stdout pipes (the same
# shape an MCP agent uses); no console window and no cmd quoting involved.
function Invoke-McpPipe([string]$RequestsPath, [string]$OutputPath, [string]$ErrorPath, [string[]]$ExtraArgs) {
    $psi = New-Object System.Diagnostics.ProcessStartInfo
    $psi.FileName = $Desktop
    $psi.Arguments = (@("mcp", "--data-dir", "`"$Data`"") + $ExtraArgs) -join " "
    $psi.RedirectStandardInput = $true
    $psi.RedirectStandardOutput = $true
    $psi.RedirectStandardError = $true
    $psi.UseShellExecute = $false
    $psi.CreateNoWindow = $true

    $proc = [System.Diagnostics.Process]::Start($psi)
    $proc.StandardInput.Write([System.IO.File]::ReadAllText($RequestsPath))
    $proc.StandardInput.Close()
    $stdout = $proc.StandardOutput.ReadToEnd()
    $stderr = $proc.StandardError.ReadToEnd()
    $proc.WaitForExit()
    [System.IO.File]::WriteAllText($OutputPath, $stdout, (New-Object System.Text.UTF8Encoding($false)))
    [System.IO.File]::WriteAllText($ErrorPath, $stderr, (New-Object System.Text.UTF8Encoding($false)))
    if ($proc.ExitCode -ne 0) {
        Write-Host $stderr
        throw "mcp exited with code $($proc.ExitCode)"
    }
}

function Read-Responses([string]$Path) {
    $lines = @(Get-Content -LiteralPath $Path -Encoding UTF8 | Where-Object { $_.Trim() -ne "" })
    return @($lines | ForEach-Object { $_ | ConvertFrom-Json })
}

try {
    Write-Host "1/2 desktop exe mcp: initialize / tools/list (local service not started)"
    $initRequests = Join-Path $Tmp "init.jsonl"
    Write-AsciiFile $initRequests @'
{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"release-smoke","version":"0"}}}
{"jsonrpc":"2.0","method":"notifications/initialized"}
{"jsonrpc":"2.0","id":2,"method":"tools/list"}
'@
    $initOut = Join-Path $Tmp "init.out"
    $initErr = Join-Path $Tmp "init.err"
    Invoke-McpPipe $initRequests $initOut $initErr @()
    $responses = Read-Responses $initOut
    if ($responses.Count -ne 2) {
        throw "expected 2 JSON-RPC responses, got $($responses.Count)"
    }
    if ($responses[0].result.serverInfo.name -ne "tengshou") {
        throw "unexpected initialize response: $($responses[0] | ConvertTo-Json -Compress)"
    }
    $toolNames = @($responses[1].result.tools | ForEach-Object { $_.name })
    foreach ($want in @("tengshou_status", "tengshou_create_field")) {
        if ($toolNames -notcontains $want) {
            throw "tools/list is missing $want"
        }
    }
    Write-Host "   initialize / tools/list passed ($($toolNames.Count) tools)"

    Write-Host "2/2 start tengshou-cli serve, then call a write tool through the desktop exe"
    $serveOut = Join-Path $Tmp "serve.out"
    $serveErr = Join-Path $Tmp "serve.err"
    $Serve = Start-Process -FilePath $Cli -PassThru -NoNewWindow `
        -ArgumentList @("serve", "--data-dir", "`"$Data`"", "--port", "$Port", "--loopback") `
        -RedirectStandardOutput $serveOut -RedirectStandardError $serveErr

    $ready = $false
    for ($i = 0; $i -lt 100; $i++) {
        if ($Serve.HasExited) { break }
        try {
            $health = Invoke-WebRequest -UseBasicParsing -TimeoutSec 1 -Uri "http://127.0.0.1:$Port/api/v1/health"
            if ($health.StatusCode -eq 200) { $ready = $true; break }
        } catch { }
        Start-Sleep -Milliseconds 200
    }
    if (-not $ready) {
        if (Test-Path -LiteralPath $serveErr) { Get-Content -LiteralPath $serveErr | Write-Host }
        throw "local service did not become ready"
    }

    $writeRequests = Join-Path $Tmp "write.jsonl"
    Write-AsciiFile $writeRequests @'
{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"release-smoke","version":"0"}}}
{"jsonrpc":"2.0","method":"notifications/initialized"}
{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"tengshou_create_field","arguments":{"entity":"contract","key":"smoke_field","label":"smoke field","type":"text"}}}
'@
    $writeOut = Join-Path $Tmp "write.out"
    $writeErr = Join-Path $Tmp "write.err"
    Invoke-McpPipe $writeRequests $writeOut $writeErr @("--api-url", "http://127.0.0.1:$Port")
    $responses = Read-Responses $writeOut
    if ($responses.Count -ne 2) {
        throw "expected 2 JSON-RPC responses, got $($responses.Count)"
    }
    $result = $responses[1].result
    if ($result.isError) {
        throw "write tool returned isError: $($responses[1] | ConvertTo-Json -Compress)"
    }
    $text = @($result.content)[0].text
    if ($text -notmatch "smoke_field") {
        throw "write tool response is missing smoke_field: $($responses[1] | ConvertTo-Json -Compress)"
    }
    Write-Host "   write tool tengshou_create_field passed"
    Write-Host "smoke passed: desktop exe mcp (stdio pipe) + tengshou-cli serve write"
} finally {
    if ($Serve -and -not $Serve.HasExited) {
        Stop-Process -Id $Serve.Id -Force -ErrorAction SilentlyContinue
    }
    Remove-Item -Recurse -Force -LiteralPath $Tmp -ErrorAction SilentlyContinue
}
