package render

import (
	"crypto/sha256"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/lober/lober/internal/domain/catalog"
	"github.com/lober/lober/internal/domain/model"
)

// 首次启动数据单独下发，避免把凭据和钩子留在 ESXi 保留的原生脚本里。
func renderESXiStage(r Request) ([]byte, []model.Problem, error) {
	if r.Snapshot.Format != "esxi-ks" {
		return nil, nil, errUnsupportedFormat
	}
	e, problems, err := answerExpansion(r)
	if err != nil || len(problems) != 0 {
		return nil, problems, err
	}
	var b strings.Builder
	b.WriteString("case ${LOBER_ESXI_STAGE_ACTION:-} in\n")
	for _, part := range []catalog.SystemPart{"firstboot", "cleanup"} {
		if command, ok := e.parts[part]; ok {
			b.WriteString(string(part) + ")\n" + command + "\n;;\n")
		}
	}
	for _, slot := range r.Snapshot.Slots {
		if !slot.Exec || slot.Timing != "firstboot" {
			continue
		}
		if command, ok := e.slots[slot.Name]; ok {
			b.WriteString("slot-" + strconv.Itoa(slot.Seq) + ")\n" + command + "\n;;\n")
		}
	}
	b.WriteString("*) exit 66;;\nesac\n")
	return []byte(b.String()), nil, nil
}

var esxiFirstbootHeader = regexp.MustCompile(`(?m)^%firstboot(?:[ \t]|\r?$)`)

func esxiStagePaths(task string) (string, string) {
	digest := sha256.Sum256([]byte(task))
	directory := fmt.Sprintf("/scratch/lober-esxi-%x", digest)
	return directory + "/stage.sh", directory
}

func esxiTemporaryStageDir(task string) string {
	digest := sha256.Sum256([]byte(task))
	return fmt.Sprintf("/tmp/lober-esxi-%x", digest)
}

func esxiDigestCheck(path, digest string) string {
	return "LOBER_HASH=$(sha256sum " + path + ") || exit 127\ncase $LOBER_HASH in " + digest + "\\ *) :;; *) exit 65;; esac\n"
}

func esxiStageAction(action string) string {
	return "LOBER_ESXI_STAGE_ACTION=" + shQuote(action) + ";export LOBER_ESXI_STAGE_ACTION;. \"$LOBER_ESXI_STAGE_PATH\""
}

func esxiNativeStageCommand(task, stagePath, stageDir, digest, action string) string {
	prefix := "LOBER_ESXI_NATIVE_SCRIPT=$0;LOBER_ESXI_OWNER=" + esxiNativeMarker(task) + ";export LOBER_ESXI_NATIVE_SCRIPT LOBER_ESXI_OWNER;"
	body := "set -eu\ncase ${LOBER_ESXI_RUN_ID:-} in ''|*[!0-9]*) exit 66;; esac\nLOBER_ESXI_STAGE_PATH=" + shQuote(stagePath) + "\nexport LOBER_ESXI_STAGE_PATH\n" +
		"[ ! -L " + shQuote(stageDir) + " ] && [ -f \"$LOBER_ESXI_STAGE_PATH\" ] && [ ! -L \"$LOBER_ESXI_STAGE_PATH\" ] && [ \"$(stat -f -c %T /scratch)\" = vmfs6l ] || exit 66\n" +
		"[ \"$(stat -c %u:%a " + shQuote(stageDir) + ")\" = 0:700 ] && [ \"$(stat -c %u:%a \"$LOBER_ESXI_STAGE_PATH\")\" = 0:600 ] || exit 66\n" +
		esxiNativeOwnerCheck(task) + esxiDigestCheck("\"$LOBER_ESXI_STAGE_PATH\"", digest)
	if action == "firstboot" {
		body += "[ ! -e /tmp/lober ] && [ ! -L /tmp/lober ] || exit 66\nmkdir -m 700 /tmp/lober\nprintf '%s:%s' " + shQuote(esxiNativeMarker(task)) + " \"$LOBER_ESXI_RUN_ID\" > /tmp/lober/.owner\n"
	} else {
		body += "[ -d /tmp/lober ] && [ ! -L /tmp/lober ] && [ \"$(stat -c %u:%a /tmp/lober)\" = 0:700 ] && [ -f /tmp/lober/.owner ] && [ ! -L /tmp/lober/.owner ] && [ \"$(cat /tmp/lober/.owner)\" = " + shQuote(esxiNativeMarker(task)) + "\":$LOBER_ESXI_RUN_ID\" ] || exit 66\n"
	}
	body += esxiStageAction(action) + "\n"
	return prefix + esxiEncodedShell(body)
}

type esxiAction struct {
	name    string
	command string
}

