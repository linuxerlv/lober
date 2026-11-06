package render

import (
	"crypto/sha256"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/lober/lober/internal/domain/catalog"
	"github.com/lober/lober/internal/domain/model"
)

func TestESXiFirstbootTerminatingOuterScriptCleansAndStopsHook(t *testing.T) {
	d := t.TempDir()
	bin := filepath.Join(d, "bin")
	if err := os.Mkdir(bin, 0700); err != nil {
		t.Fatal(err)
	}
	for name, body := range map[string]string{
		"stat":    "#!/bin/sh\nif [ \"$1\" = -f ]; then echo vmfs6l; elif [ \"$1\" = -c ]; then case \"$3\" in */stage.sh) echo 0:600;; *) echo 0:700;; esac; else exit 1; fi\n",
		"python3": "#!/bin/sh\nexit 7\n",
	} {
		if err := os.WriteFile(filepath.Join(bin, name), []byte(body), 0700); err != nil {
			t.Fatal(err)
		}
	}
	started, pidFile := filepath.Join(d, "started"), filepath.Join(d, "hook.pid")
	local, stageDir := filepath.Join(d, "local"), filepath.Join(d, "stage")
	if err := os.Mkdir(stageDir, 0700); err != nil {
		t.Fatal(err)
	}
	r := esxiAnswerFixture()
	r.Snapshot.Template.Body = "vmaccepteula\n%firstboot --interpreter=busybox\nset -eu\n{{lober:firstboot}}\n{{slot:运行@firstboot}}\n{{lober:cleanup}}\n"
	r.Snapshot.Slots = []model.SnapshotSlot{{Seq: 3, Name: "运行", Exec: true, Timing: "firstboot", Instances: []model.SnapshotInstance{runtimeInstance("printf '%s' \"$$\" > \"$QA_PID\"\nprintf started > \"$QA_STARTED\"\nsleep 20\n", true, "sh", 10)}}}
	answer, problems, err := Render(r)
	if err != nil || len(problems) != 0 {
		t.Fatalf("answer: %v %v", problems, err)
	}
	r.Kind = catalog.FileESXiStage
	stage, problems, err := Render(r)
	if err != nil || len(problems) != 0 {
		t.Fatalf("stage: %v %v", problems, err)
	}
	_, native, ok := strings.Cut(string(answer), "%firstboot --interpreter=busybox\n")
	if !ok {
		t.Fatal("missing native firstboot")
	}
	_, originalDir := esxiStagePaths(r.Values["task"])
	oldDigest := fmt.Sprintf("%x", sha256.Sum256(stage))
	stage = []byte(relocateESXiCommands(t, string(stage), local, originalDir, stageDir, "", ""))
	stage = []byte(strings.ReplaceAll(string(stage), "stat -", shQuote(filepath.Join(bin, "stat"))+" -"))
	newDigest := fmt.Sprintf("%x", sha256.Sum256(stage))
	stagePath := filepath.Join(stageDir, "stage.sh")
	if err := os.WriteFile(stagePath, stage, 0600); err != nil {
		t.Fatal(err)
	}
	native = relocateESXiCommands(t, native, local, originalDir, stageDir, oldDigest, newDigest)
	native = strings.ReplaceAll(native, "stat -", shQuote(filepath.Join(bin, "stat"))+" -")
	path := filepath.Join(d, "native.sh")
	if err := os.WriteFile(path, []byte(native), 0600); err != nil {
		t.Fatal(err)
	}
	busybox, err := exec.LookPath("busybox")
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(busybox, "sh", path)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Env = append(os.Environ(), "PATH="+bin+":"+os.Getenv("PATH"), "QA_PID="+pidFile, "QA_STARTED="+started)
	cmd.Stdout, cmd.Stderr = io.Discard, io.Discard
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if cmd.ProcessState == nil {
			_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
			_, _ = cmd.Process.Wait()
		}
		if raw, err := os.ReadFile(pidFile); err == nil {
			pid, _ := strconv.Atoi(string(raw))
			if pid > 1 {
				if pgid, err := syscall.Getpgid(pid); err == nil && pgid > 1 && pgid != syscall.Getpgrp() {
					_ = syscall.Kill(-pgid, syscall.SIGKILL)
				}
			}
		}
	}()
	deadline := time.Now().Add(8 * time.Second)
	for {
		if _, err := os.Stat(started); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("hook did not start")
		}
		time.Sleep(20 * time.Millisecond)
	}
	if err := cmd.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("terminated firstboot reported success")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("firstboot did not stop after termination")
	}
	if _, err := os.Stat(stagePath); !os.IsNotExist(err) {
		t.Fatal("termination left the protected stage")
	}
	if _, err := os.Stat(local); !os.IsNotExist(err) {
		t.Fatal("termination left temporary sensitive data")
	}
	raw, err := os.ReadFile(pidFile)
	if err != nil {
		t.Fatal(err)
	}
	pid, err := strconv.Atoi(string(raw))
	if err != nil || pid <= 1 {
		t.Fatal("invalid test hook process")
	}
	if data, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid)); err == nil {
		fields := strings.Fields(string(data))
		if len(fields) < 3 || fields[2] != "Z" {
			t.Fatal("hook survived termination of the outer script")
		}
	}
}

