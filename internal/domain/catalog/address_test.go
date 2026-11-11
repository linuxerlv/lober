package catalog

import (
	"net/url"
	"slices"
	"strings"
	"testing"
)

func TestAnswerURL按机器面地址格式拼出(t *testing.T) {
	// 旧 SPEC.md §8.1：GET /v1/answer/{task}/{token}/{file}，token 放在路径段里（旧 SPEC.md §4.4）。
	cases := []struct {
		name                    string
		base, task, token, file string
		want                    string
	}{
		{"应答文件", "http://gw", "t-1", "tok", "ks.cfg", "http://gw/v1/answer/t-1/tok/ks.cfg"},
		{"基地址带端口与路径前缀", "http://10.0.0.1:8080/lober", "t-1", "tok", "preseed.cfg",
			"http://10.0.0.1:8080/lober/v1/answer/t-1/tok/preseed.cfg"},
		{"基地址结尾的 / 去掉", "http://gw/", "t-1", "tok", "ks.cfg", "http://gw/v1/answer/t-1/tok/ks.cfg"},
		// Ubuntu 的 ds=nocloud-net;s= 要以 / 结尾的目录地址，cloud-init 在它下面取 user-data 与 meta-data。
		{"文件名为空时是以 / 结尾的目录地址", "http://gw", "t-1", "tok", "", "http://gw/v1/answer/t-1/tok/"},
		{"文件带子路径", "http://gw", "t-1", "tok", "script/3", "http://gw/v1/answer/t-1/tok/script/3"},
		// / 会多出一层路径；? 与 # 会把后面的部分变成查询串和片段；; 会把 Ubuntu 的 ds=nocloud-net;s=… 切断；
		// % 是转义符本身。
		{"task 里的 / 与空格转义", "http://gw", "a/b c", "tok", "ks.cfg", "http://gw/v1/answer/a%2Fb%20c/tok/ks.cfg"},
		{"token 里的 ? # % ; 转义", "http://gw", "t-1", "x?y#z%;w", "ks.cfg", "http://gw/v1/answer/t-1/x%3Fy%23z%25%3Bw/ks.cfg"},
		{"非 ASCII 按 UTF-8 转义", "http://gw", "任务", "tok", "ks.cfg", "http://gw/v1/answer/%E4%BB%BB%E5%8A%A1/tok/ks.cfg"},
		{"子路径逐段转义，/ 原样留着", "http://gw", "t-1", "tok", "a b/c?d", "http://gw/v1/answer/t-1/tok/a%20b/c%3Fd"},
	}
	for _, c := range cases {
		if got := AnswerURL(c.base, c.task, c.token, c.file); got != c.want {
			t.Errorf("%s：AnswerURL(%q, %q, %q, %q) = %q，想要 %q", c.name, c.base, c.task, c.token, c.file, got, c.want)
		}
	}
}

func Test应答地址按段解开能取回原值(t *testing.T) {
	// 网关按路径段路由：渲染写出的地址，按 / 切段再逐段解开，必须得到原来的 task、token 与 file 的各段，
	// 并且没有多出查询串或片段（spec §6.2 第 3 条）。
	cases := []struct{ task, token, file string }{
		{"t-20261009-0001", "0123456789abcdef", "user-data"},
		{"a/b", "c d", "e/f"},
		{"?q=1", "#frag", "%2F"},
		{"a;s=b", "&x=y", "任务/脚本 1"},
		{"t-1", "tok", "script/"},
	}
	for _, c := range cases {
		got := AnswerURL("http://gw", c.task, c.token, c.file)
		u, err := url.Parse(got)
		if err != nil {
			t.Errorf("AnswerURL(%q, %q, %q) = %q 解析不了：%v", c.task, c.token, c.file, got, err)
			continue
		}
		if u.RawQuery != "" || u.Fragment != "" {
			t.Errorf("AnswerURL(%q, %q, %q) = %q 多出了查询串 %q 或片段 %q", c.task, c.token, c.file, got, u.RawQuery, u.Fragment)
		}
		var segs []string
		for _, s := range strings.Split(u.EscapedPath(), "/")[1:] {
			v, err := url.PathUnescape(s)
			if err != nil {
				t.Errorf("AnswerURL(%q, %q, %q) = %q 的路径段 %q 解不开：%v", c.task, c.token, c.file, got, s, err)
			}
			segs = append(segs, v)
		}
		want := append([]string{"v1", "answer", c.task, c.token}, strings.Split(c.file, "/")...)
		if !slices.Equal(segs, want) {
			t.Errorf("AnswerURL(%q, %q, %q) = %q 解开的路径段 = %q，想要 %q", c.task, c.token, c.file, got, segs, want)
		}
	}
}

