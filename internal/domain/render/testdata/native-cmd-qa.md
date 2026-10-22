# Native Windows CMD validation from Ubuntu

`native-cmd-qa.go.txt` is a separate target-runtime harness, not a platform-excluded package test or a Linux test dependency. It compiles against the actual current render source and handwritten snapshots; it does not copy or stub the renderer. The module guard continues requiring all permanent package Go files to participate in normal builds.

Run from Ubuntu with Linux Go (workspace path shown for the Task6 clone):

```bash
source /mnt/e/src/lober/.tmp/l3-env-ubuntu.sh
cd /mnt/e/src/lober/.tmp/wf/r6
test ! -e internal/render/answer_cmd_windows_test.go
cp internal/render/testdata/native-cmd-qa.go.txt internal/render/answer_cmd_windows_test.go
trap 'rm -f /mnt/e/src/lober/.tmp/wf/r6/internal/render/answer_cmd_windows_test.go' EXIT
GOOS=windows GOARCH=amd64 go test -c -o /mnt/e/src/lober/.tmp/task6-native-cmd.test.exe ./internal/render
cd internal/render
/mnt/e/src/lober/.tmp/task6-native-cmd.test.exe -test.run TestAnswerWindowsNativeCMD -test.v
```

After the shell exits, the temporary build-only test source is removed. The executable resides on shared /mnt/e for WSL native interop. Normal Linux go test/make check does not compile or run this harness and requires no Windows binaries.

The harness uses Windows `SysProcAttr.CmdLine` to pass the renderer's XML-decoded CommandLine exactly to native CMD, bypassing Go's argument quoting. It stages both actual generated UTF8-BOM PowerShell bodies and a controlled `.cmd` wrapper in a private temporary SystemDrive/ProgramData/lober tree. The SystemDrive environment value is normalized to forward slashes, so no backslash in an expanded executable path can mask the finding. WINDIR is redirected to the same private tree before actual cleanup runs; no user's Windows files are touched. Telemetry uses loopback port 1 refusal and must remain nonfatal.

Assertions: actual wrapper exit 71 propagates and preserves the finalize token; exit 0 allows actual cleanup to remove it; the wrapper marker proves it ran. WinPE's actual XML-decoded Path also invokes a wrapper returning 71, with only unavailable X:/lober mapped to a private forward-slash temporary directory. This validates host native CMD grammar, not actual WinPE boot/image availability, which remains layer7 machine acceptance.

Before any renderer changes, the exact `.cmd` cases passed. A `.exe` analogue reporting invalid forward-slash syntax does not establish failure of the generated `.cmd` path. No cwd/basename redesign was applied on the basis of that analogue. Keep native payload staging, the one sequential chain, 1,024-character limit and independent finalize credential contracts unchanged.