func TestESXiDuplicateFirstbootDoesNotRemoveActiveStage(t *testing.T) {
	d := t.TempDir()
	bin := filepath.Join(d, "bin")
	if err := os.Mkdir(bin, 0700); err != nil {
		t.Fatal(err)
	}
	for name, body := range map[string]string{
		"stat":    "#!/bin/sh\nif [ \"$1\" = -f ]; then echo vmfs6l; elif [ \"$1\" = -c ]; then case \"$3\" in */stage.sh) echo 0:600;; *) echo 0:700;; esac; else exit 1; fi\n",
		"python3": "#!/bin/sh\nexit 7\n",
	} {
		if err := os.WriteFile(filepath.Join(bin, name), []byte(body), 0700); err != nil {
			t.Fatal(err)
		}
	}
	started, release := filepath.Join(d, "started"), filepath.Join(d, "release")
	local, stageDir := filepath.Join(d, "local"), filepath.Join(d, "stage")
	if err := os.Mkdir(stageDir, 0700); err != nil {
		t.Fatal(err)
	}
	r := esxiAnswerFixture()
	r.Snapshot.Template.Body = "vmaccepteula\n%firstboot --interpreter=busybox\nset -eu\n{{lober:firstboot}}\n{{slot:运行@firstboot}}\n{{lober:cleanup}}\n"
	r.Snapshot.Slots = []model.SnapshotSlot{{Seq: 3, Name: "运行", Exec: true, Timing: "firstboot", Instances: []model.SnapshotInstance{runtimeInstance("printf started > \"$QA_STARTED\"\nwhile [ ! -f \"$QA_RELEASE\" ]; do sleep 0.1; done\n", true, "sh", 10)}}}
	answer, problems, err := Render(r)
	if err != nil || len(problems) != 0 {
		t.Fatalf("answer: %v %v", problems, err)
	}
	r.Kind = catalog.FileESXiStage
	stage, problems, err := Render(r)
	if err != nil || len(problems) != 0 {
		t.Fatalf("stage: %v %v", problems, err)
	}
	_, native, ok := strings.Cut(string(answer), "%firstboot --interpreter=busybox\n")
	if !ok {
		t.Fatal("missing native firstboot")
	}
	_, originalDir := esxiStagePaths(r.Values["task"])
	oldDigest := fmt.Sprintf("%x", sha256.Sum256(stage))
	stage = []byte(relocateESXiCommands(t, string(stage), local, originalDir, stageDir, "", ""))
	stage = []byte(strings.ReplaceAll(string(stage), "stat -", shQuote(filepath.Join(bin, "stat"))+" -"))
	newDigest := fmt.Sprintf("%x", sha256.Sum256(stage))
	stagePath := filepath.Join(stageDir, "stage.sh")
	if err := os.WriteFile(stagePath, stage, 0600); err != nil {
		t.Fatal(err)
	}
	native = relocateESXiCommands(t, native, local, originalDir, stageDir, oldDigest, newDigest)
	native = strings.ReplaceAll(native, "stat -", shQuote(filepath.Join(bin, "stat"))+" -")
	path := filepath.Join(d, "native.sh")
	if err := os.WriteFile(path, []byte(native), 0600); err != nil {
		t.Fatal(err)
	}
	busybox, err := exec.LookPath("busybox")
	if err != nil {
		t.Fatal(err)
	}
	env := append(os.Environ(), "PATH="+bin+":"+os.Getenv("PATH"), "QA_STARTED="+started, "QA_RELEASE="+release)
	first := exec.Command(busybox, "sh", path)
	first.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	first.Env, first.Stdout, first.Stderr = env, io.Discard, io.Discard
	if err := first.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if first.ProcessState == nil {
			_ = os.WriteFile(release, nil, 0600)
			_ = syscall.Kill(-first.Process.Pid, syscall.SIGKILL)
			_, _ = first.Process.Wait()
		}
	}()
	deadline := time.Now().Add(8 * time.Second)
	for {
		if _, err := os.Stat(started); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("first hook did not start")
		}
		time.Sleep(20 * time.Millisecond)
	}
	second := exec.Command(busybox, "sh", path)
	second.Env = env
	output, err := second.CombinedOutput()
	exit, ok := err.(*exec.ExitError)
	if !ok || exit.ExitCode() != 66 {
		t.Fatalf("duplicate firstboot not rejected: %v %s", err, output)
	}
	if _, err := os.Stat(stagePath); err != nil {
		t.Fatal("duplicate firstboot removed the active stage")
	}
	if err := os.WriteFile(release, nil, 0600); err != nil {
		t.Fatal(err)
	}
	if err := first.Wait(); err != nil {
		t.Fatalf("first task failed after duplicate attempt: %v", err)
	}
	if _, err := os.Stat(stagePath); !os.IsNotExist(err) {
		t.Fatal("completed firstboot left the stage")
	}
}
