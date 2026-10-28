# End-to-end check for the realtime service.
#
#   1. log in to the API and obtain an access token
#   2. exchange it for a single-use WebSocket ticket
#   3. connect, subscribe, publish and ping over the socket
#
# Requires: API on 55555, realtime service on 8090, and matching WS_TICKET_SECRET.
[CmdletBinding()]
param(
    [string]$ApiBase = 'http://127.0.0.1:55555',
    [string]$WsUrl   = 'ws://127.0.0.1:8090/ws',
    [string]$Origin  = 'http://localhost:5173',
    [string]$Email   = 'ws-smoke@example.com',
    [string]$Password = 'password123'
)

$ErrorActionPreference = 'Stop'

function Write-Step([string]$message) { Write-Host "== $message" -ForegroundColor Cyan }

function Receive-Json($socket, [int]$timeoutMs = 5000) {
    $buffer = [byte[]]::new(8192)
    $segment = [ArraySegment[byte]]::new($buffer)
    $cts = [System.Threading.CancellationTokenSource]::new($timeoutMs)
    try {
        $result = $socket.ReceiveAsync($segment, $cts.Token).GetAwaiter().GetResult()
    } finally {
        $cts.Dispose()
    }
    if ($result.MessageType -eq [System.Net.WebSockets.WebSocketMessageType]::Close) {
        throw "server closed the connection with status $($socket.CloseStatus)"
    }
    $text = [System.Text.Encoding]::UTF8.GetString($buffer, 0, $result.Count)
    return $text | ConvertFrom-Json
}

function Send-Json($socket, [string]$json) {
    $bytes = [System.Text.Encoding]::UTF8.GetBytes($json)
    $segment = [ArraySegment[byte]]::new($bytes)
    [void]$socket.SendAsync($segment, [System.Net.WebSockets.WebSocketMessageType]::Text, $true, [System.Threading.CancellationToken]::None).GetAwaiter().GetResult()
}

# --- 1. credentials ---------------------------------------------------------
Write-Step "prepare an account on $ApiBase"
$credentials = @{ email = $Email; password = $Password } | ConvertTo-Json
try {
    Invoke-RestMethod -Method Post -Uri "$ApiBase/api/v1/auth/register" -ContentType 'application/json' -Body $credentials | Out-Null
    Write-Host "registered $Email"
} catch {
    Write-Host "account already exists, logging in"
}

$auth = Invoke-RestMethod -Method Post -Uri "$ApiBase/api/v1/auth/login" -ContentType 'application/json' -Body $credentials
if (-not $auth.token) { throw 'login did not return a token' }
Write-Host "access token acquired (length $($auth.token.Length))"

# --- 2. ticket --------------------------------------------------------------
Write-Step "exchange the access token for a WebSocket ticket"
$headers = @{ Authorization = "Bearer $($auth.token)" }
$ticket = Invoke-RestMethod -Method Post -Uri "$ApiBase/api/v1/ws/ticket" -Headers $headers
Write-Host "ticket acquired, expires in $($ticket.expires_in)s"

# --- 3. connection ----------------------------------------------------------
Write-Step "connect to $WsUrl"
$socket = [System.Net.WebSockets.ClientWebSocket]::new()
$socket.Options.SetRequestHeader('Origin', $Origin)
$uri = [Uri]("$WsUrl" + '?ticket=' + [Uri]::EscapeDataString($ticket.ticket))
$cts = [System.Threading.CancellationTokenSource]::new(10000)
try {
    [void]$socket.ConnectAsync($uri, $cts.Token).GetAwaiter().GetResult()
} finally {
    $cts.Dispose()
}
Write-Host "connected"

try {
    Write-Step 'subscribe to topic "demo"'
    Send-Json $socket '{"type":"subscribe","topic":"demo"}'
    $subscribed = Receive-Json $socket
    if ($subscribed.type -ne 'subscribed') { throw "expected subscribed, got $($subscribed.type)" }

    Write-Step 'publish to topic "demo"'
    Send-Json $socket '{"type":"publish","topic":"demo","data":{"hello":"world"}}'
    $message = Receive-Json $socket
    if ($message.type -ne 'message') { throw "expected message, got $($message.type)" }
    Write-Host "received payload: $($message.data | ConvertTo-Json -Compress)"

    Write-Step 'ping'
    Send-Json $socket '{"type":"ping"}'
    $pong = Receive-Json $socket
    if ($pong.type -ne 'pong') { throw "expected pong, got $($pong.type)" }

    Write-Host ''
    Write-Host 'RESULT: PASS - ticket auth, subscribe, fan-out and heartbeat all work.' -ForegroundColor Green
} finally {
    if ($socket.State -eq [System.Net.WebSockets.WebSocketState]::Open) {
        [void]$socket.CloseAsync([System.Net.WebSockets.WebSocketCloseStatus]::NormalClosure, 'done', [System.Threading.CancellationToken]::None).GetAwaiter().GetResult()
    }
    $socket.Dispose()
}
