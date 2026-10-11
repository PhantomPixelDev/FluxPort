param(
  [Parameter(Mandatory=$true)][string]$BaseURL,
  [string]$PlaywrightCLI = 'playwright-cli'
)
$ErrorActionPreference = 'Stop'
# Use only an isolated test server: this creates and deletes synthetic collections.
$uri = [Uri]$BaseURL
if (!$uri.IsAbsoluteUri -or $uri.Scheme -notin @('http','https')) { throw 'Provide an absolute FluxPort URL.' }
$callback = ((Get-Content -LiteralPath "$PSScriptRoot/browser-checks.js") | Where-Object { !$_.TrimStart().StartsWith('//') }) -join ' '
try {
  & $PlaywrightCLI -s=fluxport-checks open "$($BaseURL.TrimEnd('/'))/collections"
  if ($LASTEXITCODE -ne 0) { throw 'Unable to open the browser.' }
  & $PlaywrightCLI -s=fluxport-checks run-code $callback
  if ($LASTEXITCODE -ne 0) { throw 'Browser checks failed.' }
} finally {
  & $PlaywrightCLI -s=fluxport-checks close
}
