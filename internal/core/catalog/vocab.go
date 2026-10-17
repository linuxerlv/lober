package catalog

// Persisted vocabulary identifiers retain their spelling across releases (C1).
const (
	FamilyLinux         Family      = "linux"
	FamilyWindows       Family      = "windows"
	FamilyESXi          Family      = "esxi"
	FormatKickstart     Format      = "kickstart"
	FormatPreseed       Format      = "preseed"
	FormatAutoinstall   Format      = "autoinstall"
	FormatAutoyast      Format      = "autoyast"
	FormatAgama         Format      = "agama"
	FormatUnattend      Format      = "unattend"
	FormatESXiKS        Format      = "esxi-ks"
	SeriesRHEL          Series      = "rhel"
	SeriesRocky         Series      = "rocky"
	SeriesAlmalinux     Series      = "almalinux"
	SeriesOpeneuler     Series      = "openeuler"
	SeriesOpencloudos   Series      = "opencloudos"
	SeriesDebian        Series      = "debian"
	SeriesUbuntu        Series      = "ubuntu"
	SeriesSLES15        Series      = "sles15"
	SeriesLeap15        Series      = "leap15"
	SeriesSLES16        Series      = "sles16"
	SeriesLeap16        Series      = "leap16"
	SeriesWinServer     Series      = "windows-server"
	SeriesWinDesktop    Series      = "windows-desktop"
	SeriesESXi          Series      = "esxi"
	TimingPre           Timing      = "pre"
	TimingPostInstaller Timing      = "post-installer"
	TimingPostTarget    Timing      = "post-target"
	TimingFirstboot     Timing      = "firstboot"
	InterpSH            Interpreter = "sh"
	InterpBash          Interpreter = "bash"
	InterpCmd           Interpreter = "cmd"
	InterpPowershell    Interpreter = "powershell"
	PartSyslog          SystemPart  = "syslog"
	PartFirstboot       SystemPart  = "firstboot"
	PartCleanup         SystemPart  = "cleanup"
	SysRepo             SystemValue = "repo"
	SysISO              SystemValue = "iso"
	SysSMB              SystemValue = "smb"
	SysGateway          SystemValue = "gateway"
	SysTask             SystemValue = "task"
	SysSN               SystemValue = "sn"
	SysLBrace           SystemValue = "lbrace"
	SysToken            SystemValue = "token"
	SysSyslogAddr       SystemValue = "syslog-addr"
	SysArtifacts        SystemValue = "artifacts"
	SysFinalizeToken    SystemValue = "finalize-token"
	ArchX8664           Arch        = "x86_64"
	ArchAarch64         Arch        = "aarch64"
	FirmwareBIOS        Firmware    = "bios"
	FirmwareUEFI        Firmware    = "uefi"
)

// AllFormats returns all formats in registry order, independently owned by caller.
func AllFormats() []Format {
	out := make([]Format, len(formatTable))
	for i, r := range formatTable {
		out[i] = r.id
	}
	return out
}
func AllSeries() []Series {
	out := make([]Series, len(seriesTable))
	for i, r := range seriesTable {
		out[i] = r.id
	}
	return out
}
func AllTimings() []Timing {
	out := make([]Timing, len(timingTable))
	for i, r := range timingTable {
		out[i] = r.id
	}
	return out
}
func AllInterpreters() []Interpreter {
	out := make([]Interpreter, len(interpreterTable))
	for i, r := range interpreterTable {
		out[i] = r.id
	}
	return out
}
func AllSystemParts() []SystemPart {
	out := make([]SystemPart, len(systemPartTable))
	for i, r := range systemPartTable {
		out[i] = r.id
	}
	return out
}
func AllSystemValues() []SystemValue {
	out := make([]SystemValue, len(systemValueTable))
	for i, r := range systemValueTable {
		out[i] = r.id
	}
	return out
}
func AllArchs() []Arch {
	out := make([]Arch, len(platformArchTable))
	for i, r := range platformArchTable {
		out[i] = r.id
	}
	return out
}
func AllFirmwares() []Firmware {
	out := make([]Firmware, len(platformFirmwareTable))
	for i, r := range platformFirmwareTable {
		out[i] = r.id
	}
	return out
}
