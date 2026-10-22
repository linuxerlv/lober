package catalog

import (
	"regexp"
	"slices"
	"testing"
)

type planFamily struct {
	id      Family
	label   string
	formats []planFormat
}

type planFormat struct {
	id     Format
	label  string
	series []planSeries
}

type planSeries struct {
	id    Series
	label string
}

// planCatalog 抄录计划「任务 2」的取值表，按族、格式、系三级排列；格式的显示名以 spec §2.1
// 的表和 C1 为准，界面要与 spec、文档写法一致（T2-1）。
// 标识会进数据库和 URL，发布后不能改名，所以期望值写死在测试里，不从实现推出来。
var planCatalog = []planFamily{
	{"linux", "Linux", []planFormat{
		{"kickstart", "kickstart", []planSeries{
			{"rhel", "RHEL"},
			{"rocky", "Rocky Linux"},
			{"almalinux", "AlmaLinux"},
			{"openeuler", "openEuler"},
			{"opencloudos", "OpenCloudOS"},
		}},
		{"preseed", "preseed", []planSeries{{"debian", "Debian"}}},
		{"autoinstall", "autoinstall", []planSeries{{"ubuntu", "Ubuntu"}}},
		{"autoyast", "AutoYaST", []planSeries{{"sles15", "SLES 15"}, {"leap15", "openSUSE Leap 15"}}},
		{"agama", "Agama", []planSeries{{"sles16", "SLES 16"}, {"leap16", "openSUSE Leap 16"}}},
	}},
	{"windows", "Windows", []planFormat{
		{"unattend", "unattend", []planSeries{{"windows-server", "Windows Server"}, {"windows-desktop", "Windows 桌面版"}}},
	}},
	{"esxi", "ESXi", []planFormat{
		{"esxi-ks", "ESXi ks", []planSeries{{"esxi", "ESXi"}}},
	}},
}

// 公开接口里没有列出全部格式或全部系的函数，只靠 Families 与 SeriesOf 反推，
// 没有系的格式、挂在未知族下的系都会漏掉。所以「一个不多」和目录自洽直接遍历
// 实现里的表，断言仍走公开接口。
func formatTableIDs() []Format {
	var out []Format
	for _, r := range formatTable {
		out = append(out, r.id)
	}
	return out
}

func seriesTableIDs() []Series {
	var out []Series
	for _, r := range seriesTable {
		out = append(out, r.id)
	}
	return out
}

func Test取值表与计划一致(t *testing.T) {
	var families []Family
	var formats []Format
	var series []Series
	for _, pf := range planCatalog {
		families = append(families, pf.id)
		if f := pf.id; !f.Valid() || f.Label() != pf.label {
			t.Errorf("族 %q：Valid() = %v、Label() = %q，想要 true、%q", f, f.Valid(), f.Label(), pf.label)
		}
		var ofFamily []Series
		for _, pm := range pf.formats {
			formats = append(formats, pm.id)
			if f := pm.id; !f.Valid() || f.Label() != pm.label {
				t.Errorf("格式 %q：Valid() = %v、Label() = %q，想要 true、%q", f, f.Valid(), f.Label(), pm.label)
			}
			var ofFormat []Series
			for _, ps := range pm.series {
				ofFormat = append(ofFormat, ps.id)
				if s := ps.id; !s.Valid() || s.Label() != ps.label || s.Family() != pf.id || s.Format() != pm.id {
					t.Errorf("系 %q：Valid() = %v、Label() = %q、Family() = %q、Format() = %q，想要 true、%q、%q、%q",
						s, s.Valid(), s.Label(), s.Family(), s.Format(), ps.label, pf.id, pm.id)
				}
			}
			if got := SeriesOfFormat(pm.id); !slices.Equal(got, ofFormat) {
				t.Errorf("SeriesOfFormat(%q) = %q，想要 %q", pm.id, got, ofFormat)
			}
			ofFamily = append(ofFamily, ofFormat...)
		}
		if got := SeriesOf(pf.id); !slices.Equal(got, ofFamily) {
			t.Errorf("SeriesOf(%q) = %q，想要 %q", pf.id, got, ofFamily)
		}
		series = append(series, ofFamily...)
	}
	if got := Families(); !slices.Equal(got, families) {
		t.Errorf("Families() = %q，想要 %q", got, families)
	}

	if len(formats) != 7 || len(series) != 14 {
		t.Fatalf("抄录的取值表有 %d 种格式、%d 个系，计划是七种、十四个", len(formats), len(series))
	}
	gotFormats, wantFormats := slices.Sorted(slices.Values(formatTableIDs())), slices.Sorted(slices.Values(formats))
	if !slices.Equal(gotFormats, wantFormats) {
		t.Errorf("目录里的格式 = %q，想要恰好 %q", gotFormats, wantFormats)
	}
	gotSeries, wantSeries := slices.Sorted(slices.Values(seriesTableIDs())), slices.Sorted(slices.Values(series))
	if !slices.Equal(gotSeries, wantSeries) {
		t.Errorf("目录里的系 = %q，想要恰好 %q", gotSeries, wantSeries)
	}
}

func Test每个系的族与格式都合法(t *testing.T) {
	for _, s := range seriesTableIDs() {
		if !s.Family().Valid() || !s.Format().Valid() {
			t.Errorf("系 %q 的族 %q、格式 %q，想要都合法", s, s.Family(), s.Format())
		}
	}
}

