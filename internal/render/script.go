package render

import (
	"encoding/base64"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/lober/lober/internal/core/catalog"
	"github.com/lober/lober/internal/core/model"
)

func renderScript(r Request) ([]byte, []model.Problem, error) {
	slot, _ := findSlot(r.Snapshot, r.Slot)
	if r.Snapshot.Format == "unattend" {
		return renderWindowsScript(r, slot)
	}
	body, p := posixRuntime(r, slot, false)
	if len(p) != 0 {
		return nil, p, nil
	}
	return []byte(body), nil, nil
}

// Only generated target code performs IO. The renderer copies frozen hook bytes
// and parameters; it never evaluates hook bodies or consults the repository.
func posixRuntime(r Request, slot model.SnapshotSlot, windows bool) (string, []model.Problem) {
	keys := []catalog.SystemValue{"gateway", "task"}
	if slot.Timing != "firstboot" {
		keys = append(keys, "token")
	}
	values, p := assemblerValues(r, keys...)
	if len(p) != 0 {
		return "", p
	}
	esxi := r.Snapshot.Format == "esxi-ks"
	var b strings.Builder
	b.WriteString("#!/bin/sh\nset -u\numask 077\n")
	if !windows {
		directory := "/var/lib/lober"
		if esxi {
			directory = "/tmp/lober"
		}
		b.WriteString("DIR=" + directory + "\n")
	}
	if windows {
		b.WriteString("DIR=$LOBER_DIR\n")
		for _, applet := range []string{"mkdir", "cat", "mktemp", "rm", "sleep", "sha256sum", "tail", "base64", "head"} {
			b.WriteString(applet + "() { \"$DIR/busybox.exe\" " + applet + " \"$@\"; }\n")
		}
		b.WriteString("curl() { \"$DIR/curl.exe\" \"$@\"; }\n[ -f \"$DIR/curl.exe\" ] || exit 127\n")
	} else {
		b.WriteString("for tool in sha256sum sleep mktemp tail cat base64; do command -v \"$tool\" >/dev/null 2>&1 || exit 127; done\n")
	}
	if esxi {
		b.WriteString("command -v setsid >/dev/null 2>&1 || exit 127\nsetsid sh -c 'kill -0 -$$' || exit 125\n")
	}
	b.WriteString("mkdir -p \"$DIR\" || exit $?\n")
	if slot.Timing == "firstboot" {
		b.WriteString("TOKEN=$(cat \"$DIR/finalize.token\") || exit $?\n")
	} else {
		b.WriteString("TOKEN=" + shQuote(values["token"]) + "\n")
	}
	b.WriteString("WORK=$(mktemp -d \"$DIR/run-" + strconv.Itoa(slot.Seq) + ".XXXXXX\") || exit $?\ntrap 'rm -rf \"$WORK\"' EXIT\n")
	b.WriteString(runtimeSignalFunction(r, slot, values))
	if windows {
		b.WriteString(powerShellCopyScript())
		b.WriteString("finish_slot() { printf '%s' \"$1\" > \"$DIR/runtime-" + strconv.Itoa(slot.Seq) + ".rc\" || exit 125; exit 0; }\n")
	}
	switch {
	case windows:
		// Maintainer's negative-PID kill enumerates descendants; no external timeout
		// or WinPE WMI/PowerShell process APIs are assumed.
		b.WriteString("kill_tree() { \"$DIR/busybox.exe\" kill -KILL \"-$1\" 2>/dev/null || :; }\n")
	case esxi:
		b.WriteString("kill_tree() { kill -KILL \"-$1\" 2>/dev/null || kill -KILL \"$1\" 2>/dev/null || :; }\n")
	default:
		b.WriteString(`kill_tree() {
 children=$(cat "/proc/$1/task/$1/children" 2>/dev/null) || children=
 for child in $children; do kill_tree "$child"; done
 kill -KILL "$1" 2>/dev/null || :
}
`)
	}
	b.WriteString("run_bounded() {\n seconds=$1; shift\n ")
	if esxi {
		b.WriteString("setsid ")
	}
	b.WriteString(`"$@" &
 process=$!
`)
	if esxi {
		// Publish readiness only after the watchdog owns its child and TERM trap.
		// Cancellation reaps the sleeper without touching the hook's process group.
		b.WriteString(` (
 sleep "$seconds" &
 sleeper=$!
 trap 'kill "$sleeper" 2>/dev/null || :; wait "$sleeper" 2>/dev/null || :; exit 0' TERM
 : > "$WORK/watchdog-ready" || { kill "$sleeper" 2>/dev/null || :; wait "$sleeper" 2>/dev/null || :; exit 125; }
 wait "$sleeper" || exit 0
 : > "$WORK/timed-out"
 kill_tree "$process"
 ) >/dev/null 2>&1 &
 watchdog=$!
 while [ ! -f "$WORK/watchdog-ready" ]; do
  if ! kill -0 "$watchdog" 2>/dev/null; then
   kill_tree "$process"; wait "$process" 2>/dev/null || :
   wait "$watchdog" 2>/dev/null || :
   return 125
  fi
 done
 if wait "$process"; then status=0; else status=$?; fi
 kill -TERM "$watchdog" 2>/dev/null || :
`)
	} else {
		b.WriteString(` (sleep "$seconds" || exit 0; : > "$WORK/timed-out"; kill_tree "$process") >/dev/null 2>&1 &
 watchdog=$!
 if wait "$process"; then status=0; else status=$?; fi
 kill_tree "$watchdog"
`)
	}
	b.WriteString(" wait \"$watchdog\" 2>/dev/null || :\n")
	if esxi {
		b.WriteString(" rm -f \"$WORK/watchdog-ready\"\n")
	}
	b.WriteString(` if [ -f "$WORK/timed-out" ]; then status=124; fi
 rm -f "$WORK/timed-out"
 return "$status"
}
signal slot-start 0 0 '' ''
`)
	for index, instance := range slot.Instances {
		n := strconv.Itoa(index + 1)
		b.WriteString("HOOK=\"$DIR/" + hookFile(instance) + "\"\nLOG=\"$WORK/hook-" + n + ".log\"\n: > \"$LOG\"\nRC=0\nsignal hook-start " + n + " 0 " + shQuote(instance.Digest) + " ''\n")
		if slot.Timing != "firstboot" {
			address := catalog.AnswerURL(values["gateway"], values["task"], values["token"], "hooks/"+instance.Digest)
			fetch := strings.TrimSuffix(download(r.Snapshot.Format, address, "$HOOK"), "\n")
			fetch = strings.ReplaceAll(fetch, shQuote("$HOOK"), "\"$HOOK\"")
			b.WriteString("if " + fetch + " >\"$LOG\" 2>&1; then :; else RC=$?; fi\n")
		}
		b.WriteString("if [ \"$RC\" -eq 0 ]; then\n HASH=$(sha256sum \"$HOOK\" 2>>\"$LOG\") || HASH=\n HASH=${HASH%% *}\n if [ \"$HASH\" != " + shQuote(instance.Digest) + " ]; then RC=65; printf '%s\\n' 'hook digest mismatch' >>\"$LOG\"; fi\nfi\n")
		if windows {
			if instance.Interpreter == "powershell" {
				b.WriteString("if [ \"$RC\" -eq 0 ]; then\n EXEC_HOOK=\"$WORK/powershell-" + n + ".ps1\"\n if prepare_powershell \"$HOOK\" \"$EXEC_HOOK\" >>\"$LOG\" 2>&1; then HOOK=$EXEC_HOOK; else RC=$?; fi\nfi\n")
			}
			b.WriteString("NATIVE_LAUNCH=\"$WORK/native-" + n + ".cmd\"\nLOBER_NATIVE_RC_FILE=\"$WORK/native-" + n + ".rc\"\nexport NATIVE_LAUNCH LOBER_NATIVE_RC_FILE\n")
			b.WriteString("printf '%s' " + shQuote(base64.StdEncoding.EncodeToString([]byte(windowsNativeLauncher(instance.Interpreter)))) + " | base64 -d > \"$NATIVE_LAUNCH\" || exit $?\n")
		}
		launcher := runtimeLauncher(instance, windows)
		b.WriteString("printf '%s' " + shQuote(base64.StdEncoding.EncodeToString([]byte(launcher))) + " | base64 -d > \"$WORK/launch-" + n + ".sh\" || exit $?\n")
		program := "sh"
		if windows {
			program = "\"$DIR/busybox.exe\" sh -X"
		}
		// HOOK is a private fixed path, never an author-controlled command argument.
		b.WriteString("export HOOK\nif [ \"$RC\" -eq 0 ]; then\n if run_bounded " + strconv.Itoa(instance.TimeoutSec) + " " + program + " \"$WORK/launch-" + n + ".sh\" >>\"$LOG\" 2>&1; then RC=0; else RC=$?; fi\n")
		if windows {
			b.WriteString(" if [ \"$RC\" -eq 0 ]; then\n  if [ -f \"$LOBER_NATIVE_RC_FILE\" ]; then RC=$(cat \"$LOBER_NATIVE_RC_FILE\"); else RC=125; fi\n  case $RC in ''|-|*[!0-9-]*) RC=125;; esac\n fi\n")
		}
		b.WriteString("fi\ntail -c 4096 \"$LOG\" > \"$WORK/tail\"\nsignal hook-report " + n + " \"$RC\" " + shQuote(instance.Digest) + " \"$WORK/tail\"\n")
		if instance.Required {
			finish := "exit \"$RC\""
			if windows {
				finish = "finish_slot \"$RC\""
			}
			b.WriteString("if [ \"$RC\" -ne 0 ]; then signal slot-end 0 \"$RC\" '' ''; " + finish + "; fi\n")
		}
	}
	b.WriteString("signal slot-end 0 0 '' ''\n")
	if windows {
		b.WriteString("finish_slot 0\n")
	} else {
		b.WriteString("exit 0\n")
	}
	return b.String(), nil
}

