package catalog

import (
	"regexp"
	"slices"
	"testing"
)

// planFileKinds 是四类文件的标识，期望值写死在测试里，理由同 timing_test.go 的词表。
var planFileKinds = []struct {
	kind FileKind
	id   string
}{
	{FileMenu, "menu"},
	{FileAnswer, "answer"},
	{FileAux, "aux"},
	{FileScript, "script"},
}

// planDeliveryRow 是下发清单的一行。kinds 按机器取用的先后写（引导菜单、应答文件、附属文件、运行脚本），
// 也就是 DeliveryFiles 应有的输出；files 是经 /v1/answer 下发的文件名，没写的就是查不到。
type planDeliveryRow struct {
	format Format
	kinds  []FileKind
	files  map[FileKind]string
}

// planDelivery 抄录各格式的下发清单，依据逐行注明。旧 SPEC.md 指 7355e6a 的 SPEC.md，旧系统指 main 分支。
// 七种格式都有引导菜单、应答文件和运行脚本（渲染的四类输出，spec §4.3；每种格式都支持首次开机，正文里
// 就有执行插槽），差别只在附属文件。
var planDelivery = []planDeliveryRow{
	{
		// 旧 SPEC.md §4.5：inst.ks=…/ks.cfg。
		format: "kickstart",
		kinds:  []FileKind{FileMenu, FileAnswer, FileScript},
		files:  map[FileKind]string{FileAnswer: "ks.cfg"},
	},
	{
		// 旧 SPEC.md §4.5：preseed/url=…/preseed.cfg。
		format: "preseed",
		kinds:  []FileKind{FileMenu, FileAnswer, FileScript},
		files:  map[FileKind]string{FileAnswer: "preseed.cfg"},
	},
	{
		// 旧 SPEC.md §4.5：ds=nocloud-net;s=…/，这个目录下要有 user-data 与 meta-data；旧系统的网关
		// 为 meta-data 单独放行（gateway/answer.go:173）。
		format: "autoinstall",
		kinds:  []FileKind{FileMenu, FileAnswer, FileAux, FileScript},
		files:  map[FileKind]string{FileAnswer: "user-data", FileAux: "meta-data"},
	},
	{
		// 旧 SPEC.md §0.3：linux + initrd + autoyast=；文件名取旧系统的 autoinst.xml（model/render.go:111）。
		format: "autoyast",
		kinds:  []FileKind{FileMenu, FileAnswer, FileScript},
		files:  map[FileKind]string{FileAnswer: "autoinst.xml"},
	},
	{
		// 依据不足：旧 SPEC.md 与旧系统都没有 Agama，应答文件叫什么、要不要附属文件都没有出处。只登记
		// 推得出的三种，应答文件的文件名见 pendingDeliveryNames，由第 3 层任务 8 定。
		format: "agama",
		kinds:  []FileKind{FileMenu, FileAnswer, FileScript},
	},
	{
		// 旧 SPEC.md §4.5：unattend 由 WinPE 内的 startnet.cmd 去取，不烧进 WIM；应答文件名取旧系统的
		// unattend.xml（model/render.go:112）。启动脚本由 wimboot 注入（spec §8），这条路待真机核实（spec §2.2）。
		format: "unattend",
		kinds:  []FileKind{FileMenu, FileAnswer, FileAux, FileScript},
		files:  map[FileKind]string{FileAnswer: "unattend.xml", FileAux: "startnet.cmd"},
	},
	{
		// 旧 SPEC.md §4.5：mboot.efi -c …/boot.cfg；boot.cfg 的 ks= 再指向应答文件，旧系统叫它 ks.cfg
		// （gateway/answer.go:176、gateway/esxi_bootcfg.go:55）。
		format: "esxi-ks",
		kinds:  []FileKind{FileMenu, FileAnswer, FileAux, FileScript},
		files:  map[FileKind]string{FileAnswer: "ks.cfg", FileAux: "boot.cfg"},
	},
}

// pendingDeliveryNames 是依据不足、还没登记文件名的格子。定下来以后从这里删掉：「待定的格子确实还没
// 登记文件名」会先报红，提醒这一步。
var pendingDeliveryNames = []struct {
	format Format
	kind   FileKind
}{
	{"agama", FileAnswer},
}

