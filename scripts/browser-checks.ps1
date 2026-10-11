param(
  [Parameter(Mandatory=$true)][string]$BaseURL,
  [string]$Node = 'node'
)
$ErrorActionPreference = 'Stop'
# Use only an isolated test server: this creates and deletes synthetic collections.
$uri = [Uri]$BaseURL
if (!$uri.IsAbsoluteUri -or $uri.Scheme -notin @('http','https')) { throw 'Provide an absolute FluxPort URL.' }
& $Node "$PSScriptRoot/browser-checks.cjs" "$($BaseURL.TrimEnd('/'))/collections"
if ($LASTEXITCODE -ne 0) { throw 'Browser checks failed. Install Playwright or configure NODE_PATH to an existing installation.' }