func runtimeLauncher(instance model.SnapshotInstance, windows bool) string {
	var b strings.Builder
	b.WriteString("#!/bin/sh\n")
	names := make([]string, 0, len(instance.Params))
	for name := range instance.Params {
		names = append(names, name)
	}
	slices.Sort(names)
	for _, name := range names {
		b.WriteString("export " + model.EnvName(name) + "=" + shQuote(instance.Params[name].EnvValue()) + "\n")
	}
	command := string(instance.Interpreter) + " \"$HOOK\""
	if windows {
		command = "cmd.exe /d /v:off /c \"$NATIVE_LAUNCH\""
	}

	b.WriteString("exec " + command + "\n")
	return b.String()
}

func runtimeSignalFunction(r Request, slot model.SnapshotSlot, values map[catalog.SystemValue]string) string {
	if r.Snapshot.Format != "unattend" && r.Snapshot.Format != "esxi-ks" {
		token := values["token"]
		if slot.Timing == "firstboot" {
			token = ""
		}
		wget := r.Snapshot.Format == "preseed" && (slot.Timing == "pre" || slot.Timing == "post-installer")
		return signalQueue(values["gateway"], values["task"], token, wget) + fmt.Sprintf(`signal() {
 phase=$1; instance=$2; rc=$3; digest=$4; body=$5
 label_instance=$instance; label_step=0
 case "$phase" in hook-report) label_step=1;; slot-end) label_instance=999999999; label_step=9;; esac
 label=$(printf '1-%%09d-%%09d-%%d' %d "$label_instance" "$label_step")
 request="$phase?slot=%d&timing=%s&rc=$rc"
 if [ "$instance" -ne 0 ]; then request="$request&instance=$instance&digest=$digest"; fi
 queue_signal "$label" "$request" "$body"
}
`, slot.Seq, slot.Seq, slot.Timing)
	}
	address := catalog.SignalURL(values["gateway"], values["task"], "TOKEN", "PHASE")
	before, after := tokenURLParts(address)
	after = strings.TrimSuffix(after, "PHASE")
	transport := "curl -fsS -m 10 -X POST -H 'Content-Type: text/plain' \"$address\" --data-binary \"$body\""
	wgetSignal := r.Snapshot.Format == "esxi-ks" || (r.Snapshot.Format == "preseed" && (slot.Timing == "pre" || slot.Timing == "post-installer"))
	if wgetSignal {
		transport = "wget -q -T 10 -O /dev/null --header='Content-Type: text/plain' --post-file=\"$body\" \"$address\""
	}
	// Debian's packaged GNU wget must bypass standalone ash's wget applet;
	// that applet truncates --post-file bodies containing NUL bytes.
	if r.Snapshot.Format == "preseed" && wgetSignal {
		transport = "/usr/bin/" + transport
	}
	body := "if [ -n \"$body\" ]; then body=\"@$body\"; fi"
	if wgetSignal {
		body = "if [ -z \"$body\" ]; then body=\"$WORK/empty\"; : > \"$body\"; fi"
	}
	return fmt.Sprintf(`signal() {
 phase=$1; instance=$2; rc=$3; digest=$4; body=$5
 address=%s"$TOKEN"%s"$phase"'?slot=%d&timing=%s&rc='"$rc"
 if [ "$instance" -ne 0 ]; then address="$address&instance=$instance&digest=$digest"; fi
 %s
 %s >/dev/null 2>&1 || :
}
`, shQuote(before), shQuote(after), slot.Seq, slot.Timing, body, transport)
}

