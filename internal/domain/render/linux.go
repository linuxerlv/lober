package render

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/lober/lober/internal/domain/catalog"
	"github.com/lober/lober/internal/domain/model"
)

const linuxConsole = "console=tty0 console=ttyS0,115200n8"

func kickstartMenu(m menuInput) string {
	answer, _ := m.answerURL(catalog.FileAnswer)
	a := m.artifacts
	return fmt.Sprintf("kernel %s \\\n  inst.stage2=%s \\\n  inst.repo=%s \\\n  inst.ks=%s inst.cmdline inst.notmux ip=dhcp \\\n  %s inst.syslog=%s\ninitrd %s\n", a["kernel"], a["stage2"], m.values["repo"], answer, linuxConsole, m.values["syslog-addr"], a["initrd"])
}

func preseedMenu(m menuInput) string {
	answer, _ := m.answerURL(catalog.FileAnswer)
	return fmt.Sprintf("kernel %s auto=true priority=critical \\\n  preseed/url=%s \\\n  %s\ninitrd %s\n", m.artifacts["kernel"], answer, linuxConsole, m.artifacts["initrd"])
}

func autoinstallMenu(m menuInput) string {
	seed := catalog.AnswerURL(m.values["gateway"], m.values["task"], m.values["token"], "")
	// 避免首次配置程序把安装镜像再次当成配置下载。
	return fmt.Sprintf("kernel %s autoinstall ip=dhcp url=%s cloud-config-url=/dev/null ds=nocloud-net;s=%s %s ---\ninitrd %s\n", m.artifacts["kernel"], m.values["iso"], seed, linuxConsole, m.artifacts["initrd"])
}

func renderNoCloudMetadata(r Request) ([]byte, []model.Problem, error) {
	values, problems := assemblerValues(r, "task")
	if len(problems) != 0 {
		return nil, problems, nil
	}
	return []byte(fmt.Sprintf("instance-id: lober-task-%s\n", values["task"])), nil, nil
}