func Test同一种格式的系都属于同一个族(t *testing.T) {
	// 模板勾了第一个系后，界面只列同格式的系（C13），而模板只属于一个族：
	// 格式跨了族，界面就会让人勾上别的族的系。
	for _, f := range formatTableIDs() {
		families := map[Family]bool{}
		for _, s := range SeriesOfFormat(f) {
			families[s.Family()] = true
		}
		if len(families) > 1 {
			t.Errorf("格式 %q 的系分属多个族：%v", f, families)
		}
	}
}

func Test每个族与每种格式都至少有一个系(t *testing.T) {
	// 建 ISO 先选族再选系（spec §5.1），没有系的族选了也建不出 ISO；
	// 没有系的格式谁也用不上。两种情况都只会是表写漏了。
	for _, f := range Families() {
		if len(SeriesOf(f)) == 0 {
			t.Errorf("族 %q 下没有系", f)
		}
	}
	for _, f := range formatTableIDs() {
		if len(SeriesOfFormat(f)) == 0 {
			t.Errorf("格式 %q 没有系在用", f)
		}
	}
}

func Test标识只用小写字母数字与连字符(t *testing.T) {
	// 标识原样进数据库和 URL（全局约定：标识一律小写 ASCII），中文和大写只能出现在显示名里。
	var ids []string
	for _, f := range Families() {
		ids = append(ids, string(f))
	}
	for _, f := range formatTableIDs() {
		ids = append(ids, string(f))
	}
	for _, s := range seriesTableIDs() {
		ids = append(ids, string(s))
	}
	ident := regexp.MustCompile(`^[a-z][a-z0-9-]*$`)
	for _, id := range ids {
		if !ident.MatchString(id) {
			t.Errorf("标识 %q 不匹配 %s", id, ident)
		}
	}
}

func Test未知或大小写不同的值无效且查询为空(t *testing.T) {
	// 标识不做规范化：大小写不同、带空白、写成显示名、拿别种标识充数，一律不认，
	// 免得同一个系在库里和 URL 里出现两种写法。
	t.Run("族", func(t *testing.T) {
		for _, f := range []Family{"", "Linux", "LINUX", " linux", "linux\n", "unix", "kickstart", "rocky"} {
			if f.Valid() || f.Label() != "" || len(SeriesOf(f)) != 0 {
				t.Errorf("族 %q：Valid() = %v、Label() = %q、SeriesOf() = %q，想要 false、空、空",
					f, f.Valid(), f.Label(), SeriesOf(f))
			}
		}
	})
	t.Run("格式", func(t *testing.T) {
		for _, f := range []Format{"", "Kickstart", "AUTOYAST", " preseed", "ESXi ks", "AutoYaST", "Agama", "esxi_ks", "esxi", "ks", "linux"} {
			if f.Valid() || f.Label() != "" || len(SeriesOfFormat(f)) != 0 {
				t.Errorf("格式 %q：Valid() = %v、Label() = %q、SeriesOfFormat() = %q，想要 false、空、空",
					f, f.Valid(), f.Label(), SeriesOfFormat(f))
			}
		}
	})
	t.Run("系", func(t *testing.T) {
		unknown := []Series{
			"", "Rocky", "ROCKY", " rocky", "rocky ", "RHEL", "Rocky Linux", "Windows-Server", "windows_server",
			"centos", "sles", "leap", "linux", "windows", "esxi-ks", "kickstart",
		}
		for _, s := range unknown {
			if s.Valid() || s.Label() != "" || s.Family() != "" || s.Format() != "" {
				t.Errorf("系 %q：Valid() = %v、Label() = %q、Family() = %q、Format() = %q，想要 false 与三个空串",
					s, s.Valid(), s.Label(), s.Family(), s.Format())
			}
		}
	})
}

func Test改动返回的切片不影响下一次查询(t *testing.T) {
	// 目录是内置的静态事实（C1）：调用方拿去排序、改写的切片，不能连带改掉目录。
	t.Run("Families", func(t *testing.T) {
		got := Families()
		for i := range got {
			got[i] = "unix"
		}
		if again, want := Families(), []Family{"linux", "windows", "esxi"}; !slices.Equal(again, want) {
			t.Errorf("改过上一次的结果后 Families() = %q，想要 %q", again, want)
		}
	})
	t.Run("SeriesOf", func(t *testing.T) {
		got := SeriesOf("windows")
		for i := range got {
			got[i] = "centos"
		}
		if again, want := SeriesOf("windows"), []Series{"windows-server", "windows-desktop"}; !slices.Equal(again, want) {
			t.Errorf("改过上一次的结果后 SeriesOf(windows) = %q，想要 %q", again, want)
		}
	})
	t.Run("SeriesOfFormat", func(t *testing.T) {
		got := SeriesOfFormat("autoyast")
		for i := range got {
			got[i] = "centos"
		}
		if again, want := SeriesOfFormat("autoyast"), []Series{"sles15", "leap15"}; !slices.Equal(again, want) {
			t.Errorf("改过上一次的结果后 SeriesOfFormat(autoyast) = %q，想要 %q", again, want)
		}
	})
}