func renderWindowsScript(r Request, slot model.SnapshotSlot) ([]byte, []model.Problem, error) {
	if p := bootSnapshotProblems(r); len(p) != 0 {
		return nil, p, nil
	}
	var p []model.Problem
	for _, role := range catalog.RequiredRoles(r.Snapshot.Series) {
		_, problem := artifactURL(r, "", role)
		if problem != nil {
			p = append(p, *problem)
		}
	}
	for index, instance := range slot.Instances {
		// Every Windows hook passes through the native CMD status-capture helper.
		limit := 8191
		names := make([]string, 0, len(instance.Params))
		for name := range instance.Params {
			names = append(names, name)
		}
		slices.Sort(names)
		for _, name := range names {
			value := instance.Params[name]
			units := 0
			for _, char := range model.EnvName(name) + "=" + value.EnvValue() {
				units++
				if char > 0xffff {
					units++
				}
			}
			if units > limit {
				p = append(p, model.NewProblem("render_windows_env_limit", model.Subject{Kind: "slot", ID: templateRef(r.Snapshot.Template), Path: model.InstanceFieldKey(slot.Name, index+1, name)}, name))
			}
		}
	}
	if len(p) != 0 {
		return nil, p, nil
	}
	body, p := posixRuntime(r, slot, true)
	if len(p) != 0 {
		return nil, p, nil
	}
	dir := "X:/lober"
	if slot.Timing == "firstboot" {
		dir = "%SystemDrive%/ProgramData/lober"
	}
	return []byte(windowsShellFile(body, dir, "runtime-"+strconv.Itoa(slot.Seq))), nil, nil
}

