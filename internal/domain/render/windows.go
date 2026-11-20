package render

import (
	"encoding/base64"
	"fmt"
	"strings"

	"github.com/lober/lober/internal/domain/catalog"
	"github.com/lober/lober/internal/domain/model"
)

func windowsMenu(m menuInput) string {
	startup, name := m.answerURL(catalog.FileAux)
	a := m.artifacts
	return fmt.Sprintf("kernel %s\ninitrd %s %s\ninitrd %s winpeshl.ini\ninitrd %s busybox.exe\ninitrd %s curl.exe\ninitrd %s BCD\ninitrd %s boot.sdi\ninitrd %s boot.wim\n", a["wimboot"], startup, name, a["winpeshl"], a["winpe-busybox"], a["winpe-curl"], a["bcd"], a["boot-sdi"], a["boot-wim"])
}

func windowsAnswerSlot(_ expansion, slot model.SnapshotSlot, _ model.Subject) (string, []model.Problem) {
	if slot.Timing == "firstboot" {
		return "cmd /d /c %SystemDrive%/ProgramData/lober/" + slotFile("unattend", slot), nil
	}
	// 启动文件先下载脚本，WinPE 的应答文件在这里运行它。
	return "cmd /d /c X:/lober/" + slotFile("unattend", slot), nil
}

// startnet is selected by the real injected winpeshl.ini. Stock WinPE HTTP/PS
// availability is never assumed. setup /m copies the local $1 OEM tree only
// after target partitioning; pre hooks run only from windowsPE answer commands.
func renderWindowsAux(r Request) ([]byte, []model.Problem, error) {
	if p := bootSnapshotProblems(r); len(p) != 0 {
		return nil, p, nil
	}
	values, p := assemblerValues(r, "gateway", "task", "token", "finalize-token", "smb", "artifacts")
	tools, problems := requiredArtifacts(r, values["artifacts"])
	p = append(p, problems...)
	subject := model.Subject{Kind: "template", ID: templateRef(r.Snapshot.Template)}
	parts := map[catalog.SystemPart][]byte{}
	for _, part := range []catalog.SystemPart{"firstboot", "cleanup"} {
		body, problems := windowsPartScript(expansion{request: r}, part, subject)
		parts[part] = body
		p = append(p, problems...)
	}
	if len(p) != 0 {
		return nil, p, nil
	}
	var body strings.Builder
	body.WriteString("#!/bin/sh\nset -eu\numask 077\nDIR=X:/lober\nOEM='X:/lober-oem/$1/ProgramData/lober'\nmkdir -p \"$DIR\" \"$OEM\"\n")

	body.WriteString("curl() { \"$DIR/curl.exe\" \"$@\"; }\n")
	for _, role := range catalog.RequiredRoles(r.Snapshot.Series) {
		var filename string
		switch role {
		case "winpe-busybox":
			filename = "busybox.exe"
		case "winpe-curl":
			filename = "curl.exe"
		default:
			continue
		}
		body.WriteString(download("unattend", tools[role], "$OEM/"+filename))
	}
	text := body.String()
	text = strings.ReplaceAll(text, shQuote("$OEM/busybox.exe"), "\"$OEM/busybox.exe\"")
	text = strings.ReplaceAll(text, shQuote("$OEM/curl.exe"), "\"$OEM/curl.exe\"")
	body.Reset()
	body.WriteString(text)
	name, _ := catalog.DeliveryFileName("unattend", catalog.FileAnswer)
	body.WriteString(download("unattend", catalog.AnswerURL(values["gateway"], values["task"], values["token"], name), "X:/lober/"+name))
	body.WriteString("printf '%s' " + shQuote(values["finalize-token"]) + " > \"$OEM/finalize.token\"\n")
	body.WriteString("printf '%s' " + shQuote(base64.StdEncoding.EncodeToString(windowsReportScript(values["gateway"], values["task"]))) + " | base64 -d > \"$OEM/reports.ps1\"\n")
	for _, part := range []catalog.SystemPart{"firstboot", "cleanup"} {
		body.WriteString("printf '%s' " + shQuote(base64.StdEncoding.EncodeToString(parts[part])) + " | base64 -d > \"$OEM/" + windowsPartFile(part) + "\"\n")
	}
	for _, slot := range r.Snapshot.Slots {
		if !slot.Exec {
			continue
		}
		destination := "$DIR/" + slotFile("unattend", slot)
		if slot.Timing == "firstboot" {
			destination = "$OEM/" + slotFile("unattend", slot)
		}
		path, _ := catalog.DeliveryScriptPath(r.Snapshot.Format, slot.Seq)
		fetch := download("unattend", catalog.AnswerURL(values["gateway"], values["task"], values["token"], path), destination)
		fetch = strings.ReplaceAll(fetch, shQuote(destination), "\""+destination+"\"")
		body.WriteString(fetch)
		if slot.Timing == "firstboot" {
			for _, instance := range slot.Instances {
				path := "$OEM/" + hookFile(instance)
				fetch := download("unattend", catalog.AnswerURL(values["gateway"], values["task"], values["token"], "hooks/"+instance.Digest), path)
				fetch = strings.ReplaceAll(fetch, shQuote(path), "\""+path+"\"")
				if instance.Required {
					body.WriteString(fetch)
				} else {
					body.WriteString("(\n" + fetch + ") || :\n")
				}
			}
		}
	}
	// CMD arguments here are only paths and a safe smb value; free hook params
	// remain in the encoded runtime and never participate in command parsing.
	share := strings.ReplaceAll(values["smb"], "\\", "/")
	body.WriteString("net.exe use " + shQuote(strings.ReplaceAll(share, "/", "\\")) + "\n")
	setup := windowsNativeCommand("\"%LOBER_SETUP%\" /unattend:X:/lober/" + name + " /m:X:/lober-oem")
	body.WriteString("printf '%s' " + shQuote(base64.StdEncoding.EncodeToString([]byte(setup))) + " | base64 -d > \"$DIR/setup-launch.cmd\"\n")
	body.WriteString("LOBER_SETUP=" + shQuote(strings.ReplaceAll(share+"/setup.exe", "/", "\\")) + "\nLOBER_NATIVE_RC_FILE=\"$DIR/setup-result.rc\"\nexport LOBER_SETUP LOBER_NATIVE_RC_FILE\ncmd.exe /d /v:off /c \"$DIR/setup-launch.cmd\"\nRC=$(cat \"$LOBER_NATIVE_RC_FILE\") || exit 125\nprintf '%s' \"$RC\" > \"$DIR/startup.rc\"\nexit 0\n")
	// Bootstrap has only ASCII target paths and invokes the injected interpreter.
	bootstrap := windowsShellFile(body.String(), "X:/lober", "startup")
	bootstrap = strings.Replace(bootstrap, "set \"LOBER_DIR=X:/lober\"\r\n", "wpeinit\r\nif errorlevel 1 exit /b %errorlevel%\r\nmkdir X:\\lober\r\ncopy /y \"%SystemRoot%\\System32\\busybox.exe\" X:\\lober\\busybox.exe >nul\r\nif errorlevel 1 exit /b %errorlevel%\r\ncopy /y \"%SystemRoot%\\System32\\curl.exe\" X:\\lober\\curl.exe >nul\r\nif errorlevel 1 exit /b %errorlevel%\r\nset \"LOBER_DIR=X:/lober\"\r\n", 1)
	return []byte(bootstrap), nil, nil
}

