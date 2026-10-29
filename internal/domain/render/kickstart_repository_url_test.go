package render

import (
	"strings"
	"testing"

	"github.com/lober/lober/internal/domain/catalog"
)

func TestKickstartAnswerRepositoryURLHasNoTrailingSlash(t *testing.T) {
	for _, series := range []catalog.Series{"almalinux", "rocky"} {
		for _, firmware := range []catalog.Firmware{"bios", "uefi"} {
			for _, repoPath := range []string{"media/install/", "media/install"} {
				t.Run(string(series)+"/"+string(firmware)+"/"+repoPath, func(t *testing.T) {
					r := answerFixture(t, "kickstart", false, firmware)
					r.Snapshot.Series = series
					r.Snapshot.ISO.Artifacts["repo"] = repoPath
					got, problems, err := Render(r)
					if err != nil || len(problems) != 0 {
						t.Fatalf("render: %v %v", problems, err)
					}
					var directives []string
					for _, line := range strings.Split(string(got), "\n") {
						if strings.HasPrefix(line, "url --url=") {
							directives = append(directives, line)
						}
					}
					want := "url --url=http://ng.internal/artifacts/media/install"
					if len(directives) != 1 || directives[0] != want {
						t.Fatalf("安装仓库指令 = %q，期望唯一指令 %q", directives, want)
					}
					if r.Snapshot.ISO.Artifacts["repo"] != repoPath {
						t.Fatal("渲染修改了原仓库目录角色")
					}
				})
			}
		}
	}
}

func TestKickstartMenuRepositoryURLPreservesStage2(t *testing.T) {
	for _, series := range []catalog.Series{"almalinux", "rocky"} {
		t.Run(string(series), func(t *testing.T) {
			r := menuFixture(series, "uefi")
			// 真实最小盘的仓库根和安装器镜像目录均保留末尾斜杠。
			r.Snapshot.ISO.Artifacts["repo"] = "media/install/"
			r.Snapshot.ISO.Artifacts["stage2"] = "media/install/"
			got, problems, err := Render(r)
			if err != nil || len(problems) != 0 {
				t.Fatalf("render: %v %v", problems, err)
			}
			for _, want := range []string{
				"inst.repo=http://ng.internal/artifacts/media/install",
				"inst.stage2=http://ng.internal/artifacts/media/install/",
			} {
				found := false
				for _, field := range strings.Fields(string(got)) {
					if field == want {
						found = true
					}
				}
				if !found {
					t.Errorf("引导菜单缺少完整参数 %q：\n%s", want, got)
				}
			}
		})
	}
}

func TestOtherAnswerFormatsPreserveDirectoryRepositoryURL(t *testing.T) {
	for _, format := range []catalog.Format{"preseed", "autoinstall", "autoyast", "unattend"} {
		t.Run(string(format), func(t *testing.T) {
			r := answerFixture(t, format, false, "uefi")
			r.Snapshot.ISO.Artifacts["repo"] = "media/install/"
			comment, want := "# repo={{sys:repo}} iso={{sys:iso}}\n", "# repo=http://ng.internal/artifacts/media/install/ iso=http://ng.internal/artifacts/fixture/image.iso\n"
			if format == "autoyast" || format == "unattend" {
				comment = "<!-- repo={{sys:repo}} iso={{sys:iso}} -->\n"
				want = "<!-- repo=http://ng.internal/artifacts/media/install/ iso=http://ng.internal/artifacts/fixture/image.iso -->\n"
			}
			r.Snapshot.Template.Body += comment
			got, problems, err := Render(r)
			if err != nil || len(problems) != 0 {
				t.Fatalf("render: %v %v", problems, err)
			}
			if !strings.Contains(string(got), want) {
				t.Fatalf("其他格式的仓库或镜像地址发生变化，缺少 %q", want)
			}
		})
	}
}