func Test目录地址加上文件名就是下发文件的地址(t *testing.T) {
	// Ubuntu 菜单里 s= 写的目录，要正好是 user-data 与 meta-data 所在的目录（第 3 层计划任务 7）。其余格式
	// 的文件也都在同一个目录下，地址只差最后一段。
	dir := AnswerURL("http://gw", "t-1", "tok", "")
	for _, f := range formatTableIDs() {
		for _, k := range fileKindTable {
			name, ok := DeliveryFileName(f, k)
			if !ok {
				continue
			}
			if got := AnswerURL("http://gw", "t-1", "tok", name); got != dir+name {
				t.Errorf("%s 的 %s：AnswerURL(…, %q) = %q，想要目录地址 %q 加文件名", f, k, name, got, dir)
			}
		}
	}
}

func TestArtifactURL按相对路径拼出制品地址(t *testing.T) {
	// rel 取自 ISO.Artifacts，是制品目录下的相对路径，由第 4 层导入时写入（internal/core/model/registry.go 的
	// ISO.Artifacts）。
	cases := []struct {
		name, base, rel, want string
	}{
		{"内核", "http://ng/artifacts", "rocky/9.6/x86_64/images/pxeboot/vmlinuz",
			"http://ng/artifacts/rocky/9.6/x86_64/images/pxeboot/vmlinuz"},
		// 仓库树是目录，inst.repo= 这类参数要目录地址。
		{"目录结尾的 / 留着", "http://ng/artifacts", "rocky/9.6/x86_64/", "http://ng/artifacts/rocky/9.6/x86_64/"},
		{"基地址结尾的 / 去掉", "http://ng/artifacts/", "windows-server/2025/x86_64/sources/boot.wim",
			"http://ng/artifacts/windows-server/2025/x86_64/sources/boot.wim"},
		// 版本合 ^[0-9A-Za-z][0-9A-Za-z._-]{0,31}$，这些字符都不用转义；ESXi ISO 里的文件名是大写的。
		{"版本里的点、下划线、连字符与大写原样保留", "http://ng", "esxi/8.0U3_b-1/x86_64/MBOOT.EFI",
			"http://ng/esxi/8.0U3_b-1/x86_64/MBOOT.EFI"},
		// rel 是盘上的路径，不是转义过的地址：文件名里的 % 也要转义，nginx 解开后才找得到这个文件。
		{"文件名里要转义的字符逐段转义", "http://ng", "ubuntu/24.04/x86_64/ubuntu 24.04 #1?%.iso",
			"http://ng/ubuntu/24.04/x86_64/ubuntu%2024.04%20%231%3F%25.iso"},
	}
	for _, c := range cases {
		if got := ArtifactURL(c.base, c.rel); got != c.want {
			t.Errorf("%s：ArtifactURL(%q, %q) = %q，想要 %q", c.name, c.base, c.rel, got, c.want)
		}
	}
}

func TestArtifactURL对边界上的相对路径照拼不误(t *testing.T) {
	// ArtifactURL 不规范化、也不拒收 rel：rel 合不合规由第 4 层写 ISO.Artifacts 时保证（非空、不以 / 开头、
	// 没有 . 或 .. 段）。这里把现在的行为钉住，要改成拒收或规范化，先改这条测试，调用方也要跟着处理。
	cases := []struct {
		name, rel, want string
	}{
		{"空的 rel 得到基地址加 /", "", "http://ng/artifacts/"},
		{"以 / 开头多出一个空段", "/rocky/9.6/vmlinuz", "http://ng/artifacts//rocky/9.6/vmlinuz"},
		// 转义挡不住 ..：%2E%2E 解开还是 ..，所以防目录穿越只能靠写 rel 的一方。
		{"含 .. 段原样拼进去", "rocky/../../etc/passwd", "http://ng/artifacts/rocky/../../etc/passwd"},
	}
	for _, c := range cases {
		if got := ArtifactURL("http://ng/artifacts", c.rel); got != c.want {
			t.Errorf("%s：ArtifactURL(…, %q) = %q，想要 %q", c.name, c.rel, got, c.want)
		}
	}
}

func Test制品地址解开后就是基地址下的相对路径(t *testing.T) {
	// nginx 把地址解开后按路径找文件，解出来的必须正好是基地址下的 rel，且没有多出查询串或片段。
	for _, rel := range []string{
		"debian/13.6/x86_64/netboot/debian-installer/amd64/linux",
		"ubuntu/24.04/x86_64/",
		"a b/c?d/e#f/100%",
		"任务/脚本",
	} {
		got := ArtifactURL("http://ng/artifacts", rel)
		u, err := url.Parse(got)
		if err != nil {
			t.Errorf("ArtifactURL(%q) = %q 解析不了：%v", rel, got, err)
			continue
		}
		if u.RawQuery != "" || u.Fragment != "" {
			t.Errorf("ArtifactURL(%q) = %q 多出了查询串 %q 或片段 %q", rel, got, u.RawQuery, u.Fragment)
		}
		if want := "/artifacts/" + rel; u.Path != want {
			t.Errorf("ArtifactURL(%q) = %q 解开的路径 = %q，想要 %q", rel, got, u.Path, want)
		}
	}
}
