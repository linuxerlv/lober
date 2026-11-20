$ErrorActionPreference = 'Stop'
$features = $env:LOBER_PARAM_PACKAGES -split "`n"
$result = Install-WindowsFeature -Name $features -ErrorAction Stop
if (-not $result.Success) { throw 'Feature installation failed' }
# Reboot is deferred until the ordered firstboot chain has completed.