func esxiNativeSupervisor(task, stagePath, stageDir, digest string, actions []esxiAction) string {
	var b strings.Builder
	b.WriteString("LOBER_ESXI_RUN_ID=$$\nexport LOBER_ESXI_RUN_ID\nLOBER_ESXI_CHILD=\nLOBER_ESXI_DONE=0\n")
	b.WriteString("lober_esxi_cleanup() {\n" +
		" if [ -d /tmp/lober ] && [ ! -L /tmp/lober ] && [ \"$(stat -c %u:%a /tmp/lober)\" = 0:700 ] && [ -f /tmp/lober/.owner ] && [ ! -L /tmp/lober/.owner ] && [ \"$(cat /tmp/lober/.owner)\" = " + shQuote(esxiNativeMarker(task)) + "\":$LOBER_ESXI_RUN_ID\" ]; then\n" +
		"  if [ ! -L " + shQuote(stageDir) + " ] && [ -f " + shQuote(stagePath) + " ] && [ ! -L " + shQuote(stagePath) + " ] && [ \"$(stat -f -c %T /scratch)\" = vmfs6l ] && [ \"$(stat -c %u:%a " + shQuote(stageDir) + ")\" = 0:700 ] && [ \"$(stat -c %u:%a " + shQuote(stagePath) + ")\" = 0:600 ]; then\n" +
		"   hash=$(sha256sum " + shQuote(stagePath) + " 2>/dev/null || :)\n   case $hash in " + digest + "\\ *) rm -f " + shQuote(stagePath) + " || :; rmdir " + shQuote(stageDir) + " 2>/dev/null || :;; esac\n  fi\n" +
		"  rm -rf /tmp/lober || :\n fi\n}\n" +
		"lober_esxi_abort() {\n code=$1\n trap - HUP INT TERM\n" +
		" if [ -n \"${LOBER_ESXI_CHILD:-}\" ]; then\n" +
		"  kill -TERM \"-$LOBER_ESXI_CHILD\" 2>/dev/null || kill -TERM \"$LOBER_ESXI_CHILD\" 2>/dev/null || :\n" +
		"  wait \"$LOBER_ESXI_CHILD\" 2>/dev/null || :\n fi\n" +
		" lober_esxi_cleanup\n exit \"$code\"\n}\n" +
		"trap 's=$?; if [ \"$LOBER_ESXI_DONE\" -ne 1 ]; then lober_esxi_cleanup; fi; exit \"$s\"' EXIT\n" +
		"trap 'lober_esxi_abort 129' HUP\ntrap 'lober_esxi_abort 130' INT\ntrap 'lober_esxi_abort 143' TERM\n" +
		"lober_esxi_run() {\n case $1 in\n")
	for _, action := range actions {
		b.WriteString("  " + action.name + ") setsid sh -c " + shQuote(action.command) + " \"$0\" & ;;\n")
	}
	b.WriteString("  *) exit 66;;\n esac\n LOBER_ESXI_CHILD=$!\n if wait \"$LOBER_ESXI_CHILD\"; then LOBER_ESXI_STATUS=0; else LOBER_ESXI_STATUS=$?; fi\n LOBER_ESXI_CHILD=\n if [ \"$LOBER_ESXI_STATUS\" -ne 0 ]; then lober_esxi_cleanup; exit \"$LOBER_ESXI_STATUS\"; fi\n if [ \"$1\" = cleanup ]; then LOBER_ESXI_DONE=1; fi\n}\n")
	return b.String()
}

