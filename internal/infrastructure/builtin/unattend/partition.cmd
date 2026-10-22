@echo off
setlocal EnableExtensions DisableDelayedExpansion
wpeutil UpdateBootInfo
if errorlevel 1 exit /b 1
set "firmware="
for /f "tokens=3" %%F in ('reg query HKLM\SYSTEM\CurrentControlSet\Control /v PEFirmwareType') do set "firmware=%%F"
if "%firmware%"=="0x2" goto uefi
if "%firmware%"=="0x1" goto bios
exit /b 65
:uefi
> X:\lober\partition.txt echo select disk %LOBER_PARAM_DISK_INDEX%
>> X:\lober\partition.txt echo clean
>> X:\lober\partition.txt echo convert gpt
>> X:\lober\partition.txt echo create partition efi size=260
>> X:\lober\partition.txt echo format quick fs=fat32 label=System
>> X:\lober\partition.txt echo create partition msr size=16
>> X:\lober\partition.txt echo create partition primary
>> X:\lober\partition.txt echo format quick fs=ntfs label=Windows
goto apply
:bios
> X:\lober\partition.txt echo select disk %LOBER_PARAM_DISK_INDEX%
>> X:\lober\partition.txt echo clean
>> X:\lober\partition.txt echo convert mbr
>> X:\lober\partition.txt echo create partition primary size=750
>> X:\lober\partition.txt echo format quick fs=ntfs label=Recovery
>> X:\lober\partition.txt echo set id=27
>> X:\lober\partition.txt echo create partition primary size=550
>> X:\lober\partition.txt echo format quick fs=ntfs label=System
>> X:\lober\partition.txt echo active
>> X:\lober\partition.txt echo create partition primary
>> X:\lober\partition.txt echo format quick fs=ntfs label=Windows
:apply
diskpart /s X:\lober\partition.txt
exit /b %errorlevel%
