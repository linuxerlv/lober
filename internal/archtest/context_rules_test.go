package archtest

import (
	"strings"
	"testing"
	"testing/fstest"
)

func Test领域禁止通过上下文计时或启动异步回调(t *testing.T) {
	for _, pkg := range []string{pkgModel, pkgStore, pkgStore + "/nested", layer1 + "/selection", pkgRender} {
		check := checkSource
		if under(pkg, pkgStore) {
			check = checkStoreSource
		}
		if under(pkg, pkgRender) {
			check = checkLayer3Source
		}
		for _, operation := range []string{"WithTimeout", "WithTimeoutCause", "WithDeadline", "WithDeadlineCause", "AfterFunc"} {
			for _, alias := range []string{"context", "ctxpkg", "."} {
				importSpec := alias + ` "context"`
				if alias == "context" {
					importSpec = `"context"`
				}
				prefix := alias + "."
				if alias == "." {
					prefix = ""
				}
				call := prefix + operation + "(ctx, 1)"
				switch operation {
				case "WithTimeoutCause":
					call = prefix + operation + "(ctx, 1, nil)"
				case "WithDeadline":
					call = prefix + operation + "(ctx, deadline)"
				case "WithDeadlineCause":
					call = prefix + operation + "(ctx, deadline, nil)"
				case "AfterFunc":
					call = prefix + operation + "(ctx, func(){})"
				}
				body := "func f(ctx " + prefix + "Context) { deadline, _ := ctx.Deadline(); _ = deadline; " + call + " }"
				for _, source := range []string{body, "var effect = " + prefix + operation} {
					t.Run(pkg+"/"+operation+"/"+alias+"/"+source[:3], func(t *testing.T) {
						got := check(t, pkg, withImport(importSpec, source))
						if len(got) == 0 {
							t.Fatal("领域代码绕过了时钟和异步操作限制")
						}
					})
				}
			}
		}
		for _, alias := range []string{"context", "ctxpkg"} {
			source := "func pass(ctx " + alias + ".Context) " + alias + ".Context { return ctx }\n// " + alias + ".WithTimeout 只是注释\nvar note = \"" + alias + ".AfterFunc\""
			if got := check(t, pkg, withImport(alias+` "context"`, source)); len(got) != 0 {
				t.Fatalf("上下文契约类型被误拒绝：%v", got)
			}
		}
		if got := check(t, pkg, "package p\ntype scheduler struct{}\nfunc (scheduler) WithTimeout() {}\nfunc f(){ var context scheduler; context.WithTimeout() }"); len(got) != 0 {
			t.Fatalf("本地同名方法被误报：%v", got)
		}
	}
}

func Test领域上下文限制接入全模块检查(t *testing.T) {
	pkg := pkgStore + "/nested"
	name := "internal/domain/repository/nested/x.go"
	fsys := fstest.MapFS{name: {Data: []byte(withImport(`c "context"`, "var effect = c.WithTimeoutCause"))}}
	got, counts := checkModule(fsys, []string{"internal/domain/repository/nested"}, []goPackage{{ImportPath: pkg, GoFiles: []string{"x.go"}}})
	if len(got) != 1 || !strings.Contains(got[0].subject, "context.WithTimeoutCause") || counts[layerStore] != 1 {
		t.Fatalf("仓储子目录的计时入口漏出检查：%v %v", got, counts)
	}
	// 数据库夹具仍能给操作设置时限，不能把领域限制误套到实现。
	if got := checkStoreSource(t, pkgPgtest, withImport(`"context"`, "var timeout = context.WithTimeout")); len(got) != 0 {
		t.Fatalf("数据库夹具被误拒绝：%v", got)
	}
}

func Test上下文导入被局部同名变量遮住不误报(t *testing.T) {
	for _, alias := range []string{"context", "c"} {
		body := "type scheduler struct{}\nfunc (scheduler) WithTimeout() {}\nfunc local(ctx " + alias + ".Context) { " + alias + " := scheduler{}; " + alias + ".WithTimeout() }\n"
		for _, realUse := range []string{"", "func real(ctx " + alias + ".Context) { " + alias + ".WithTimeout(ctx, 1) }", "var real = " + alias + ".WithTimeout"} {
			source := withImport(alias+` "context"`, body+realUse)
			want := 0
			if realUse != "" {
				want = 1
			}
			check := func(got []violation) {
				t.Helper()
				if len(got) != want {
					t.Fatalf("局部变量与真实包调用混淆：%v，期望%d条", got, want)
				}
				if want == 1 && got[0].subject != "context.WithTimeout" {
					t.Fatal("没有报告真实包调用", got)
				}
			}
			check(checkStoreSource(t, pkgStore+"/nested", source))
			fsys := fstest.MapFS{"internal/domain/repository/nested/x.go": {Data: []byte(source)}}
			got, _ := checkModule(fsys, nil, []goPackage{{ImportPath: pkgStore + "/nested", GoFiles: []string{"x.go"}}})
			check(got)
		}
	}
}