func linuxAnswerPart(e expansion, part catalog.SystemPart, subject model.Subject) (string, []model.Problem) {
	root := targetRoot(e.request.Snapshot.Format)
	switch part {
	case "syslog":
		values, p := answerValues(e, subject, "syslog-addr")
		if len(p) != 0 {
			return "", p
		}
		pos := strings.LastIndex(values[0], ":")
		if pos < 1 {
			return "", []model.Problem{model.NewProblem("render_line_invalid", subject, "syslog-addr")}
		}
		host, port := values[0][:pos], values[0][pos+1:]
		n, err := strconv.Atoi(port)
		if err != nil || n < 1 || n > 65535 {
			return "", []model.Problem{model.NewProblem("render_line_invalid", subject, "syslog-addr")}
		}
		host = strings.TrimPrefix(strings.TrimSuffix(host, "]"), "[")
		return "logging --host=" + host + " --port=" + port, nil
	case "firstboot":
		values, p := answerValues(e, subject, "finalize-token")
		if len(p) != 0 {
			return "", p
		}
		gateway, p := answerValues(e, subject, "gateway", "task")
		if len(p) != 0 {
			return "", p
		}
		var runner strings.Builder
		runner.WriteString("#!/bin/sh\nset -eu\numask 077\nDIR=/var/lib/lober\n")
		runner.WriteString(signalQueue(gateway[0], gateway[1], "", false))
		runner.WriteString("if [ -f \"$DIR/execution.started\" ]; then sh \"$DIR/cleanup.sh\"; exit 0; fi\ntouch \"$DIR/execution.started\"\n")
		runner.WriteString(queuedNativeSignal(model.PhaseBooted))
		runner.WriteString(queuedNativeSignal(model.PhaseInitStart))
		for _, slot := range e.request.Snapshot.Slots {
			if slot.Exec && slot.Timing == "firstboot" {
				runner.WriteString("sh /var/lib/lober/" + slotFile(e.request.Snapshot.Format, slot) + "\n")
			}
		}
		runner.WriteString("touch /var/lib/lober/execution.completed\nsh /var/lib/lober/cleanup.sh\n")
		unit := "[Unit]\nDescription=Lober first boot\nAfter=network-online.target\nWants=network-online.target\nConditionPathExists=/var/lib/lober/firstboot.pending\n[Service]\nType=oneshot\nExecStart=/bin/sh /var/lib/lober/firstboot.sh\n[Install]\nWantedBy=multi-user.target\n"
		body := "set -eu\numask 077\nmkdir -p " + shQuote(root+"/var/lib/lober") + " " + shQuote(root+"/etc/systemd/system/multi-user.target.wants") + "\n"
		body += "printf '%s' " + shQuote(values[0]) + " > " + shQuote(root+"/var/lib/lober/finalize.token") + "\n"
		body += writeHeredoc(root+"/var/lib/lober/firstboot.sh", "LOBER_FIRSTBOOT_EOF", runner.String())
		body += writeHeredoc(root+"/etc/systemd/system/lober-firstboot.service", "LOBER_UNIT_EOF", unit)
		replayUnit := "[Unit]\nDescription=Lober signal replay only\nAfter=network-online.target\nWants=network-online.target\nConditionPathExists=/var/lib/lober/firstboot.pending\n[Service]\nType=oneshot\nExecStart=/bin/sh /var/lib/lober/cleanup.sh\n"
		replayTimer := "[Unit]\nDescription=Retry unacknowledged Lober signals\n[Timer]\nOnBootSec=45s\nOnUnitActiveSec=30s\nUnit=lober-signals.service\n[Install]\nWantedBy=timers.target\n"
		body += "mkdir -p " + shQuote(root+"/etc/systemd/system/timers.target.wants") + "\n"
		body += writeHeredoc(root+"/etc/systemd/system/lober-signals.service", "LOBER_REPLAY_EOF", replayUnit)
		body += writeHeredoc(root+"/etc/systemd/system/lober-signals.timer", "LOBER_TIMER_EOF", replayTimer)
		body += "ln -sf ../lober-signals.timer " + shQuote(root+"/etc/systemd/system/timers.target.wants/lober-signals.timer") + "\n"
		body += "ln -sf ../lober-firstboot.service " + shQuote(root+"/etc/systemd/system/multi-user.target.wants/lober-firstboot.service") + "\ntouch " + shQuote(root+"/var/lib/lober/firstboot.pending") + "\n"
		return installerShellPayload(e.request.Snapshot.Format, body), nil
	case "cleanup":
		values, p := answerValues(e, subject, "gateway", "task", "token")
		if len(p) != 0 {
			return "", p
		}
		cleanup := "#!/bin/sh\nset -eu\numask 077\nDIR=/var/lib/lober\n" + signalQueue(values[0], values[1], "", false)
		cleanup += "if [ ! -f \"$DIR/execution.completed\" ]; then flush_signals || :; exit 0; fi\n"
		cleanup += queuedNativeSignal(model.PhaseInitDone) + queuedNativeSignal(model.PhaseReady)
		cleanup += "flush_signals || exit 0\n"
		cleanup += "rm -f /var/lib/lober/finalize.token /var/lib/lober/firstboot.pending /var/lib/lober/script-*.sh /var/lib/lober/hook-* /var/lib/lober/firstboot.sh /var/lib/lober/cleanup.sh\n"
		cleanup += "rm -f /var/lib/lober/execution.started /var/lib/lober/execution.completed /etc/systemd/system/timers.target.wants/lober-signals.timer /etc/systemd/system/lober-signals.timer /etc/systemd/system/lober-signals.service\n"
		cleanup += "systemctl stop lober-signals.timer >/dev/null 2>&1 || :\n"
		// Installer copies may retain the answer's passwords and boot credential.
		cleanup += "rm -f /root/anaconda-ks.cfg /root/original-ks.cfg /var/log/installer/autoinstall-user-data /var/log/installer/autoinstall-user-data.txt /var/log/installer/cdebconf/questions.dat /var/adm/autoinstall/cache/installedSystem.xml\n"
		// Installer logs persist tokenized answer and signal URLs from kernel arguments.
		cleanup += "for ROOT in /var/lib/lober /var/lib/cloud /var/log/installer /var/log/anaconda /var/log/YaST2 /var/adm/autoinstall/cache /root; do\n"
		cleanup += " [ -d \"$ROOT\" ] || continue\n"
		cleanup += " grep -rlI -E '/v1/(answer|signal)/[^/[:space:]]+/[A-Za-z0-9_.=-]{32,}/' \"$ROOT\" 2>/dev/null | while IFS= read -r FILE; do\n"
		cleanup += "  sed -i -E 's#(/v1/(answer|signal)/[^/[:space:]]+/)[A-Za-z0-9_.=-]{32,}/#\\1[redacted]/#g' \"$FILE\"\n"
		cleanup += " done\n"
		cleanup += "done\n"
		staging := "set -eu\numask 077\nmkdir -p " + shQuote(root+"/var/lib/lober") + "\n" + writeHeredoc(root+"/var/lib/lober/cleanup.sh", "LOBER_CLEANUP_EOF", cleanup)
		staging += signalQueue(values[0], values[1], values[2], e.request.Snapshot.Format == "preseed")
		staging += "for DIR in /var/lib/lober " + shQuote(root+"/var/lib/lober") + "; do flush_signals; done\n"
		// Acknowledge the irreversible boundary before the installer reboots. Keep
		// the install credential in installer code only, never in the target runner.
		address := catalog.SignalURL(values[0], values[1], values[2], string(model.PhaseInstalled))
		if e.request.Snapshot.Format == "preseed" {
			staging += "wget -q -T 10 --tries=4 -O /dev/null --post-data='' " + shQuote(address) + "\n"
		} else {
			staging += "curl -fsS --retry 3 --retry-all-errors --max-time 10 -X POST " + shQuote(address) + " --data-binary '' >/dev/null\n"
		}
		return installerShellPayload(e.request.Snapshot.Format, staging), nil
	}
	return "", []model.Problem{model.NewProblem("render_replacement_missing", subject, part)}
}