// Arbitrary text never enters CMD parsing; only short ASCII base64 chunks do.
func windowsShellFile(body, dir, name string) string {
	var b strings.Builder
	b.WriteString("@echo off\r\nsetlocal DisableDelayedExpansion\r\nset \"ERRORLEVEL=\"\r\nset \"LOBER_DIR=" + dir + "\"\r\n")
	b.WriteString("del /q \"%LOBER_DIR%/" + name + ".rc\" >nul 2>nul\r\nif exist \"%LOBER_DIR%/" + name + ".rc\" exit /b 125\r\ncmd /d /c exit 0\r\n")
	encoded := base64.StdEncoding.EncodeToString([]byte(body))
	for offset := 0; offset < len(encoded); offset += 2048 {
		end := offset + 2048
		if end > len(encoded) {
			end = len(encoded)
		}
		redirect := ">"
		if offset != 0 {
			redirect = ">>"
		}
		b.WriteString(redirect + " \"%LOBER_DIR%/" + name + ".b64\" echo " + encoded[offset:end] + "\r\nif errorlevel 1 exit /b %errorlevel%\r\n")
	}
	b.WriteString("\"%LOBER_DIR%/busybox.exe\" base64 -d \"%LOBER_DIR%/" + name + ".b64\" > \"%LOBER_DIR%/" + name + ".sh\"\r\nif errorlevel 1 exit /b %errorlevel%\r\n")
	b.WriteString("\"%LOBER_DIR%/busybox.exe\" sh -X \"%LOBER_DIR%/" + name + ".sh\"\r\nset \"RC=%errorlevel%\"\r\nif \"%RC%\"==\"0\" if exist \"%LOBER_DIR%/" + name + ".rc\" set /p \"RC=\"<\"%LOBER_DIR%/" + name + ".rc\"\r\ndel /q \"%LOBER_DIR%/" + name + ".b64\" \"%LOBER_DIR%/" + name + ".sh\" \"%LOBER_DIR%/" + name + ".rc\" >nul 2>nul\r\nendlocal & exit /b %RC%\r\n")
	return b.String()
}

// CMD preserves the native 32-bit process status in a data file; POSIX shell
// status alone cannot represent every Windows installer/hook failure code.
func windowsNativeLauncher(interpreter catalog.Interpreter) string {
	command := "cmd.exe /d /v:off /c \"%HOOK%\""
	if interpreter == "powershell" {
		command = "powershell.exe -NoProfile -NonInteractive -ExecutionPolicy Bypass -File \"%HOOK%\""
	}
	return windowsNativeCommand(command)
}

func windowsNativeCommand(command string) string {
	return "@echo off\r\nsetlocal DisableDelayedExpansion\r\nset \"ERRORLEVEL=\"\r\n" + command + "\r\nset \"LOBER_NATIVE_RC=%errorlevel%\"\r\n> \"%LOBER_NATIVE_RC_FILE%\" echo %LOBER_NATIVE_RC%\r\nexit /b 0\r\n"
}

// Hash the original cache first; BOM is only an execution-encoding hint for
// Windows PowerShell5.1. Existing BOM is retained exactly once.
func powerShellCopyScript() string {
	return `prepare_powershell() {
 head -c 3 "$1" > "$WORK/prefix" || return $?
 PREFIX=$(base64 "$WORK/prefix") || return $?
 : > "$2" || return $?
 if [ "$PREFIX" != '77u/' ]; then printf '\357\273\277' >> "$2" || return $?; fi
 cat "$1" >> "$2"
}
`
}
