package catalog

import "slices"

// FileKind 是网关给机器下发、由渲染产出的文件种类，就是渲染的四类输出（spec §4.3）。hook 脚本不在其中：
// 它不经渲染，网关按快照里的摘要从仓储取出直接下发，与格式无关（spec §6.2 第 5 条）。
type FileKind string

// 四类文件的标识，和 series.go 的表一样发布后不能改名。
const (
	FileMenu   FileKind = "menu"   // 引导菜单：iPXE 菜单项，经 /v1/boot 下发
	FileAnswer FileKind = "answer" // 应答文件：该格式的主应答文件
	FileAux    FileKind = "aux"    // 附属文件：格式要的其余文件，如 Ubuntu 的 meta-data、ESXi 的 boot.cfg
	FileScript FileKind = "script" // 运行脚本：某个执行插槽展开出的脚本，装机时由插槽那一行命令取回
)

// fileKindTable 是全部文件种类，Valid 只认它；它的顺序就是 DeliveryFiles 对外的顺序。
var fileKindTable = []FileKind{FileMenu, FileAnswer, FileAux, FileScript}

// deliveryRow 是一种格式的下发清单。
type deliveryRow struct {
	kinds []FileKind          // 要下发的种类；对外的顺序由 fileKindTable 定，这里写成什么顺序都不影响
	names map[FileKind]string // 经 /v1/answer 下发的文件名：只有应答文件与附属文件有
}

// deliveryTable 是每种格式要下发的文件（spec §2.1「格式决定四件事」之一）。网关按它查机器要的文件在不在
// 清单里（spec §5.4 第 3 步），渲染按同一个文件名写地址，文件名因此只有这一份。引导菜单走 /v1/boot，
// 运行脚本按插槽取，都没有固定的文件名。用格式做 map 的键，理由同 capabilityTable。
//
// 七种格式都下发引导菜单、应答文件和运行脚本：每种格式都支持首次开机时机，正文里就有执行插槽。差别只在
// 附属文件。依据逐行注明：旧 SPEC.md 指 7355e6a 的 SPEC.md，旧系统指 9c7c374（当时的 main，以后会被新系统
// 取代），路径与行号都按它，用 git show 9c7c374:<路径> 回查。
var deliveryTable = map[Format]deliveryRow{
	// 旧 SPEC.md §4.5：inst.ks=…/ks.cfg。
	"kickstart": {
		kinds: []FileKind{FileMenu, FileAnswer, FileScript},
		names: map[FileKind]string{FileAnswer: "ks.cfg"},
	},
	// 旧 SPEC.md §4.5：preseed/url=…/preseed.cfg。
	"preseed": {
		kinds: []FileKind{FileMenu, FileAnswer, FileScript},
		names: map[FileKind]string{FileAnswer: "preseed.cfg"},
	},
	// 旧 SPEC.md §4.5：ds=nocloud-net;s=…/，cloud-init 在这个目录下取 user-data 与 meta-data；旧系统的网关为
	// meta-data 单独放行（internal/gateway/answer.go:173）。
	"autoinstall": {
		kinds: []FileKind{FileMenu, FileAnswer, FileAux, FileScript},
		names: map[FileKind]string{FileAnswer: "user-data", FileAux: "meta-data"},
	},
	// 旧 SPEC.md §0.3：linux + initrd + autoyast=；文件名取旧系统的 autoinst.xml
	// （internal/core/model/render.go:111）。
	"autoyast": {
		kinds: []FileKind{FileMenu, FileAnswer, FileScript},
		names: map[FileKind]string{FileAnswer: "autoinst.xml"},
	},
	// Agama primary docs: https://agama-project.github.io/docs/overview/unattended
	// inst.auto fetches/evaluates one JSON/Jsonnet profile. Lober chooses the documented
	// autoinst.jsonnet convention; no companion file is required by this protocol.
	"agama": {
		kinds: []FileKind{FileMenu, FileAnswer, FileScript},
		names: map[FileKind]string{FileAnswer: "autoinst.jsonnet"},
	},
	// 旧 SPEC.md §4.5：unattend 由 WinPE 内的 startnet.cmd 去取，不烧进 WIM；文件名取旧系统的 unattend.xml
	// （internal/core/model/render.go:112）。启动脚本由 wimboot 注入（spec §8），这条路待真机核实（spec §2.2）。
	"unattend": {
		kinds: []FileKind{FileMenu, FileAnswer, FileAux, FileScript},
		names: map[FileKind]string{FileAnswer: "unattend.xml", FileAux: "startnet.cmd"},
	},
	// 旧 SPEC.md §4.5：mboot.efi -c …/boot.cfg；boot.cfg 的 ks= 再指向应答文件，旧系统叫它 ks.cfg
	// （internal/gateway/answer.go:176、internal/gateway/esxi_bootcfg.go:55）。
	"esxi-ks": {
		kinds: []FileKind{FileMenu, FileAnswer, FileAux, FileScript},
		names: map[FileKind]string{FileAnswer: "ks.cfg", FileAux: "boot.cfg"},
	},
}

// Valid 报告 k 是不是四类文件之一，规则同 Family.Valid。
func (k FileKind) Valid() bool {
	return slices.Contains(fileKindTable, k)
}

// DeliveryFiles 是格式 f 要下发给机器的文件种类，按 FileKind 的固定次序（菜单、应答文件、附属文件、运行
// 脚本），不是机器取用的先后：ESXi 与 Windows 都先取附属文件，再由它去取应答文件。顺序照 fileKindTable
// 遍历得出，与 Format.Timings 同理，不靠 deliveryTable 的书写顺序。未知的格式返回空。每次都返回新切片，
// 调用方改了不影响目录。
func DeliveryFiles(f Format) []FileKind {
	kinds := deliveryTable[f].kinds
	var out []FileKind
	for _, k := range fileKindTable {
		if slices.Contains(kinds, k) {
			out = append(out, k)
		}
	}
	return out
}

// DeliveryFileName 是格式 f 里种类 k 的文件在 /v1/answer 下的文件名，只有应答文件与附属文件有。引导菜单、
// 运行脚本、f 不下发的种类、以及未知的格式或种类都返回 false。
// 不拿空串表示没有：AnswerURL 收到空的文件名给出的是目录地址，调用方漏判就会悄悄拿错地址。
func DeliveryFileName(f Format, k FileKind) (string, bool) {
	name, ok := deliveryTable[f].names[k]
	return name, ok
}
