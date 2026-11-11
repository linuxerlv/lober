$ErrorActionPreference = 'Stop'
$capability = Get-WindowsCapability -Online -Name 'OpenSSH.Server*' | Select-Object -First 1
if (-not $capability) { throw 'OpenSSH Server capability unavailable; qualify the target image' }
if ($capability.State -ne 'Installed') { Add-WindowsCapability -Online -Name $capability.Name -ErrorAction Stop | Out-Null }
$account = Get-LocalUser -Name $env:LOBER_PARAM_USERNAME -ErrorAction Stop
$admins = Get-LocalGroup -SID 'S-1-5-32-544'
$member = Get-LocalGroupMember -Group $admins | Where-Object { $_.SID -eq $account.SID }
if (-not $member) { throw 'This builtin SSH hook requires the administrator account created by the account hook' }
$directory = Join-Path $env:ProgramData 'ssh'
New-Item -ItemType Directory -Force -Path $directory | Out-Null
$path = Join-Path $directory 'administrators_authorized_keys'
[IO.File]::AppendAllText($path, $env:LOBER_PARAM_KEY + "`n", (New-Object Text.UTF8Encoding($false)))
$acl = New-Object Security.AccessControl.FileSecurity
$acl.SetAccessRuleProtection($true, $false)
foreach ($sid in @('S-1-5-18', 'S-1-5-32-544')) {
  $identity = New-Object Security.Principal.SecurityIdentifier($sid)
  $rule = New-Object Security.AccessControl.FileSystemAccessRule($identity, 'FullControl', 'Allow')
  $acl.AddAccessRule($rule)
}
Set-Acl -LiteralPath $path -AclObject $acl
Set-Service -Name sshd -StartupType Automatic
Start-Service sshd
if (-not (Get-NetFirewallRule -Name 'OpenSSH-Server-In-TCP' -ErrorAction SilentlyContinue)) {
  New-NetFirewallRule -Name 'OpenSSH-Server-In-TCP' -DisplayName 'OpenSSH Server' -Enabled True -Direction Inbound -Protocol TCP -Action Allow -LocalPort 22 | Out-Null
}
