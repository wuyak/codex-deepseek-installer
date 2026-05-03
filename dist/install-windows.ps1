$ErrorActionPreference = "Stop"

$ScriptDir = Split-Path -Parent $MyInvocation.MyCommand.Path
$Arch = [System.Runtime.InteropServices.RuntimeInformation]::ProcessArchitecture.ToString().ToLowerInvariant()

switch ($Arch) {
  "x64" { $ArchTag = "amd64" }
  "arm64" { $ArchTag = "arm64" }
  default {
    Write-Error "Unsupported Windows architecture: $Arch"
    exit 1
  }
}

$Candidates = @(
  (Join-Path $ScriptDir "codex-deepseek-installer.exe"),
  (Join-Path $ScriptDir "codex-deepseek-installer-windows-$ArchTag.exe"),
  (Join-Path $ScriptDir "dist/codex-deepseek-installer-windows-$ArchTag.exe")
)

$Bin = $null
foreach ($Candidate in $Candidates) {
  if (Test-Path -LiteralPath $Candidate) {
    $Bin = $Candidate
    break
  }
}

if (-not $Bin) {
  Write-Error ("Missing executable. Expected one of:`n  " + ($Candidates -join "`n  "))
  exit 1
}

Write-Host "Windows mode currently patches config/catalog only."
Write-Host "Codex App picker Statsig patch is skipped until the Windows Codex App Local Storage path is QA-verified."

& $Bin install --skip-statsig @args
exit $LASTEXITCODE