func Test文件种类就是渲染的四类输出(t *testing.T) {
	// 四类输出见 spec §4.3。hook 脚本不在里面：它不经渲染，网关按快照里的摘要直接下发，与格式无关
	// （spec §6.2 第 5 条）。
	var planned []FileKind
	for _, p := range planFileKinds {
		planned = append(planned, p.kind)
		if string(p.kind) != p.id || !p.kind.Valid() {
			t.Errorf("文件种类 %q：Valid() = %v，想要标识 %q 且有效", p.kind, p.kind.Valid(), p.id)
		}
	}
	if !slices.Equal(fileKindTable, planned) {
		t.Errorf("目录里的文件种类 = %q，想要恰好 %q", fileKindTable, planned)
	}
	for _, k := range []FileKind{"", "Menu", "ANSWER", " aux", "script ", "hook", "ks.cfg", "boot", "应答文件"} {
		if k.Valid() {
			t.Errorf("文件种类 %q：Valid() = true，想要 false", k)
		}
	}
}

func Test下发清单逐格与计划一致(t *testing.T) {
	var planned []Format
	for _, p := range planDelivery {
		f := p.format
		planned = append(planned, f)
		if got := DeliveryFiles(f); !slices.Equal(got, p.kinds) {
			t.Errorf("DeliveryFiles(%s) = %q，想要 %q", f, got, p.kinds)
		}
		// 四类逐个问，计划里没写文件名的要查不到。
		for _, k := range fileKindTable {
			want, wantOK := p.files[k]
			if got, ok := DeliveryFileName(f, k); got != want || ok != wantOK {
				t.Errorf("DeliveryFileName(%s, %s) = %q, %v，想要 %q, %v", f, k, got, ok, want, wantOK)
			}
		}
	}
	if got, want := slices.Sorted(slices.Values(planned)), slices.Sorted(slices.Values(formatTableIDs())); !slices.Equal(got, want) {
		t.Errorf("抄录的下发清单覆盖的格式 = %q，目录里的格式 = %q，想要一致", got, want)
	}
}

func Test下发清单恰好覆盖目录里的格式且每格合法(t *testing.T) {
	// 「逐格与计划一致」只问抄录里的格式：清单漏了一种格式，查询不报错，只会对它一律返回空，网关就什么都
	// 不给；多登记了一个拼错的格式，那一行永远查不到。两边一起写错时只有这里报红，所以直接查实现里的表。
	var listed []Format
	for f, r := range deliveryTable {
		listed = append(listed, f)
		checkCell(t, "DeliveryFiles("+string(f)+")", r.kinds)
		for k, name := range r.names {
			// 只有应答文件与附属文件经 /v1/answer 按文件名取；引导菜单走 /v1/boot，运行脚本按插槽取。
			if k != FileAnswer && k != FileAux {
				t.Errorf("格式 %q 给 %q 登记了文件名 %q，只有 answer 与 aux 有文件名", f, k, name)
			}
			if !slices.Contains(r.kinds, k) {
				t.Errorf("格式 %q 给不下发的 %q 登记了文件名 %q", f, k, name)
			}
			if name == "" {
				t.Errorf("格式 %q 的 %q 登记了空的文件名：查得到却是空串，AnswerURL 会给出目录地址", f, k)
			}
		}
	}
	if got, want := slices.Sorted(slices.Values(listed)), slices.Sorted(slices.Values(formatTableIDs())); !slices.Equal(got, want) {
		t.Errorf("下发清单登记的格式 = %q，目录里的格式 = %q，想要恰好一致，不多不少", got, want)
	}
}

func Test每种格式都下发引导菜单与应答文件且有执行时机就下发运行脚本(t *testing.T) {
	// 引导菜单与应答文件是装机的起点，缺了哪个机器都走不下去。运行脚本跟着能力表走：格式支持执行时机，
	// 正文里就能写执行插槽，插槽那一行命令要取回运行脚本（spec §2.5）。
	for _, f := range formatTableIDs() {
		got := DeliveryFiles(f)
		for _, k := range []FileKind{FileMenu, FileAnswer} {
			if !slices.Contains(got, k) {
				t.Errorf("DeliveryFiles(%s) = %q，想要含 %s", f, got, k)
			}
		}
		if len(f.Timings()) > 0 && !slices.Contains(got, FileScript) {
			t.Errorf("%s 支持执行时机 %q，DeliveryFiles = %q 却不含 script", f, f.Timings(), got)
		}
	}
}