func renderESXiAnswer(r Request, e expansion) ([]byte, []model.Problem, error) {
	if _, ok := e.parts["firstboot"]; !ok {
		b, problems := e.expand(r.Snapshot.Template.Body, r.Snapshot.Template.Params, model.Subject{Kind: "template", ID: templateRef(r.Snapshot.Template)})
		return b, problems, nil
	}
	subject := model.Subject{Kind: "template", ID: templateRef(r.Snapshot.Template)}
	if _, ok := e.parts["cleanup"]; !ok {
		return nil, []model.Problem{model.NewProblem("render_replacement_missing", subject, "cleanup")}, nil
	}
	values, problems := answerValues(e, subject, "gateway", "task", "token")
	if len(problems) != 0 {
		return nil, problems, nil
	}
	stage, problems, err := renderESXiStage(r)
	if err != nil || len(problems) != 0 {
		return nil, problems, err
	}
	digest := sha256.Sum256(stage)
	digestText := fmt.Sprintf("%x", digest)
	stagePath, stageDir := esxiStagePaths(values[1])
	tempDir := esxiTemporaryStageDir(values[1])
	file, _ := catalog.DeliveryFileName("esxi-ks", catalog.FileESXiStage)
	url := catalog.AnswerURL(values[0], values[1], values[2], file)

	pre := "%pre --interpreter=busybox\nset -eu\numask 077\nD=" + shQuote(tempDir) + "\n" +
		"[ ! -e \"$D\" ] && [ ! -L \"$D\" ] || exit 66\nmkdir -m 700 \"$D\"\n" +
		"trap 's=$?; if [ \"$s\" -ne 0 ]; then rm -f \"$D/stage.sh\"; rmdir \"$D\" 2>/dev/null || :; fi' EXIT\n" +
		"trap 'exit 129' HUP\ntrap 'exit 130' INT\ntrap 'exit 143' TERM\n" +
		"timeout -t 30 wget -q -O \"$D/stage.sh\" " + shQuote(url) + "\n" +
		esxiDigestCheck("\"$D/stage.sh\"", digestText) + "trap - EXIT HUP INT TERM\n"
	post := "%post --interpreter=busybox\nset -eu\numask 077\nD=" + shQuote(tempDir) + "\n" +
		"[ -d \"$D\" ] && [ ! -L \"$D\" ] && [ -f \"$D/stage.sh\" ] && [ ! -L \"$D/stage.sh\" ] || exit 66\n" +
		"[ \"$(stat -c %u:%a \"$D\")\" = 0:700 ] && [ \"$(stat -c %u:%a \"$D/stage.sh\")\" = 0:600 ] || exit 66\n" +
		"TARGET=\nCREATED=0\ntrap 's=$?; if [ \"$s\" -ne 0 ]; then if [ \"$CREATED\" -eq 1 ]; then rm -f \"$TARGET/stage.sh\" || :; rmdir \"$TARGET\" 2>/dev/null || :; fi; rm -f \"$D/stage.sh\" || :; rmdir \"$D\" 2>/dev/null || :; fi' EXIT\n" +
		"trap 'exit 129' HUP\ntrap 'exit 130' INT\ntrap 'exit 143' TERM\n" +
		"LOBER_DEVICES=$(localcli storage core device list) || exit 66\n" +
		"LOBER_DISKS=$(printf '%s\\n' \"$LOBER_DEVICES\" | awk '/^[[:space:]]*Device Type:[[:space:]]*Direct-Access[[:space:]]*$/ { n++ } END { print n+0 }') || exit 66\n" +
		"[ \"$LOBER_DISKS\" -eq 1 ] || exit 66\n" +
		"COUNT=0\nVOLUME=\nfor CANDIDATE in /vmfs/volumes/OSDATA-*; do\n [ -d \"$CANDIDATE\" ] || continue\n COUNT=$((COUNT + 1))\n VOLUME=$CANDIDATE\ndone\n" +
		"[ \"$COUNT\" -eq 1 ] && [ \"$(stat -f -c %T \"$VOLUME\")\" = vmfs6l ] || exit 66\n" +
		"TARGET=\"$VOLUME/" + strings.TrimPrefix(stageDir, "/scratch/") + "\"\n" +
		"[ ! -e \"$TARGET\" ] && [ ! -L \"$TARGET\" ] || exit 66\nmkdir -m 700 \"$TARGET\"\nCREATED=1\n" +
		"cp \"$D/stage.sh\" \"$TARGET/stage.sh\"\nchmod 600 \"$TARGET/stage.sh\"\n" +
		esxiDigestCheck("\"$TARGET/stage.sh\"", digestText) +
		"rm -f \"$D/stage.sh\"\nrmdir \"$D\"\ntrap - EXIT HUP INT TERM\n"

	actions := []esxiAction{
		{name: "firstboot", command: esxiNativeStageCommand(values[1], stagePath, stageDir, digestText, "firstboot")},
		{name: "cleanup", command: esxiNativeStageCommand(values[1], stagePath, stageDir, digestText, "cleanup")},
	}
	e.parts["firstboot"] = "lober_esxi_run firstboot"
	e.parts["cleanup"] = "lober_esxi_run cleanup"
	for _, slot := range r.Snapshot.Slots {
		if slot.Exec && slot.Timing == "firstboot" {
			if _, ok := e.slots[slot.Name]; ok {
				name := "slot-" + strconv.Itoa(slot.Seq)
				actions = append(actions, esxiAction{name: name, command: esxiNativeStageCommand(values[1], stagePath, stageDir, digestText, name)})
				e.slots[slot.Name] = "lober_esxi_run " + name
			}
		}
	}
	b, problems := e.expand(r.Snapshot.Template.Body, r.Snapshot.Template.Params, subject)
	if len(problems) != 0 {
		return nil, problems, nil
	}
	headers := esxiFirstbootHeader.FindAllIndex(b, -1)
	if len(headers) != 1 {
		return nil, []model.Problem{model.NewProblem("render_replacement_missing", subject, "%firstboot")}, nil
	}
	header := headers[0]
	lineEnd := strings.IndexByte(string(b[header[0]:]), '\n')
	if lineEnd < 0 {
		return nil, []model.Problem{model.NewProblem("render_replacement_missing", subject, "%firstboot")}, nil
	}
	lineEnd += header[0] + 1
	supervisor := esxiNativeSupervisor(values[1], stagePath, stageDir, digestText, actions)
	result := make([]byte, 0, len(b)+len(pre)+len(post)+len(supervisor))
	result = append(result, b[:header[0]]...)
	result = append(result, pre...)
	result = append(result, post...)
	result = append(result, b[header[0]:lineEnd]...)
	result = append(result, supervisor...)
	result = append(result, b[lineEnd:]...)
	return result, nil, nil
}