func windowsPart(e expansion, part catalog.SystemPart, subject model.Subject) (string, []model.Problem) {
	_, p := windowsPartScript(e, part, subject)
	if len(p) != 0 {
		return "", p
	}
	return "powershell.exe -NoProfile -NonInteractive -ExecutionPolicy Bypass -File %SystemDrive%/ProgramData/lober/" + windowsPartFile(part), nil
}

// FileAux stages these concrete bytes into its local OEM $1/ProgramData/lober
// tree BEFORE setup /m copies them to the target. UTF-8 BOM preserves all text
// on Windows PowerShell 5.1. The answer uses compact -File paths to keep its
// single native command chain within Microsoft's 1024-character field limit.
func windowsPartScript(e expansion, part catalog.SystemPart, subject model.Subject) ([]byte, []model.Problem) {
	_, p := answerValues(e, subject, "gateway", "task")
	if len(p) != 0 {
		return nil, p
	}
	if part != "firstboot" && part != "cleanup" {
		return nil, []model.Problem{model.NewProblem("render_replacement_missing", subject, part)}
	}
	body := "$ErrorActionPreference = 'Stop'\n. (Join-Path $PSScriptRoot 'reports.ps1')\n"
	if part == "cleanup" {
		body = "param([switch]$Replay)\n" + body + `if (-not $Replay) {
    Write-Report '900-init-done' 'init-done'
    Write-Report '901-ready' 'ready'
}
`
	} else {
		body += `Register-ReportReplay
Write-Report '000-booted' 'booted'
Write-Report '001-init-start' 'init-start'
`
	}
	return windowsPowerShellBytes(body + "Send-Reports\nexit 0\n"), nil
}

func windowsPartFile(part catalog.SystemPart) string { return string(part) + ".ps1" }

func windowsPowerShellBytes(text string) []byte { return []byte("\xef\xbb\xbf" + text) }

func psQuote(text string) string { return "'" + strings.ReplaceAll(text, "'", "''") + "'" }