func Test应答文件与附属文件都登记了文件名(t *testing.T) {
	pending := func(f Format, k FileKind) bool {
		for _, p := range pendingDeliveryNames {
			if p.format == f && p.kind == k {
				return true
			}
		}
		return false
	}
	t.Run("除待定的格子外都有", func(t *testing.T) {
		// 网关按文件名查它在不在下发清单里（spec §5.4 第 3 步），渲染按同一个名字写地址：下发了却没登记
		// 文件名，两边就只能各写一份。
		for _, f := range formatTableIDs() {
			for _, k := range DeliveryFiles(f) {
				if (k != FileAnswer && k != FileAux) || pending(f, k) {
					continue
				}
				if _, ok := DeliveryFileName(f, k); !ok {
					t.Errorf("%s 下发 %s，DeliveryFileName 却查不到文件名", f, k)
				}
			}
		}
	})
	t.Run("待定的格子确实还没登记文件名", func(t *testing.T) {
		for _, p := range pendingDeliveryNames {
			if !slices.Contains(DeliveryFiles(p.format), p.kind) {
				t.Errorf("待定的格子 %s/%s：DeliveryFiles(%s) = %q 不含 %s，待定的应是下发了却没定名字的格子",
					p.format, p.kind, p.format, DeliveryFiles(p.format), p.kind)
			}
			if name, ok := DeliveryFileName(p.format, p.kind); ok {
				t.Errorf("待定的格子 %s/%s 已经登记了 %q：定下来了就从 pendingDeliveryNames 删掉，并在抄录里写上依据",
					p.format, p.kind, name)
			}
		}
	})
}

func Test下发的文件名放进地址不用转义且同一格式里不重名(t *testing.T) {
	// 文件名原样出现在 /v1/answer 的路径里，网关按它分辨机器要的是哪个文件：一种格式里两个种类同名，
	// 网关就分不出来；名字要转义的话，菜单里写的和网关解出来的就不是同一串字符。
	name := regexp.MustCompile(`^[a-z][a-z0-9.-]*$`)
	for _, f := range formatTableIDs() {
		seen := map[string]FileKind{}
		for _, k := range fileKindTable {
			n, ok := DeliveryFileName(f, k)
			if !ok {
				continue
			}
			if !name.MatchString(n) {
				t.Errorf("%s 的 %s 文件名 %q 不匹配 %s", f, k, n, name)
			}
			if other, dup := seen[n]; dup {
				t.Errorf("%s 的 %s 与 %s 同名 %q", f, other, k, n)
			}
			seen[n] = k
		}
	}
}

func Test未知格式与不下发的种类查询为空(t *testing.T) {
	t.Run("未知格式", func(t *testing.T) {
		// 显示名、族、系的标识都不是格式。
		for _, f := range []Format{"", "Kickstart", " preseed", "ESXi ks", "esxi", "rocky", "suse"} {
			if got := DeliveryFiles(f); len(got) != 0 {
				t.Errorf("DeliveryFiles(%q) = %q，想要空", f, got)
			}
			for _, k := range fileKindTable {
				if name, ok := DeliveryFileName(f, k); ok || name != "" {
					t.Errorf("DeliveryFileName(%q, %s) = %q, %v，想要空、false", f, k, name, ok)
				}
			}
		}
	})
	t.Run("没有文件名的种类与不下发的种类", func(t *testing.T) {
		cases := []struct {
			f Format
			k FileKind
		}{
			{"kickstart", FileMenu},
			{"kickstart", FileScript},
			{"kickstart", FileAux},
			{"preseed", FileAux},
			{"esxi-ks", FileScript},
		}
		for _, c := range cases {
			if name, ok := DeliveryFileName(c.f, c.k); ok || name != "" {
				t.Errorf("DeliveryFileName(%s, %s) = %q, %v，想要空、false", c.f, c.k, name, ok)
			}
		}
	})
	t.Run("未知种类", func(t *testing.T) {
		for _, f := range formatTableIDs() {
			for _, k := range []FileKind{"", "Answer", "hook", "ks.cfg"} {
				if name, ok := DeliveryFileName(f, k); ok || name != "" {
					t.Errorf("DeliveryFileName(%s, %q) = %q, %v，想要空、false", f, k, name, ok)
				}
			}
		}
	})
}

func Test改动DeliveryFiles返回的切片不影响下一次查询(t *testing.T) {
	// 下发清单是内置的静态事实（C1），理由同「改动能力查询返回的切片不影响下一次查询」。
	got := DeliveryFiles("autoinstall")
	for i := range got {
		got[i] = FileAux
	}
	want := []FileKind{FileMenu, FileAnswer, FileAux, FileScript}
	if again := DeliveryFiles("autoinstall"); !slices.Equal(again, want) {
		t.Errorf("改过上一次的结果后 DeliveryFiles(autoinstall) = %q，想要 %q", again, want)
	}
}
