param([string]$Binary = "$PSScriptRoot/../dist/fluxport.exe", [string]$Report = "$PSScriptRoot/../artifacts/performance.json")
$ErrorActionPreference = 'Stop'
$binaryPath = (Resolve-Path -LiteralPath $Binary).Path
$reportPath = [IO.Path]::GetFullPath($Report)
[IO.Directory]::CreateDirectory([IO.Path]::GetDirectoryName($reportPath)) | Out-Null
$measurementDir = Join-Path ([IO.Path]::GetTempPath()) ('fluxport-measure-' + [Guid]::NewGuid().ToString('N'))
[IO.Directory]::CreateDirectory($measurementDir) | Out-Null
$keys = @('FLUXPORT_ADDRESS','FLUXPORT_DATA_DIR','FLUXPORT_STATE_DIR','FLUXPORT_ADMIN_PASSWORD','FLUXPORT_API_KEY','FLUXPORT_CONFIG','FLUXPORT_MAX_FILE_BYTES','FLUXPORT_PUBLIC_DEMO')
$saved = @{}
foreach ($key in $keys) { $saved[$key] = [Environment]::GetEnvironmentVariable($key, 'Process') }
$runs = @()
$assets = @()
$http = [Net.Http.HttpClient]::new()
$http.Timeout = [TimeSpan]::FromSeconds(2)
try {
  for ($iteration = 1; $iteration -le 5; $iteration++) {
    $listener = [Net.Sockets.TcpListener]::new([Net.IPAddress]::Loopback,0)
    $listener.Start(); $port = $listener.LocalEndpoint.Port; $listener.Stop()
    $env:FLUXPORT_ADDRESS = "127.0.0.1:$port"
    $env:FLUXPORT_DATA_DIR = Join-Path $measurementDir "data-$iteration"
    $env:FLUXPORT_STATE_DIR = Join-Path $measurementDir "state-$iteration"
    $env:FLUXPORT_ADMIN_PASSWORD = ''; $env:FLUXPORT_API_KEY = ''; $env:FLUXPORT_CONFIG = ''; $env:FLUXPORT_MAX_FILE_BYTES = '16777216'
    $env:FLUXPORT_PUBLIC_DEMO = 'false'
    $timer = [Diagnostics.Stopwatch]::StartNew()
    $startInfo = [Diagnostics.ProcessStartInfo]::new($binaryPath,'serve')
    $startInfo.UseShellExecute = $false
    $startInfo.CreateNoWindow = $true
    $startInfo.RedirectStandardOutput = $true
    $startInfo.RedirectStandardError = $true
    $process = [Diagnostics.Process]::new()
    $process.StartInfo = $startInfo
    $process.Start() | Out-Null
    try {
      $ready = $false
      while ($timer.Elapsed.TotalSeconds -lt 10) {
        if ($process.HasExited) { throw 'FluxPort exited during startup.' }
        $probe = [Net.Sockets.TcpClient]::new()
        try {
          $probe.ReceiveTimeout = 2000; $probe.SendTimeout = 2000
          $connection = $probe.ConnectAsync('127.0.0.1',$port)
          if (!$connection.Wait(10)) { throw 'Probe connection timeout.' }
          $stream = $probe.GetStream()
          $requestBytes = [Text.Encoding]::ASCII.GetBytes("GET /healthz HTTP/1.1`r`nHost: 127.0.0.1:$port`r`nConnection: close`r`n`r`n")
          $stream.Write($requestBytes,0,$requestBytes.Length)
          $buffer = [byte[]]::new(256)
          $count = $stream.Read($buffer,0,$buffer.Length)
          $ready = [Text.Encoding]::ASCII.GetString($buffer,0,$count).StartsWith('HTTP/1.1 200')
        } catch {} finally { $probe.Dispose() }
        if ($ready) { break }; Start-Sleep -Milliseconds 10
      }
      if (!$ready) { throw 'Health check timeout.' }
      $startup = $timer.Elapsed.TotalMilliseconds
      Start-Sleep -Seconds 1
      $process.Refresh()
      $runs += [ordered]@{startup_ms = [math]::Round($startup,2); idle_working_set_bytes = $process.WorkingSet64; idle_private_bytes = $process.PrivateMemorySize64}
      if ($iteration -eq 5) {
        foreach ($path in @('/','/static/css/tokens.css','/static/css/components.css','/static/css/workspace.css','/static/js/htmx.min.js','/static/js/app.js','/static/js/workspace.js','/static/js/workbench.js','/static/js/pipelines.js','/static/icons/icons.svg','/static/icons/logo.svg')) {
          $request = [Net.Http.HttpRequestMessage]::new([Net.Http.HttpMethod]::Get,"http://127.0.0.1:$port$path")
          $request.Headers.TryAddWithoutValidation('Accept-Encoding','gzip') | Out-Null
          $response = $http.SendAsync($request).GetAwaiter().GetResult()
          $bytes = $response.Content.ReadAsByteArrayAsync().GetAwaiter().GetResult()
          $assets += [ordered]@{path=$path; transfer_bytes=$bytes.Length; encoding=($response.Content.Headers.ContentEncoding -join ',')}
          $response.Dispose(); $request.Dispose()
        }
      }
    } finally {
      if (!$process.HasExited) { $process.Kill(); $process.WaitForExit() }
      [IO.File]::WriteAllText((Join-Path $measurementDir "stdout-$iteration.log"),$process.StandardOutput.ReadToEnd())
      [IO.File]::WriteAllText((Join-Path $measurementDir "stderr-$iteration.log"),$process.StandardError.ReadToEnd())
      $process.Dispose()
    }
  }
  $result = [ordered]@{measured_at = [DateTime]::UtcNow.ToString('o'); platform = [Runtime.InteropServices.RuntimeInformation]::OSDescription; architecture = [Runtime.InteropServices.RuntimeInformation]::OSArchitecture.ToString(); binary_bytes = (Get-Item -LiteralPath $binaryPath).Length; dataset='Empty data folder, fresh SQLite database each run'; startup_method='Process launch through first successful HTTP health check; 10 ms retry interval'; idle_method='Working set and private bytes at one second after health check'; runs=$runs; frontend_gzip=$assets; log_directory=$measurementDir}
  $json = $result | ConvertTo-Json -Depth 6
  [IO.File]::WriteAllText($reportPath,$json)
  $json
} finally { $http.Dispose(); foreach ($key in $keys) { [Environment]::SetEnvironmentVariable($key,$saved[$key],'Process') } }
