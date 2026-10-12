# Exact legacy line differences

Each deleted or inserted line is listed with its original one-based line number. Reasons below apply explicitly to the indicated line ranges; no changed line is omitted. Equal lines are omitted. Encoded inserted commands are decoded in `decoded-scaffolds.md` and shell-checked by tests. Old implementation source was not read.

## kickstart-full-bios.golden

Compared with `kickstart_full.golden`; firmware was not split in that old reference.

### Old 1–0; new 1–2

B3: cmdline and explicit url installation source fix 9a252db/743e8af; %include follows %pre (2d59bbb), fixture writes real firmware-specific partition contents to the include file before use. BIOS GPT adds biosboot, UEFI uses ESP (83584d7). Native %pre/%post erroronfail + set -eu preserve failure; %post --nochroot matches /mnt/sysroot staging. Added curl dependency and reboot are fixture-owned syntax.

S5: all remaining profile/blank/layout/value differences in this range are directly authored in the committed .tmpl/.fragment snapshots: hostname remains web-01; identity is task-42 (valid model ID) rather than old numeric 42; new tokens shorter but valid; full includes one real script per supported timing, minimal keeps the slot empty (still receives runtime wrapper for stage signals); SSH keys/legacy host metadata omitted from independent assembly fixtures because Task9 owns actual builtin defaults and SSH hook. No implicit old-model field assembly remains.

```diff
+0001 cmdline
+0002 url --url=http://ng.internal/artifacts/fixture/repo/
```

### Old 5–7; new 7–7

B3: cmdline and explicit url installation source fix 9a252db/743e8af; %include follows %pre (2d59bbb), fixture writes real firmware-specific partition contents to the include file before use. BIOS GPT adds biosboot, UEFI uses ESP (83584d7). Native %pre/%post erroronfail + set -eu preserve failure; %post --nochroot matches /mnt/sysroot staging. Added curl dependency and reboot are fixture-owned syntax.

S5: all remaining profile/blank/layout/value differences in this range are directly authored in the committed .tmpl/.fragment snapshots: hostname remains web-01; identity is task-42 (valid model ID) rather than old numeric 42; new tokens shorter but valid; full includes one real script per supported timing, minimal keeps the slot empty (still receives runtime wrapper for stage signals); SSH keys/legacy host metadata omitted from independent assembly fixtures because Task9 owns actual builtin defaults and SSH hook. No implicit old-model field assembly remains.

```diff
-0005 clearpart --all --drives=/dev/disk/by-id/wwn-0x5000c500fixed0001
-0006 part / --fstype=xfs --grow --ondisk=/dev/disk/by-id/wwn-0x5000c500fixed0001
-0007 network --bootproto=dhcp --activate
+0007 network --bootproto=dhcp --activate --hostname=web-01
```

### Old 9–9; new 9–8

B3: cmdline and explicit url installation source fix 9a252db/743e8af; %include follows %pre (2d59bbb), fixture writes real firmware-specific partition contents to the include file before use. BIOS GPT adds biosboot, UEFI uses ESP (83584d7). Native %pre/%post erroronfail + set -eu preserve failure; %post --nochroot matches /mnt/sysroot staging. Added curl dependency and reboot are fixture-owned syntax.

S5: all remaining profile/blank/layout/value differences in this range are directly authored in the committed .tmpl/.fragment snapshots: hostname remains web-01; identity is task-42 (valid model ID) rather than old numeric 42; new tokens shorter but valid; full includes one real script per supported timing, minimal keeps the slot empty (still receives runtime wrapper for stage signals); SSH keys/legacy host metadata omitted from independent assembly fixtures because Task9 owns actual builtin defaults and SSH hook. No implicit old-model field assembly remains.

```diff
-0009 sshkey --username=root "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIGZ5ZDhhOWMwZTNiNGY1YzZkN2E4YjljMGUzYjRmNWM2 ops@lober"
```

### Old 13–12; new 12–12

B3: cmdline and explicit url installation source fix 9a252db/743e8af; %include follows %pre (2d59bbb), fixture writes real firmware-specific partition contents to the include file before use. BIOS GPT adds biosboot, UEFI uses ESP (83584d7). Native %pre/%post erroronfail + set -eu preserve failure; %post --nochroot matches /mnt/sysroot staging. Added curl dependency and reboot are fixture-owned syntax.

S5: all remaining profile/blank/layout/value differences in this range are directly authored in the committed .tmpl/.fragment snapshots: hostname remains web-01; identity is task-42 (valid model ID) rather than old numeric 42; new tokens shorter but valid; full includes one real script per supported timing, minimal keeps the slot empty (still receives runtime wrapper for stage signals); SSH keys/legacy host metadata omitted from independent assembly fixtures because Task9 owns actual builtin defaults and SSH hook. No implicit old-model field assembly remains.

```diff
+0012 curl
```

### Old 14–71; new 14–22

S1: fixed global environment and inline signal/run_hook library are removed; flattened snapshot instances and FileScript now own per-instance EnvName/EnvValue, digest/interpreter/timeout/required execution. Task6 inserts one-line wrapper downloads, never copies old inline runtime. Legacy fake digest strings become real SHA-256 fixture digest. Task8 implements/verifies runtime bodies.

B3: cmdline and explicit url installation source fix 9a252db/743e8af; %include follows %pre (2d59bbb), fixture writes real firmware-specific partition contents to the include file before use. BIOS GPT adds biosboot, UEFI uses ESP (83584d7). Native %pre/%post erroronfail + set -eu preserve failure; %post --nochroot matches /mnt/sysroot staging. Added curl dependency and reboot are fixture-owned syntax.

S5: all remaining profile/blank/layout/value differences in this range are directly authored in the committed .tmpl/.fragment snapshots: hostname remains web-01; identity is task-42 (valid model ID) rather than old numeric 42; new tokens shorter but valid; full includes one real script per supported timing, minimal keeps the slot empty (still receives runtime wrapper for stage signals); SSH keys/legacy host metadata omitted from independent assembly fixtures because Task9 owns actual builtin defaults and SSH hook. No implicit old-model field assembly remains.

```diff
-0014 
-0015 %pre
-0016 LOBER_CONTRACT_VERSION=''
-0017 LOBER_TASK_ID='42'
-0018 LOBER_SN='SN-FIXED-0001'
-0019 LOBER_MAC=''
-0020 LOBER_UUID=''
-0021 LOBER_HOSTNAME='web-01'
-0022 LOBER_OS_FAMILY='rhel'
-0023 LOBER_OS_VERSION=''
-0024 LOBER_ARCH=''
-0025 LOBER_DISKS=''
-0026 LOBER_DISK_PRIMARY='/dev/disk/by-id/wwn-0x5000c500fixed0001'
-0027 LOBER_NICS=''
-0028 LOBER_RAM_MB=''
-0029 LOBER_CPU_CORES=''
-0030 LOBER_VENDOR=''
-0031 LOBER_PRODUCT=''
-0032 LOBER_BIZ_IP='10.20.0.15'
-0033 LOBER_BIZ_GW=''
-0034 LOBER_BIZ_MASK=''
-0035 LOBER_ARTIFACT_BASE='http://ng.internal/artifacts'
-0036 LOBER_GATEWAY_BASE='http://gw.internal'
-0037 LOBER_RACK=''
-0038 LOBER_ROW=''
-0039 LOBER_DC=''
-0040 # 用户 hook 从这里开始
-0041 GW='http://gw.internal'
-0042 ART='http://ng.internal/artifacts'
-0043 TASK='42'
-0044 TOKEN='install-tok-fixed-0000000000000000'
-0045 SEQ=0
-0046 
-0047 signal() {
-0048   local phase="$1" rc="${2:-0}" log="${3:-/dev/null}" digest="${4:-}"
-0049   SEQ=$((SEQ+1))
-0050   local q="$GW/v1/signal/$TASK/$TOKEN/$phase?rc=$rc&seq=$SEQ"
-0051   if [ -n "$digest" ]; then
-0052     q="$q&digest=$digest"
-0053   fi
-0054   for i in 1 2 3; do
-0055     curl -fsS -m 10 -X POST "$q" \
-0056       -H 'Content-Type: text/plain' --data-binary "@$log" && return 0
-0057     sleep $((i*3))
-0058   done
-0059   logger -t lober "signal failed: $phase rc=$rc"
-0060   return 0
-0061 }
-0062 
-0063 run_hook() {
-0064   local digest="$1" required="$2" timeout_sec="$3"
-0065   curl -fsS -o /tmp/h.sh "$ART/hooks/$digest/script.sh" || return 1
-0066   echo "$digest  /tmp/h.sh" | sha256sum -c - || return 1
-0067   ( set -euo pipefail; timeout "$timeout_sec" bash /tmp/h.sh ) 2>&1 | tee /tmp/h.log
-0068   local rc=${PIPESTATUS[0]}
-0069   signal "hook-report" "$rc" /tmp/h.log "$digest"
-0070   [ "$rc" -eq 0 ] || [ "$required" != "true" ]
-0071 }
+0014 %pre --erroronfail
+0015 set -eu
+0016 echo c2V0IC1ldQp1bWFzayAwNzcKbWtkaXIgLXAgJy90bXAnCmN1cmwgLWZzUyAtLXJldHJ5IDMgLW8gJy90bXAvc2NyaXB0LTIuc2gnICdodHRwOi8vZ3cuaW50ZXJuYWwvdjEvYW5zd2VyL3Rhc2stNDIvaW5zdGFsbC10b2stZml4ZWQvc2NyaXB0cy8yLnNoJwpzaCAnL3RtcC9zY3JpcHQtMi5zaCcK | base64 -d | sh
+0017 cat > /tmp/lober-partition.ks <<'LOBER_PARTITION_EOF'
+0018 zerombr
+0019 clearpart --all --initlabel --disklabel=gpt --drives=sda
+0020 part biosboot --fstype=biosboot --size=1 --ondisk=sda
+0021 part / --fstype=xfs --grow --ondisk=sda
+0022 LOBER_PARTITION_EOF
```

### Old 73–105; new 24–31

S1: fixed global environment and inline signal/run_hook library are removed; flattened snapshot instances and FileScript now own per-instance EnvName/EnvValue, digest/interpreter/timeout/required execution. Task6 inserts one-line wrapper downloads, never copies old inline runtime. Legacy fake digest strings become real SHA-256 fixture digest. Task8 implements/verifies runtime bodies.

B3: cmdline and explicit url installation source fix 9a252db/743e8af; %include follows %pre (2d59bbb), fixture writes real firmware-specific partition contents to the include file before use. BIOS GPT adds biosboot, UEFI uses ESP (83584d7). Native %pre/%post erroronfail + set -eu preserve failure; %post --nochroot matches /mnt/sysroot staging. Added curl dependency and reboot are fixture-owned syntax.

S5: all remaining profile/blank/layout/value differences in this range are directly authored in the committed .tmpl/.fragment snapshots: hostname remains web-01; identity is task-42 (valid model ID) rather than old numeric 42; new tokens shorter but valid; full includes one real script per supported timing, minimal keeps the slot empty (still receives runtime wrapper for stage signals); SSH keys/legacy host metadata omitted from independent assembly fixtures because Task9 owns actual builtin defaults and SSH hook. No implicit old-model field assembly remains.

```diff
-0073 %pre
-0074 GW='http://gw.internal'
-0075 ART='http://ng.internal/artifacts'
-0076 TASK='42'
-0077 TOKEN='install-tok-fixed-0000000000000000'
-0078 SEQ=0
-0079 
-0080 signal() {
-0081   local phase="$1" rc="${2:-0}" log="${3:-/dev/null}" digest="${4:-}"
-0082   SEQ=$((SEQ+1))
-0083   local q="$GW/v1/signal/$TASK/$TOKEN/$phase?rc=$rc&seq=$SEQ"
-0084   if [ -n "$digest" ]; then
-0085     q="$q&digest=$digest"
-0086   fi
-0087   for i in 1 2 3; do
-0088     curl -fsS -m 10 -X POST "$q" \
-0089       -H 'Content-Type: text/plain' --data-binary "@$log" && return 0
-0090     sleep $((i*3))
-0091   done
-0092   logger -t lober "signal failed: $phase rc=$rc"
-0093   return 0
-0094 }
-0095 
-0096 run_hook() {
-0097   local digest="$1" required="$2" timeout_sec="$3"
-0098   curl -fsS -o /tmp/h.sh "$ART/hooks/$digest/script.sh" || return 1
-0099   echo "$digest  /tmp/h.sh" | sha256sum -c - || return 1
-0100   ( set -euo pipefail; timeout "$timeout_sec" bash /tmp/h.sh ) 2>&1 | tee /tmp/h.log
-0101   local rc=${PIPESTATUS[0]}
-0102   signal "hook-report" "$rc" /tmp/h.log "$digest"
-0103   [ "$rc" -eq 0 ] || [ "$required" != "true" ]
-0104 }
-0105 run_hook 'aaaa1111pre' 'true' '300' || exit 1
+0024 %include /tmp/lober-partition.ks
+0025 %post --nochroot --erroronfail
+0026 set -eu
+0027 echo c2V0IC1ldQp1bWFzayAwNzcKbWtkaXIgLXAgJy90bXAnCmN1cmwgLWZzUyAtLXJldHJ5IDMgLW8gJy90bXAvc2NyaXB0LTMuc2gnICdodHRwOi8vZ3cuaW50ZXJuYWwvdjEvYW5zd2VyL3Rhc2stNDIvaW5zdGFsbC10b2stZml4ZWQvc2NyaXB0cy8zLnNoJwpzaCAnL3RtcC9zY3JpcHQtMy5zaCcK | base64 -d | sh
+0028 echo c2V0IC1ldQp1bWFzayAwNzcKbWtkaXIgLXAgJy9tbnQvc3lzcm9vdC92YXIvbGliL2xvYmVyJwpjdXJsIC1mc1MgLS1yZXRyeSAzIC1vICcvbW50L3N5c3Jvb3QvdmFyL2xpYi9sb2Jlci9zY3JpcHQtNC5zaCcgJ2h0dHA6Ly9ndy5pbnRlcm5hbC92MS9hbnN3ZXIvdGFzay00Mi9pbnN0YWxsLXRvay1maXhlZC9zY3JpcHRzLzQuc2gnCmNocm9vdCAnL21udC9zeXNyb290JyBzaCAnL3Zhci9saWIvbG9iZXIvc2NyaXB0LTQuc2gnCg== | base64 -d | sh
+0029 echo c2V0IC1ldQp1bWFzayAwNzcKbWtkaXIgLXAgJy9tbnQvc3lzcm9vdC92YXIvbGliL2xvYmVyJwpjdXJsIC1mc1MgLS1yZXRyeSAzIC1vICcvbW50L3N5c3Jvb3QvdmFyL2xpYi9sb2Jlci9zY3JpcHQtNS5zaCcgJ2h0dHA6Ly9ndy5pbnRlcm5hbC92MS9hbnN3ZXIvdGFzay00Mi9pbnN0YWxsLXRvay1maXhlZC9zY3JpcHRzLzUuc2gnCmN1cmwgLWZzUyAtLXJldHJ5IDMgLW8gJy9tbnQvc3lzcm9vdC92YXIvbGliL2xvYmVyL2hvb2stM2VlMDQ5ZTU5MWY1ZTk5YWU4ZjQ5OGFlMzgxZjQ1OTM2NWMyMjFlNDFlNmI0MmYyYWQ2ZTlhNmE5NzUwNTMyZS5zaCcgJ2h0dHA6Ly9ndy5pbnRlcm5hbC92MS9hbnN3ZXIvdGFzay00Mi9pbnN0YWxsLXRvay1maXhlZC9ob29rcy8zZWUwNDllNTkxZjVlOTlhZThmNDk4YWUzODFmNDU5MzY1YzIyMWU0MWU2YjQyZjJhZDZlOWE2YTk3NTA1MzJlJwo= | base64 -d | sh
+0030 echo c2V0IC1ldQp1bWFzayAwNzcKbWtkaXIgLXAgJy9tbnQvc3lzcm9vdC92YXIvbGliL2xvYmVyJyAnL21udC9zeXNyb290L2V0Yy9zeXN0ZW1kL3N5c3RlbS9tdWx0aS11c2VyLnRhcmdldC53YW50cycKcHJpbnRmICclcycgJ2ZpbmFsaXplLXRvay1maXhlZCcgPiAnL21udC9zeXNyb290L3Zhci9saWIvbG9iZXIvZmluYWxpemUudG9rZW4nCmNhdCA+ICcvbW50L3N5c3Jvb3QvdmFyL2xpYi9sb2Jlci9maXJzdGJvb3Quc2gnIDw8J0xPQkVSX0ZJUlNUQk9PVF9FT0YnCiMhL2Jpbi9zaApzZXQgLWV1ClRPS0VOPSQoY2F0IC92YXIvbGliL2xvYmVyL2ZpbmFsaXplLnRva2VuKQpjdXJsIC1mc1MgLW0gMTAgLVggUE9TVCAnaHR0cDovL2d3LmludGVybmFsL3YxL3NpZ25hbC90YXNrLTQyLyciJFRPS0VOIicvYm9vdGVkJyAtLWRhdGEtYmluYXJ5ICcnID4vZGV2L251bGwgMj4mMSB8fCA6ClRPS0VOPSQoY2F0IC92YXIvbGliL2xvYmVyL2ZpbmFsaXplLnRva2VuKQpjdXJsIC1mc1MgLW0gMTAgLVggUE9TVCAnaHR0cDovL2d3LmludGVybmFsL3YxL3NpZ25hbC90YXNrLTQyLyciJFRPS0VOIicvaW5pdC1zdGFydCcgLS1kYXRhLWJpbmFyeSAnJyA+L2Rldi9udWxsIDI+JjEgfHwgOgpzaCAvdmFyL2xpYi9sb2Jlci9zY3JpcHQtNS5zaApzaCAvdmFyL2xpYi9sb2Jlci9jbGVhbnVwLnNoCkxPQkVSX0ZJUlNUQk9PVF9FT0YKY2F0ID4gJy9tbnQvc3lzcm9vdC9ldGMvc3lzdGVtZC9zeXN0ZW0vbG9iZXItZmlyc3Rib290LnNlcnZpY2UnIDw8J0xPQkVSX1VOSVRfRU9GJwpbVW5pdF0KRGVzY3JpcHRpb249TG9iZXIgZmlyc3QgYm9vdApBZnRlcj1uZXR3b3JrLW9ubGluZS50YXJnZXQKV2FudHM9bmV0d29yay1vbmxpbmUudGFyZ2V0CkNvbmRpdGlvblBhdGhFeGlzdHM9L3Zhci9saWIvbG9iZXIvZmlyc3Rib290LnBlbmRpbmcKW1NlcnZpY2VdClR5cGU9b25lc2hvdApFeGVjU3RhcnQ9L2Jpbi9zaCAvdmFyL2xpYi9sb2Jlci9maXJzdGJvb3Quc2gKW0luc3RhbGxdCldhbnRlZEJ5PW11bHRpLXVzZXIudGFyZ2V0CkxPQkVSX1VOSVRfRU9GCmxuIC1zZiAuLi9sb2Jlci1maXJzdGJvb3Quc2VydmljZSAnL21udC9zeXNyb290L2V0Yy9zeXN0ZW1kL3N5c3RlbS9tdWx0aS11c2VyLnRhcmdldC53YW50cy9sb2Jlci1maXJzdGJvb3Quc2VydmljZScKdG91Y2ggJy9tbnQvc3lzcm9vdC92YXIvbGliL2xvYmVyL2ZpcnN0Ym9vdC5wZW5kaW5nJwo= | base64 -d | sh
+0031 echo c2V0IC1ldQp1bWFzayAwNzcKbWtkaXIgLXAgJy9tbnQvc3lzcm9vdC92YXIvbGliL2xvYmVyJwpjYXQgPiAnL21udC9zeXNyb290L3Zhci9saWIvbG9iZXIvY2xlYW51cC5zaCcgPDwnTE9CRVJfQ0xFQU5VUF9FT0YnCiMhL2Jpbi9zaApzZXQgLWV1ClRPS0VOPSQoY2F0IC92YXIvbGliL2xvYmVyL2ZpbmFsaXplLnRva2VuKQpjdXJsIC1mc1MgLW0gMTAgLVggUE9TVCAnaHR0cDovL2d3LmludGVybmFsL3YxL3NpZ25hbC90YXNrLTQyLyciJFRPS0VOIicvaW5pdC1kb25lJyAtLWRhdGEtYmluYXJ5ICcnID4vZGV2L251bGwgMj4mMSB8fCA6ClRPS0VOPSQoY2F0IC92YXIvbGliL2xvYmVyL2ZpbmFsaXplLnRva2VuKQpjdXJsIC1mc1MgLW0gMTAgLVggUE9TVCAnaHR0cDovL2d3LmludGVybmFsL3YxL3NpZ25hbC90YXNrLTQyLyciJFRPS0VOIicvcmVhZHknIC0tZGF0YS1iaW5hcnkgJycgPi9kZXYvbnVsbCAyPiYxIHx8IDoKcm0gLWYgL3Zhci9saWIvbG9iZXIvZmluYWxpemUudG9rZW4gL3Zhci9saWIvbG9iZXIvZmlyc3Rib290LnBlbmRpbmcgL3Zhci9saWIvbG9iZXIvc2NyaXB0LSouc2ggL3Zhci9saWIvbG9iZXIvaG9vay0qIC92YXIvbGliL2xvYmVyL2ZpcnN0Ym9vdC5zaCAvdmFyL2xpYi9sb2Jlci9jbGVhbnVwLnNoCnJtIC1mIC9yb290L2FuYWNvbmRhLWtzLmNmZyAvcm9vdC9vcmlnaW5hbC1rcy5jZmcgL3Zhci9sb2cvaW5zdGFsbGVyL2F1dG9pbnN0YWxsLXVzZXItZGF0YSAvdmFyL2xvZy9pbnN0YWxsZXIvYXV0b2luc3RhbGwtdXNlci1kYXRhLnR4dCAvdmFyL2xvZy9pbnN0YWxsZXIvY2RlYmNvbmYvcXVlc3Rpb25zLmRhdCAvdmFyL2FkbS9hdXRvaW5zdGFsbC9jYWNoZS9pbnN0YWxsZWRTeXN0ZW0ueG1sCkxPQkVSX0NMRUFOVVBfRU9GCg== | base64 -d | sh
```

### Old 107–284; new 33–33

S1: fixed global environment and inline signal/run_hook library are removed; flattened snapshot instances and FileScript now own per-instance EnvName/EnvValue, digest/interpreter/timeout/required execution. Task6 inserts one-line wrapper downloads, never copies old inline runtime. Legacy fake digest strings become real SHA-256 fixture digest. Task8 implements/verifies runtime bodies.

B1: firstboot now stages local slot wrappers and local digest payloads before reboot; systemd invokes the installed native runner. Boot credential no longer enters firstboot; independent local finalize token used. Cleanup follows success and erases cached answers/payloads. Legacy firstboot reused install TOKEN despite writing finalize.token. Unit/marker/local paths change to this explicit contract.

B3: cmdline and explicit url installation source fix 9a252db/743e8af; %include follows %pre (2d59bbb), fixture writes real firmware-specific partition contents to the include file before use. BIOS GPT adds biosboot, UEFI uses ESP (83584d7). Native %pre/%post erroronfail + set -eu preserve failure; %post --nochroot matches /mnt/sysroot staging. Added curl dependency and reboot are fixture-owned syntax.

S5: all remaining profile/blank/layout/value differences in this range are directly authored in the committed .tmpl/.fragment snapshots: hostname remains web-01; identity is task-42 (valid model ID) rather than old numeric 42; new tokens shorter but valid; full includes one real script per supported timing, minimal keeps the slot empty (still receives runtime wrapper for stage signals); SSH keys/legacy host metadata omitted from independent assembly fixtures because Task9 owns actual builtin defaults and SSH hook. No implicit old-model field assembly remains.

```diff
-0107 %post --nochroot
-0108 GW='http://gw.internal'
-0109 ART='http://ng.internal/artifacts'
-0110 TASK='42'
-0111 TOKEN='install-tok-fixed-0000000000000000'
-0112 SEQ=0
-0113 
-0114 signal() {
-0115   local phase="$1" rc="${2:-0}" log="${3:-/dev/null}" digest="${4:-}"
-0116   SEQ=$((SEQ+1))
-0117   local q="$GW/v1/signal/$TASK/$TOKEN/$phase?rc=$rc&seq=$SEQ"
-0118   if [ -n "$digest" ]; then
-0119     q="$q&digest=$digest"
-0120   fi
-0121   for i in 1 2 3; do
-0122     curl -fsS -m 10 -X POST "$q" \
-0123       -H 'Content-Type: text/plain' --data-binary "@$log" && return 0
-0124     sleep $((i*3))
-0125   done
-0126   logger -t lober "signal failed: $phase rc=$rc"
-0127   return 0
-0128 }
-0129 
-0130 run_hook() {
-0131   local digest="$1" required="$2" timeout_sec="$3"
-0132   curl -fsS -o /tmp/h.sh "$ART/hooks/$digest/script.sh" || return 1
-0133   echo "$digest  /tmp/h.sh" | sha256sum -c - || return 1
-0134   ( set -euo pipefail; timeout "$timeout_sec" bash /tmp/h.sh ) 2>&1 | tee /tmp/h.log
-0135   local rc=${PIPESTATUS[0]}
-0136   signal "hook-report" "$rc" /tmp/h.log "$digest"
-0137   [ "$rc" -eq 0 ] || [ "$required" != "true" ]
-0138 }
-0139 run_hook 'bbbb2222nochroot' 'false' '300' || exit 1
-0140 %end
-0141 %post
-0142 GW='http://gw.internal'
-0143 ART='http://ng.internal/artifacts'
-0144 TASK='42'
-0145 TOKEN='install-tok-fixed-0000000000000000'
-0146 SEQ=0
-0147 
-0148 signal() {
-0149   local phase="$1" rc="${2:-0}" log="${3:-/dev/null}" digest="${4:-}"
-0150   SEQ=$((SEQ+1))
-0151   local q="$GW/v1/signal/$TASK/$TOKEN/$phase?rc=$rc&seq=$SEQ"
-0152   if [ -n "$digest" ]; then
-0153     q="$q&digest=$digest"
-0154   fi
-0155   for i in 1 2 3; do
-0156     curl -fsS -m 10 -X POST "$q" \
-0157       -H 'Content-Type: text/plain' --data-binary "@$log" && return 0
-0158     sleep $((i*3))
-0159   done
-0160   logger -t lober "signal failed: $phase rc=$rc"
-0161   return 0
-0162 }
-0163 
-0164 run_hook() {
-0165   local digest="$1" required="$2" timeout_sec="$3"
-0166   curl -fsS -o /tmp/h.sh "$ART/hooks/$digest/script.sh" || return 1
-0167   echo "$digest  /tmp/h.sh" | sha256sum -c - || return 1
-0168   ( set -euo pipefail; timeout "$timeout_sec" bash /tmp/h.sh ) 2>&1 | tee /tmp/h.log
-0169   local rc=${PIPESTATUS[0]}
-0170   signal "hook-report" "$rc" /tmp/h.log "$digest"
-0171   [ "$rc" -eq 0 ] || [ "$required" != "true" ]
-0172 }
-0173 run_hook 'cccc3333post' 'true' '300' || exit 1
-0174 %end
-0175 %post
-0176 mkdir -p /var/lib/lober /var/lib/lober/hooks
-0177 
-0178 install -m 600 /dev/null /var/lib/lober/finalize.token
-0179 echo 'finalize-tok-fixed-0000000000000000' > /var/lib/lober/finalize.token
-0180 
-0181 install -m 600 /dev/null /var/lib/lober/params.env
-0182 cat > /var/lib/lober/params.env <<'LOBER_PARAMS_EOF'
-0183 LOBER_CONTRACT_VERSION=''
-0184 LOBER_TASK_ID='42'
-0185 LOBER_SN='SN-FIXED-0001'
-0186 LOBER_MAC=''
-0187 LOBER_UUID=''
-0188 LOBER_HOSTNAME='web-01'
-0189 LOBER_OS_FAMILY='rhel'
-0190 LOBER_OS_VERSION=''
-0191 LOBER_ARCH=''
-0192 LOBER_DISKS=''
-0193 LOBER_DISK_PRIMARY='/dev/disk/by-id/wwn-0x5000c500fixed0001'
-0194 LOBER_NICS=''
-0195 LOBER_RAM_MB=''
-0196 LOBER_CPU_CORES=''
-0197 LOBER_VENDOR=''
-0198 LOBER_PRODUCT=''
-0199 LOBER_BIZ_IP='10.20.0.15'
-0200 LOBER_BIZ_GW=''
-0201 LOBER_BIZ_MASK=''
-0202 LOBER_ARTIFACT_BASE='http://ng.internal/artifacts'
-0203 LOBER_GATEWAY_BASE='http://gw.internal'
-0204 LOBER_RACK=''
-0205 LOBER_ROW=''
-0206 LOBER_DC=''
-0207 LOBER_PARAMS_EOF
-0208 
-0209 cat > /var/lib/lober/hooks/manifest <<'LOBER_MANIFEST_EOF'
-0210 010-dddd4444firstboot.sh digest=dddd4444firstboot required=true order=10
-0211 LOBER_MANIFEST_EOF
-0212 
-0213 curl -fsS -o /var/lib/lober/hooks/010-dddd4444firstboot.sh 'http://ng.internal/artifacts/hooks/dddd4444firstboot/script.sh'
-0214 echo 'dddd4444firstboot  /var/lib/lober/hooks/010-dddd4444firstboot.sh' | sha256sum -c -
-0215 chmod 700 /var/lib/lober/hooks/010-dddd4444firstboot.sh
-0216 
-0217 cat > /usr/local/sbin/lober-init.sh <<'LOBER_INIT_EOF'
-0218 #!/bin/bash
-0219 . /var/lib/lober/params.env
-0220 
-0221 GW='http://gw.internal'
-0222 ART='http://ng.internal/artifacts'
-0223 TASK='42'
-0224 TOKEN='install-tok-fixed-0000000000000000'
-0225 SEQ=0
-0226 
-0227 signal() {
-0228   local phase="$1" rc="${2:-0}" log="${3:-/dev/null}" digest="${4:-}"
-0229   SEQ=$((SEQ+1))
-0230   local q="$GW/v1/signal/$TASK/$TOKEN/$phase?rc=$rc&seq=$SEQ"
-0231   if [ -n "$digest" ]; then
-0232     q="$q&digest=$digest"
-0233   fi
-0234   for i in 1 2 3; do
-0235     curl -fsS -m 10 -X POST "$q" \
-0236       -H 'Content-Type: text/plain' --data-binary "@$log" && return 0
-0237     sleep $((i*3))
-0238   done
-0239   logger -t lober "signal failed: $phase rc=$rc"
-0240   return 0
-0241 }
-0242 
-0243 run_hook() {
-0244   local digest="$1" required="$2" timeout_sec="$3"
-0245   curl -fsS -o /tmp/h.sh "$ART/hooks/$digest/script.sh" || return 1
-0246   echo "$digest  /tmp/h.sh" | sha256sum -c - || return 1
-0247   ( set -euo pipefail; timeout "$timeout_sec" bash /tmp/h.sh ) 2>&1 | tee /tmp/h.log
-0248   local rc=${PIPESTATUS[0]}
-0249   signal "hook-report" "$rc" /tmp/h.log "$digest"
-0250   [ "$rc" -eq 0 ] || [ "$required" != "true" ]
-0251 }
-0252 
-0253 signal "booted" 0
-0254 signal "init-start" 0
-0255 
-0256 bash /var/lib/lober/hooks/010-dddd4444firstboot.sh > /tmp/fb-dddd4444firstboot.log 2>&1
-0257 signal "hook-report" "$?" /tmp/fb-dddd4444firstboot.log "dddd4444firstboot"
-0258 
-0259 signal "init-done" 0
-0260 signal "ready" 0
-0261 
-0262 shred -u /var/lib/lober/params.env /var/lib/lober/finalize.token
-0263 rm -f /var/lib/lober/init.pending
-0264 sed -i '/rootpw\|--iscrypted\|inst.ks=/d' /root/anaconda-ks.cfg 2>/dev/null
-0265 LOBER_INIT_EOF
-0266 chmod 755 /usr/local/sbin/lober-init.sh
-0267 
-0268 cat > /etc/systemd/system/lober-init.service <<'LOBER_UNIT_EOF'
-0269 [Unit]
-0270 Description=Lober OS init
-0271 After=network-online.target
-0272 Wants=network-online.target
-0273 ConditionPathExists=/var/lib/lober/init.pending
-0274 [Service]
-0275 Type=oneshot
-0276 ExecStart=/usr/local/sbin/lober-init.sh
-0277 StandardOutput=journal+console
-0278 [Install]
-0279 WantedBy=multi-user.target
-0280 LOBER_UNIT_EOF
-0281 
-0282 systemctl enable lober-init.service
-0283 touch /var/lib/lober/init.pending
-0284 %end
+0033 reboot
```

## kickstart-full-uefi.golden

Compared with `kickstart_full.golden`; firmware was not split in that old reference.

### Old 1–0; new 1–2

B3: cmdline and explicit url installation source fix 9a252db/743e8af; %include follows %pre (2d59bbb), fixture writes real firmware-specific partition contents to the include file before use. BIOS GPT adds biosboot, UEFI uses ESP (83584d7). Native %pre/%post erroronfail + set -eu preserve failure; %post --nochroot matches /mnt/sysroot staging. Added curl dependency and reboot are fixture-owned syntax.

S5: all remaining profile/blank/layout/value differences in this range are directly authored in the committed .tmpl/.fragment snapshots: hostname remains web-01; identity is task-42 (valid model ID) rather than old numeric 42; new tokens shorter but valid; full includes one real script per supported timing, minimal keeps the slot empty (still receives runtime wrapper for stage signals); SSH keys/legacy host metadata omitted from independent assembly fixtures because Task9 owns actual builtin defaults and SSH hook. No implicit old-model field assembly remains.

```diff
+0001 cmdline
+0002 url --url=http://ng.internal/artifacts/fixture/repo/
```

### Old 5–7; new 7–7

B3: cmdline and explicit url installation source fix 9a252db/743e8af; %include follows %pre (2d59bbb), fixture writes real firmware-specific partition contents to the include file before use. BIOS GPT adds biosboot, UEFI uses ESP (83584d7). Native %pre/%post erroronfail + set -eu preserve failure; %post --nochroot matches /mnt/sysroot staging. Added curl dependency and reboot are fixture-owned syntax.

S5: all remaining profile/blank/layout/value differences in this range are directly authored in the committed .tmpl/.fragment snapshots: hostname remains web-01; identity is task-42 (valid model ID) rather than old numeric 42; new tokens shorter but valid; full includes one real script per supported timing, minimal keeps the slot empty (still receives runtime wrapper for stage signals); SSH keys/legacy host metadata omitted from independent assembly fixtures because Task9 owns actual builtin defaults and SSH hook. No implicit old-model field assembly remains.

```diff
-0005 clearpart --all --drives=/dev/disk/by-id/wwn-0x5000c500fixed0001
-0006 part / --fstype=xfs --grow --ondisk=/dev/disk/by-id/wwn-0x5000c500fixed0001
-0007 network --bootproto=dhcp --activate
+0007 network --bootproto=dhcp --activate --hostname=web-01
```

### Old 9–9; new 9–8

B3: cmdline and explicit url installation source fix 9a252db/743e8af; %include follows %pre (2d59bbb), fixture writes real firmware-specific partition contents to the include file before use. BIOS GPT adds biosboot, UEFI uses ESP (83584d7). Native %pre/%post erroronfail + set -eu preserve failure; %post --nochroot matches /mnt/sysroot staging. Added curl dependency and reboot are fixture-owned syntax.

S5: all remaining profile/blank/layout/value differences in this range are directly authored in the committed .tmpl/.fragment snapshots: hostname remains web-01; identity is task-42 (valid model ID) rather than old numeric 42; new tokens shorter but valid; full includes one real script per supported timing, minimal keeps the slot empty (still receives runtime wrapper for stage signals); SSH keys/legacy host metadata omitted from independent assembly fixtures because Task9 owns actual builtin defaults and SSH hook. No implicit old-model field assembly remains.

```diff
-0009 sshkey --username=root "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIGZ5ZDhhOWMwZTNiNGY1YzZkN2E4YjljMGUzYjRmNWM2 ops@lober"
```

### Old 13–12; new 12–12

B3: cmdline and explicit url installation source fix 9a252db/743e8af; %include follows %pre (2d59bbb), fixture writes real firmware-specific partition contents to the include file before use. BIOS GPT adds biosboot, UEFI uses ESP (83584d7). Native %pre/%post erroronfail + set -eu preserve failure; %post --nochroot matches /mnt/sysroot staging. Added curl dependency and reboot are fixture-owned syntax.

S5: all remaining profile/blank/layout/value differences in this range are directly authored in the committed .tmpl/.fragment snapshots: hostname remains web-01; identity is task-42 (valid model ID) rather than old numeric 42; new tokens shorter but valid; full includes one real script per supported timing, minimal keeps the slot empty (still receives runtime wrapper for stage signals); SSH keys/legacy host metadata omitted from independent assembly fixtures because Task9 owns actual builtin defaults and SSH hook. No implicit old-model field assembly remains.

```diff
+0012 curl
```

### Old 14–71; new 14–22

S1: fixed global environment and inline signal/run_hook library are removed; flattened snapshot instances and FileScript now own per-instance EnvName/EnvValue, digest/interpreter/timeout/required execution. Task6 inserts one-line wrapper downloads, never copies old inline runtime. Legacy fake digest strings become real SHA-256 fixture digest. Task8 implements/verifies runtime bodies.

B3: cmdline and explicit url installation source fix 9a252db/743e8af; %include follows %pre (2d59bbb), fixture writes real firmware-specific partition contents to the include file before use. BIOS GPT adds biosboot, UEFI uses ESP (83584d7). Native %pre/%post erroronfail + set -eu preserve failure; %post --nochroot matches /mnt/sysroot staging. Added curl dependency and reboot are fixture-owned syntax.

S5: all remaining profile/blank/layout/value differences in this range are directly authored in the committed .tmpl/.fragment snapshots: hostname remains web-01; identity is task-42 (valid model ID) rather than old numeric 42; new tokens shorter but valid; full includes one real script per supported timing, minimal keeps the slot empty (still receives runtime wrapper for stage signals); SSH keys/legacy host metadata omitted from independent assembly fixtures because Task9 owns actual builtin defaults and SSH hook. No implicit old-model field assembly remains.

```diff
-0014 
-0015 %pre
-0016 LOBER_CONTRACT_VERSION=''
-0017 LOBER_TASK_ID='42'
-0018 LOBER_SN='SN-FIXED-0001'
-0019 LOBER_MAC=''
-0020 LOBER_UUID=''
-0021 LOBER_HOSTNAME='web-01'
-0022 LOBER_OS_FAMILY='rhel'
-0023 LOBER_OS_VERSION=''
-0024 LOBER_ARCH=''
-0025 LOBER_DISKS=''
-0026 LOBER_DISK_PRIMARY='/dev/disk/by-id/wwn-0x5000c500fixed0001'
-0027 LOBER_NICS=''
-0028 LOBER_RAM_MB=''
-0029 LOBER_CPU_CORES=''
-0030 LOBER_VENDOR=''
-0031 LOBER_PRODUCT=''
-0032 LOBER_BIZ_IP='10.20.0.15'
-0033 LOBER_BIZ_GW=''
-0034 LOBER_BIZ_MASK=''
-0035 LOBER_ARTIFACT_BASE='http://ng.internal/artifacts'
-0036 LOBER_GATEWAY_BASE='http://gw.internal'
-0037 LOBER_RACK=''
-0038 LOBER_ROW=''
-0039 LOBER_DC=''
-0040 # 用户 hook 从这里开始
-0041 GW='http://gw.internal'
-0042 ART='http://ng.internal/artifacts'
-0043 TASK='42'
-0044 TOKEN='install-tok-fixed-0000000000000000'
-0045 SEQ=0
-0046 
-0047 signal() {
-0048   local phase="$1" rc="${2:-0}" log="${3:-/dev/null}" digest="${4:-}"
-0049   SEQ=$((SEQ+1))
-0050   local q="$GW/v1/signal/$TASK/$TOKEN/$phase?rc=$rc&seq=$SEQ"
-0051   if [ -n "$digest" ]; then
-0052     q="$q&digest=$digest"
-0053   fi
-0054   for i in 1 2 3; do
-0055     curl -fsS -m 10 -X POST "$q" \
-0056       -H 'Content-Type: text/plain' --data-binary "@$log" && return 0
-0057     sleep $((i*3))
-0058   done
-0059   logger -t lober "signal failed: $phase rc=$rc"
-0060   return 0
-0061 }
-0062 
-0063 run_hook() {
-0064   local digest="$1" required="$2" timeout_sec="$3"
-0065   curl -fsS -o /tmp/h.sh "$ART/hooks/$digest/script.sh" || return 1
-0066   echo "$digest  /tmp/h.sh" | sha256sum -c - || return 1
-0067   ( set -euo pipefail; timeout "$timeout_sec" bash /tmp/h.sh ) 2>&1 | tee /tmp/h.log
-0068   local rc=${PIPESTATUS[0]}
-0069   signal "hook-report" "$rc" /tmp/h.log "$digest"
-0070   [ "$rc" -eq 0 ] || [ "$required" != "true" ]
-0071 }
+0014 %pre --erroronfail
+0015 set -eu
+0016 echo c2V0IC1ldQp1bWFzayAwNzcKbWtkaXIgLXAgJy90bXAnCmN1cmwgLWZzUyAtLXJldHJ5IDMgLW8gJy90bXAvc2NyaXB0LTIuc2gnICdodHRwOi8vZ3cuaW50ZXJuYWwvdjEvYW5zd2VyL3Rhc2stNDIvaW5zdGFsbC10b2stZml4ZWQvc2NyaXB0cy8yLnNoJwpzaCAnL3RtcC9zY3JpcHQtMi5zaCcK | base64 -d | sh
+0017 cat > /tmp/lober-partition.ks <<'LOBER_PARTITION_EOF'
+0018 zerombr
+0019 clearpart --all --initlabel --disklabel=gpt --drives=sda
+0020 part /boot/efi --fstype=efi --size=600 --ondisk=sda
+0021 part / --fstype=xfs --grow --ondisk=sda
+0022 LOBER_PARTITION_EOF
```

### Old 73–105; new 24–31

S1: fixed global environment and inline signal/run_hook library are removed; flattened snapshot instances and FileScript now own per-instance EnvName/EnvValue, digest/interpreter/timeout/required execution. Task6 inserts one-line wrapper downloads, never copies old inline runtime. Legacy fake digest strings become real SHA-256 fixture digest. Task8 implements/verifies runtime bodies.

B3: cmdline and explicit url installation source fix 9a252db/743e8af; %include follows %pre (2d59bbb), fixture writes real firmware-specific partition contents to the include file before use. BIOS GPT adds biosboot, UEFI uses ESP (83584d7). Native %pre/%post erroronfail + set -eu preserve failure; %post --nochroot matches /mnt/sysroot staging. Added curl dependency and reboot are fixture-owned syntax.

S5: all remaining profile/blank/layout/value differences in this range are directly authored in the committed .tmpl/.fragment snapshots: hostname remains web-01; identity is task-42 (valid model ID) rather than old numeric 42; new tokens shorter but valid; full includes one real script per supported timing, minimal keeps the slot empty (still receives runtime wrapper for stage signals); SSH keys/legacy host metadata omitted from independent assembly fixtures because Task9 owns actual builtin defaults and SSH hook. No implicit old-model field assembly remains.

```diff
-0073 %pre
-0074 GW='http://gw.internal'
-0075 ART='http://ng.internal/artifacts'
-0076 TASK='42'
-0077 TOKEN='install-tok-fixed-0000000000000000'
-0078 SEQ=0
-0079 
-0080 signal() {
-0081   local phase="$1" rc="${2:-0}" log="${3:-/dev/null}" digest="${4:-}"
-0082   SEQ=$((SEQ+1))
-0083   local q="$GW/v1/signal/$TASK/$TOKEN/$phase?rc=$rc&seq=$SEQ"
-0084   if [ -n "$digest" ]; then
-0085     q="$q&digest=$digest"
-0086   fi
-0087   for i in 1 2 3; do
-0088     curl -fsS -m 10 -X POST "$q" \
-0089       -H 'Content-Type: text/plain' --data-binary "@$log" && return 0
-0090     sleep $((i*3))
-0091   done
-0092   logger -t lober "signal failed: $phase rc=$rc"
-0093   return 0
-0094 }
-0095 
-0096 run_hook() {
-0097   local digest="$1" required="$2" timeout_sec="$3"
-0098   curl -fsS -o /tmp/h.sh "$ART/hooks/$digest/script.sh" || return 1
-0099   echo "$digest  /tmp/h.sh" | sha256sum -c - || return 1
-0100   ( set -euo pipefail; timeout "$timeout_sec" bash /tmp/h.sh ) 2>&1 | tee /tmp/h.log
-0101   local rc=${PIPESTATUS[0]}
-0102   signal "hook-report" "$rc" /tmp/h.log "$digest"
-0103   [ "$rc" -eq 0 ] || [ "$required" != "true" ]
-0104 }
-0105 run_hook 'aaaa1111pre' 'true' '300' || exit 1
+0024 %include /tmp/lober-partition.ks
+0025 %post --nochroot --erroronfail
+0026 set -eu
+0027 echo c2V0IC1ldQp1bWFzayAwNzcKbWtkaXIgLXAgJy90bXAnCmN1cmwgLWZzUyAtLXJldHJ5IDMgLW8gJy90bXAvc2NyaXB0LTMuc2gnICdodHRwOi8vZ3cuaW50ZXJuYWwvdjEvYW5zd2VyL3Rhc2stNDIvaW5zdGFsbC10b2stZml4ZWQvc2NyaXB0cy8zLnNoJwpzaCAnL3RtcC9zY3JpcHQtMy5zaCcK | base64 -d | sh
+0028 echo c2V0IC1ldQp1bWFzayAwNzcKbWtkaXIgLXAgJy9tbnQvc3lzcm9vdC92YXIvbGliL2xvYmVyJwpjdXJsIC1mc1MgLS1yZXRyeSAzIC1vICcvbW50L3N5c3Jvb3QvdmFyL2xpYi9sb2Jlci9zY3JpcHQtNC5zaCcgJ2h0dHA6Ly9ndy5pbnRlcm5hbC92MS9hbnN3ZXIvdGFzay00Mi9pbnN0YWxsLXRvay1maXhlZC9zY3JpcHRzLzQuc2gnCmNocm9vdCAnL21udC9zeXNyb290JyBzaCAnL3Zhci9saWIvbG9iZXIvc2NyaXB0LTQuc2gnCg== | base64 -d | sh
+0029 echo c2V0IC1ldQp1bWFzayAwNzcKbWtkaXIgLXAgJy9tbnQvc3lzcm9vdC92YXIvbGliL2xvYmVyJwpjdXJsIC1mc1MgLS1yZXRyeSAzIC1vICcvbW50L3N5c3Jvb3QvdmFyL2xpYi9sb2Jlci9zY3JpcHQtNS5zaCcgJ2h0dHA6Ly9ndy5pbnRlcm5hbC92MS9hbnN3ZXIvdGFzay00Mi9pbnN0YWxsLXRvay1maXhlZC9zY3JpcHRzLzUuc2gnCmN1cmwgLWZzUyAtLXJldHJ5IDMgLW8gJy9tbnQvc3lzcm9vdC92YXIvbGliL2xvYmVyL2hvb2stM2VlMDQ5ZTU5MWY1ZTk5YWU4ZjQ5OGFlMzgxZjQ1OTM2NWMyMjFlNDFlNmI0MmYyYWQ2ZTlhNmE5NzUwNTMyZS5zaCcgJ2h0dHA6Ly9ndy5pbnRlcm5hbC92MS9hbnN3ZXIvdGFzay00Mi9pbnN0YWxsLXRvay1maXhlZC9ob29rcy8zZWUwNDllNTkxZjVlOTlhZThmNDk4YWUzODFmNDU5MzY1YzIyMWU0MWU2YjQyZjJhZDZlOWE2YTk3NTA1MzJlJwo= | base64 -d | sh
+0030 echo c2V0IC1ldQp1bWFzayAwNzcKbWtkaXIgLXAgJy9tbnQvc3lzcm9vdC92YXIvbGliL2xvYmVyJyAnL21udC9zeXNyb290L2V0Yy9zeXN0ZW1kL3N5c3RlbS9tdWx0aS11c2VyLnRhcmdldC53YW50cycKcHJpbnRmICclcycgJ2ZpbmFsaXplLXRvay1maXhlZCcgPiAnL21udC9zeXNyb290L3Zhci9saWIvbG9iZXIvZmluYWxpemUudG9rZW4nCmNhdCA+ICcvbW50L3N5c3Jvb3QvdmFyL2xpYi9sb2Jlci9maXJzdGJvb3Quc2gnIDw8J0xPQkVSX0ZJUlNUQk9PVF9FT0YnCiMhL2Jpbi9zaApzZXQgLWV1ClRPS0VOPSQoY2F0IC92YXIvbGliL2xvYmVyL2ZpbmFsaXplLnRva2VuKQpjdXJsIC1mc1MgLW0gMTAgLVggUE9TVCAnaHR0cDovL2d3LmludGVybmFsL3YxL3NpZ25hbC90YXNrLTQyLyciJFRPS0VOIicvYm9vdGVkJyAtLWRhdGEtYmluYXJ5ICcnID4vZGV2L251bGwgMj4mMSB8fCA6ClRPS0VOPSQoY2F0IC92YXIvbGliL2xvYmVyL2ZpbmFsaXplLnRva2VuKQpjdXJsIC1mc1MgLW0gMTAgLVggUE9TVCAnaHR0cDovL2d3LmludGVybmFsL3YxL3NpZ25hbC90YXNrLTQyLyciJFRPS0VOIicvaW5pdC1zdGFydCcgLS1kYXRhLWJpbmFyeSAnJyA+L2Rldi9udWxsIDI+JjEgfHwgOgpzaCAvdmFyL2xpYi9sb2Jlci9zY3JpcHQtNS5zaApzaCAvdmFyL2xpYi9sb2Jlci9jbGVhbnVwLnNoCkxPQkVSX0ZJUlNUQk9PVF9FT0YKY2F0ID4gJy9tbnQvc3lzcm9vdC9ldGMvc3lzdGVtZC9zeXN0ZW0vbG9iZXItZmlyc3Rib290LnNlcnZpY2UnIDw8J0xPQkVSX1VOSVRfRU9GJwpbVW5pdF0KRGVzY3JpcHRpb249TG9iZXIgZmlyc3QgYm9vdApBZnRlcj1uZXR3b3JrLW9ubGluZS50YXJnZXQKV2FudHM9bmV0d29yay1vbmxpbmUudGFyZ2V0CkNvbmRpdGlvblBhdGhFeGlzdHM9L3Zhci9saWIvbG9iZXIvZmlyc3Rib290LnBlbmRpbmcKW1NlcnZpY2VdClR5cGU9b25lc2hvdApFeGVjU3RhcnQ9L2Jpbi9zaCAvdmFyL2xpYi9sb2Jlci9maXJzdGJvb3Quc2gKW0luc3RhbGxdCldhbnRlZEJ5PW11bHRpLXVzZXIudGFyZ2V0CkxPQkVSX1VOSVRfRU9GCmxuIC1zZiAuLi9sb2Jlci1maXJzdGJvb3Quc2VydmljZSAnL21udC9zeXNyb290L2V0Yy9zeXN0ZW1kL3N5c3RlbS9tdWx0aS11c2VyLnRhcmdldC53YW50cy9sb2Jlci1maXJzdGJvb3Quc2VydmljZScKdG91Y2ggJy9tbnQvc3lzcm9vdC92YXIvbGliL2xvYmVyL2ZpcnN0Ym9vdC5wZW5kaW5nJwo= | base64 -d | sh
+0031 echo c2V0IC1ldQp1bWFzayAwNzcKbWtkaXIgLXAgJy9tbnQvc3lzcm9vdC92YXIvbGliL2xvYmVyJwpjYXQgPiAnL21udC9zeXNyb290L3Zhci9saWIvbG9iZXIvY2xlYW51cC5zaCcgPDwnTE9CRVJfQ0xFQU5VUF9FT0YnCiMhL2Jpbi9zaApzZXQgLWV1ClRPS0VOPSQoY2F0IC92YXIvbGliL2xvYmVyL2ZpbmFsaXplLnRva2VuKQpjdXJsIC1mc1MgLW0gMTAgLVggUE9TVCAnaHR0cDovL2d3LmludGVybmFsL3YxL3NpZ25hbC90YXNrLTQyLyciJFRPS0VOIicvaW5pdC1kb25lJyAtLWRhdGEtYmluYXJ5ICcnID4vZGV2L251bGwgMj4mMSB8fCA6ClRPS0VOPSQoY2F0IC92YXIvbGliL2xvYmVyL2ZpbmFsaXplLnRva2VuKQpjdXJsIC1mc1MgLW0gMTAgLVggUE9TVCAnaHR0cDovL2d3LmludGVybmFsL3YxL3NpZ25hbC90YXNrLTQyLyciJFRPS0VOIicvcmVhZHknIC0tZGF0YS1iaW5hcnkgJycgPi9kZXYvbnVsbCAyPiYxIHx8IDoKcm0gLWYgL3Zhci9saWIvbG9iZXIvZmluYWxpemUudG9rZW4gL3Zhci9saWIvbG9iZXIvZmlyc3Rib290LnBlbmRpbmcgL3Zhci9saWIvbG9iZXIvc2NyaXB0LSouc2ggL3Zhci9saWIvbG9iZXIvaG9vay0qIC92YXIvbGliL2xvYmVyL2ZpcnN0Ym9vdC5zaCAvdmFyL2xpYi9sb2Jlci9jbGVhbnVwLnNoCnJtIC1mIC9yb290L2FuYWNvbmRhLWtzLmNmZyAvcm9vdC9vcmlnaW5hbC1rcy5jZmcgL3Zhci9sb2cvaW5zdGFsbGVyL2F1dG9pbnN0YWxsLXVzZXItZGF0YSAvdmFyL2xvZy9pbnN0YWxsZXIvYXV0b2luc3RhbGwtdXNlci1kYXRhLnR4dCAvdmFyL2xvZy9pbnN0YWxsZXIvY2RlYmNvbmYvcXVlc3Rpb25zLmRhdCAvdmFyL2FkbS9hdXRvaW5zdGFsbC9jYWNoZS9pbnN0YWxsZWRTeXN0ZW0ueG1sCkxPQkVSX0NMRUFOVVBfRU9GCg== | base64 -d | sh
```

### Old 107–284; new 33–33

S1: fixed global environment and inline signal/run_hook library are removed; flattened snapshot instances and FileScript now own per-instance EnvName/EnvValue, digest/interpreter/timeout/required execution. Task6 inserts one-line wrapper downloads, never copies old inline runtime. Legacy fake digest strings become real SHA-256 fixture digest. Task8 implements/verifies runtime bodies.

B1: firstboot now stages local slot wrappers and local digest payloads before reboot; systemd invokes the installed native runner. Boot credential no longer enters firstboot; independent local finalize token used. Cleanup follows success and erases cached answers/payloads. Legacy firstboot reused install TOKEN despite writing finalize.token. Unit/marker/local paths change to this explicit contract.

B3: cmdline and explicit url installation source fix 9a252db/743e8af; %include follows %pre (2d59bbb), fixture writes real firmware-specific partition contents to the include file before use. BIOS GPT adds biosboot, UEFI uses ESP (83584d7). Native %pre/%post erroronfail + set -eu preserve failure; %post --nochroot matches /mnt/sysroot staging. Added curl dependency and reboot are fixture-owned syntax.

S5: all remaining profile/blank/layout/value differences in this range are directly authored in the committed .tmpl/.fragment snapshots: hostname remains web-01; identity is task-42 (valid model ID) rather than old numeric 42; new tokens shorter but valid; full includes one real script per supported timing, minimal keeps the slot empty (still receives runtime wrapper for stage signals); SSH keys/legacy host metadata omitted from independent assembly fixtures because Task9 owns actual builtin defaults and SSH hook. No implicit old-model field assembly remains.

```diff
-0107 %post --nochroot
-0108 GW='http://gw.internal'
-0109 ART='http://ng.internal/artifacts'
-0110 TASK='42'
-0111 TOKEN='install-tok-fixed-0000000000000000'
-0112 SEQ=0
-0113 
-0114 signal() {
-0115   local phase="$1" rc="${2:-0}" log="${3:-/dev/null}" digest="${4:-}"
-0116   SEQ=$((SEQ+1))
-0117   local q="$GW/v1/signal/$TASK/$TOKEN/$phase?rc=$rc&seq=$SEQ"
-0118   if [ -n "$digest" ]; then
-0119     q="$q&digest=$digest"
-0120   fi
-0121   for i in 1 2 3; do
-0122     curl -fsS -m 10 -X POST "$q" \
-0123       -H 'Content-Type: text/plain' --data-binary "@$log" && return 0
-0124     sleep $((i*3))
-0125   done
-0126   logger -t lober "signal failed: $phase rc=$rc"
-0127   return 0
-0128 }
-0129 
-0130 run_hook() {
-0131   local digest="$1" required="$2" timeout_sec="$3"
-0132   curl -fsS -o /tmp/h.sh "$ART/hooks/$digest/script.sh" || return 1
-0133   echo "$digest  /tmp/h.sh" | sha256sum -c - || return 1
-0134   ( set -euo pipefail; timeout "$timeout_sec" bash /tmp/h.sh ) 2>&1 | tee /tmp/h.log
-0135   local rc=${PIPESTATUS[0]}
-0136   signal "hook-report" "$rc" /tmp/h.log "$digest"
-0137   [ "$rc" -eq 0 ] || [ "$required" != "true" ]
-0138 }
-0139 run_hook 'bbbb2222nochroot' 'false' '300' || exit 1
-0140 %end
-0141 %post
-0142 GW='http://gw.internal'
-0143 ART='http://ng.internal/artifacts'
-0144 TASK='42'
-0145 TOKEN='install-tok-fixed-0000000000000000'
-0146 SEQ=0
-0147 
-0148 signal() {
-0149   local phase="$1" rc="${2:-0}" log="${3:-/dev/null}" digest="${4:-}"
-0150   SEQ=$((SEQ+1))
-0151   local q="$GW/v1/signal/$TASK/$TOKEN/$phase?rc=$rc&seq=$SEQ"
-0152   if [ -n "$digest" ]; then
-0153     q="$q&digest=$digest"
-0154   fi
-0155   for i in 1 2 3; do
-0156     curl -fsS -m 10 -X POST "$q" \
-0157       -H 'Content-Type: text/plain' --data-binary "@$log" && return 0
-0158     sleep $((i*3))
-0159   done
-0160   logger -t lober "signal failed: $phase rc=$rc"
-0161   return 0
-0162 }
-0163 
-0164 run_hook() {
-0165   local digest="$1" required="$2" timeout_sec="$3"
-0166   curl -fsS -o /tmp/h.sh "$ART/hooks/$digest/script.sh" || return 1
-0167   echo "$digest  /tmp/h.sh" | sha256sum -c - || return 1
-0168   ( set -euo pipefail; timeout "$timeout_sec" bash /tmp/h.sh ) 2>&1 | tee /tmp/h.log
-0169   local rc=${PIPESTATUS[0]}
-0170   signal "hook-report" "$rc" /tmp/h.log "$digest"
-0171   [ "$rc" -eq 0 ] || [ "$required" != "true" ]
-0172 }
-0173 run_hook 'cccc3333post' 'true' '300' || exit 1
-0174 %end
-0175 %post
-0176 mkdir -p /var/lib/lober /var/lib/lober/hooks
-0177 
-0178 install -m 600 /dev/null /var/lib/lober/finalize.token
-0179 echo 'finalize-tok-fixed-0000000000000000' > /var/lib/lober/finalize.token
-0180 
-0181 install -m 600 /dev/null /var/lib/lober/params.env
-0182 cat > /var/lib/lober/params.env <<'LOBER_PARAMS_EOF'
-0183 LOBER_CONTRACT_VERSION=''
-0184 LOBER_TASK_ID='42'
-0185 LOBER_SN='SN-FIXED-0001'
-0186 LOBER_MAC=''
-0187 LOBER_UUID=''
-0188 LOBER_HOSTNAME='web-01'
-0189 LOBER_OS_FAMILY='rhel'
-0190 LOBER_OS_VERSION=''
-0191 LOBER_ARCH=''
-0192 LOBER_DISKS=''
-0193 LOBER_DISK_PRIMARY='/dev/disk/by-id/wwn-0x5000c500fixed0001'
-0194 LOBER_NICS=''
-0195 LOBER_RAM_MB=''
-0196 LOBER_CPU_CORES=''
-0197 LOBER_VENDOR=''
-0198 LOBER_PRODUCT=''
-0199 LOBER_BIZ_IP='10.20.0.15'
-0200 LOBER_BIZ_GW=''
-0201 LOBER_BIZ_MASK=''
-0202 LOBER_ARTIFACT_BASE='http://ng.internal/artifacts'
-0203 LOBER_GATEWAY_BASE='http://gw.internal'
-0204 LOBER_RACK=''
-0205 LOBER_ROW=''
-0206 LOBER_DC=''
-0207 LOBER_PARAMS_EOF
-0208 
-0209 cat > /var/lib/lober/hooks/manifest <<'LOBER_MANIFEST_EOF'
-0210 010-dddd4444firstboot.sh digest=dddd4444firstboot required=true order=10
-0211 LOBER_MANIFEST_EOF
-0212 
-0213 curl -fsS -o /var/lib/lober/hooks/010-dddd4444firstboot.sh 'http://ng.internal/artifacts/hooks/dddd4444firstboot/script.sh'
-0214 echo 'dddd4444firstboot  /var/lib/lober/hooks/010-dddd4444firstboot.sh' | sha256sum -c -
-0215 chmod 700 /var/lib/lober/hooks/010-dddd4444firstboot.sh
-0216 
-0217 cat > /usr/local/sbin/lober-init.sh <<'LOBER_INIT_EOF'
-0218 #!/bin/bash
-0219 . /var/lib/lober/params.env
-0220 
-0221 GW='http://gw.internal'
-0222 ART='http://ng.internal/artifacts'
-0223 TASK='42'
-0224 TOKEN='install-tok-fixed-0000000000000000'
-0225 SEQ=0
-0226 
-0227 signal() {
-0228   local phase="$1" rc="${2:-0}" log="${3:-/dev/null}" digest="${4:-}"
-0229   SEQ=$((SEQ+1))
-0230   local q="$GW/v1/signal/$TASK/$TOKEN/$phase?rc=$rc&seq=$SEQ"
-0231   if [ -n "$digest" ]; then
-0232     q="$q&digest=$digest"
-0233   fi
-0234   for i in 1 2 3; do
-0235     curl -fsS -m 10 -X POST "$q" \
-0236       -H 'Content-Type: text/plain' --data-binary "@$log" && return 0
-0237     sleep $((i*3))
-0238   done
-0239   logger -t lober "signal failed: $phase rc=$rc"
-0240   return 0
-0241 }
-0242 
-0243 run_hook() {
-0244   local digest="$1" required="$2" timeout_sec="$3"
-0245   curl -fsS -o /tmp/h.sh "$ART/hooks/$digest/script.sh" || return 1
-0246   echo "$digest  /tmp/h.sh" | sha256sum -c - || return 1
-0247   ( set -euo pipefail; timeout "$timeout_sec" bash /tmp/h.sh ) 2>&1 | tee /tmp/h.log
-0248   local rc=${PIPESTATUS[0]}
-0249   signal "hook-report" "$rc" /tmp/h.log "$digest"
-0250   [ "$rc" -eq 0 ] || [ "$required" != "true" ]
-0251 }
-0252 
-0253 signal "booted" 0
-0254 signal "init-start" 0
-0255 
-0256 bash /var/lib/lober/hooks/010-dddd4444firstboot.sh > /tmp/fb-dddd4444firstboot.log 2>&1
-0257 signal "hook-report" "$?" /tmp/fb-dddd4444firstboot.log "dddd4444firstboot"
-0258 
-0259 signal "init-done" 0
-0260 signal "ready" 0
-0261 
-0262 shred -u /var/lib/lober/params.env /var/lib/lober/finalize.token
-0263 rm -f /var/lib/lober/init.pending
-0264 sed -i '/rootpw\|--iscrypted\|inst.ks=/d' /root/anaconda-ks.cfg 2>/dev/null
-0265 LOBER_INIT_EOF
-0266 chmod 755 /usr/local/sbin/lober-init.sh
-0267 
-0268 cat > /etc/systemd/system/lober-init.service <<'LOBER_UNIT_EOF'
-0269 [Unit]
-0270 Description=Lober OS init
-0271 After=network-online.target
-0272 Wants=network-online.target
-0273 ConditionPathExists=/var/lib/lober/init.pending
-0274 [Service]
-0275 Type=oneshot
-0276 ExecStart=/usr/local/sbin/lober-init.sh
-0277 StandardOutput=journal+console
-0278 [Install]
-0279 WantedBy=multi-user.target
-0280 LOBER_UNIT_EOF
-0281 
-0282 systemctl enable lober-init.service
-0283 touch /var/lib/lober/init.pending
-0284 %end
+0033 reboot
```

## kickstart-minimal-bios.golden

Compared with `kickstart_minimal.golden`; firmware was not split in that old reference.

### Old 1–0; new 1–2

B3: cmdline and explicit url installation source fix 9a252db/743e8af; %include follows %pre (2d59bbb), fixture writes real firmware-specific partition contents to the include file before use. BIOS GPT adds biosboot, UEFI uses ESP (83584d7). Native %pre/%post erroronfail + set -eu preserve failure; %post --nochroot matches /mnt/sysroot staging. Added curl dependency and reboot are fixture-owned syntax.

S5: all remaining profile/blank/layout/value differences in this range are directly authored in the committed .tmpl/.fragment snapshots: hostname remains web-01; identity is task-42 (valid model ID) rather than old numeric 42; new tokens shorter but valid; full includes one real script per supported timing, minimal keeps the slot empty (still receives runtime wrapper for stage signals); SSH keys/legacy host metadata omitted from independent assembly fixtures because Task9 owns actual builtin defaults and SSH hook. No implicit old-model field assembly remains.

```diff
+0001 cmdline
+0002 url --url=http://ng.internal/artifacts/fixture/repo/
```

### Old 5–7; new 7–7

B3: cmdline and explicit url installation source fix 9a252db/743e8af; %include follows %pre (2d59bbb), fixture writes real firmware-specific partition contents to the include file before use. BIOS GPT adds biosboot, UEFI uses ESP (83584d7). Native %pre/%post erroronfail + set -eu preserve failure; %post --nochroot matches /mnt/sysroot staging. Added curl dependency and reboot are fixture-owned syntax.

S5: all remaining profile/blank/layout/value differences in this range are directly authored in the committed .tmpl/.fragment snapshots: hostname remains web-01; identity is task-42 (valid model ID) rather than old numeric 42; new tokens shorter but valid; full includes one real script per supported timing, minimal keeps the slot empty (still receives runtime wrapper for stage signals); SSH keys/legacy host metadata omitted from independent assembly fixtures because Task9 owns actual builtin defaults and SSH hook. No implicit old-model field assembly remains.

```diff
-0005 clearpart --all --drives=/dev/disk/by-id/wwn-0x5000c500fixed0001
-0006 part / --fstype=xfs --grow --ondisk=/dev/disk/by-id/wwn-0x5000c500fixed0001
-0007 network --bootproto=dhcp --activate
+0007 network --bootproto=dhcp --activate --hostname=web-01
```

### Old 9–9; new 9–8

B3: cmdline and explicit url installation source fix 9a252db/743e8af; %include follows %pre (2d59bbb), fixture writes real firmware-specific partition contents to the include file before use. BIOS GPT adds biosboot, UEFI uses ESP (83584d7). Native %pre/%post erroronfail + set -eu preserve failure; %post --nochroot matches /mnt/sysroot staging. Added curl dependency and reboot are fixture-owned syntax.

S5: all remaining profile/blank/layout/value differences in this range are directly authored in the committed .tmpl/.fragment snapshots: hostname remains web-01; identity is task-42 (valid model ID) rather than old numeric 42; new tokens shorter but valid; full includes one real script per supported timing, minimal keeps the slot empty (still receives runtime wrapper for stage signals); SSH keys/legacy host metadata omitted from independent assembly fixtures because Task9 owns actual builtin defaults and SSH hook. No implicit old-model field assembly remains.

```diff
-0009 sshkey --username=root "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIGZ5ZDhhOWMwZTNiNGY1YzZkN2E4YjljMGUzYjRmNWM2 ops@lober"
```

### Old 13–12; new 12–12

B3: cmdline and explicit url installation source fix 9a252db/743e8af; %include follows %pre (2d59bbb), fixture writes real firmware-specific partition contents to the include file before use. BIOS GPT adds biosboot, UEFI uses ESP (83584d7). Native %pre/%post erroronfail + set -eu preserve failure; %post --nochroot matches /mnt/sysroot staging. Added curl dependency and reboot are fixture-owned syntax.

S5: all remaining profile/blank/layout/value differences in this range are directly authored in the committed .tmpl/.fragment snapshots: hostname remains web-01; identity is task-42 (valid model ID) rather than old numeric 42; new tokens shorter but valid; full includes one real script per supported timing, minimal keeps the slot empty (still receives runtime wrapper for stage signals); SSH keys/legacy host metadata omitted from independent assembly fixtures because Task9 owns actual builtin defaults and SSH hook. No implicit old-model field assembly remains.

```diff
+0012 curl
```

### Old 14–71; new 14–22

S1: fixed global environment and inline signal/run_hook library are removed; flattened snapshot instances and FileScript now own per-instance EnvName/EnvValue, digest/interpreter/timeout/required execution. Task6 inserts one-line wrapper downloads, never copies old inline runtime. Legacy fake digest strings become real SHA-256 fixture digest. Task8 implements/verifies runtime bodies.

B3: cmdline and explicit url installation source fix 9a252db/743e8af; %include follows %pre (2d59bbb), fixture writes real firmware-specific partition contents to the include file before use. BIOS GPT adds biosboot, UEFI uses ESP (83584d7). Native %pre/%post erroronfail + set -eu preserve failure; %post --nochroot matches /mnt/sysroot staging. Added curl dependency and reboot are fixture-owned syntax.

S5: all remaining profile/blank/layout/value differences in this range are directly authored in the committed .tmpl/.fragment snapshots: hostname remains web-01; identity is task-42 (valid model ID) rather than old numeric 42; new tokens shorter but valid; full includes one real script per supported timing, minimal keeps the slot empty (still receives runtime wrapper for stage signals); SSH keys/legacy host metadata omitted from independent assembly fixtures because Task9 owns actual builtin defaults and SSH hook. No implicit old-model field assembly remains.

```diff
-0014 
-0015 %pre
-0016 LOBER_CONTRACT_VERSION=''
-0017 LOBER_TASK_ID='42'
-0018 LOBER_SN='SN-FIXED-0001'
-0019 LOBER_MAC=''
-0020 LOBER_UUID=''
-0021 LOBER_HOSTNAME='web-01'
-0022 LOBER_OS_FAMILY='rhel'
-0023 LOBER_OS_VERSION=''
-0024 LOBER_ARCH=''
-0025 LOBER_DISKS=''
-0026 LOBER_DISK_PRIMARY='/dev/disk/by-id/wwn-0x5000c500fixed0001'
-0027 LOBER_NICS=''
-0028 LOBER_RAM_MB=''
-0029 LOBER_CPU_CORES=''
-0030 LOBER_VENDOR=''
-0031 LOBER_PRODUCT=''
-0032 LOBER_BIZ_IP='10.20.0.15'
-0033 LOBER_BIZ_GW=''
-0034 LOBER_BIZ_MASK=''
-0035 LOBER_ARTIFACT_BASE='http://ng.internal/artifacts'
-0036 LOBER_GATEWAY_BASE='http://gw.internal'
-0037 LOBER_RACK=''
-0038 LOBER_ROW=''
-0039 LOBER_DC=''
-0040 # 用户 hook 从这里开始
-0041 GW='http://gw.internal'
-0042 ART='http://ng.internal/artifacts'
-0043 TASK='42'
-0044 TOKEN='install-tok-fixed-0000000000000000'
-0045 SEQ=0
-0046 
-0047 signal() {
-0048   local phase="$1" rc="${2:-0}" log="${3:-/dev/null}" digest="${4:-}"
-0049   SEQ=$((SEQ+1))
-0050   local q="$GW/v1/signal/$TASK/$TOKEN/$phase?rc=$rc&seq=$SEQ"
-0051   if [ -n "$digest" ]; then
-0052     q="$q&digest=$digest"
-0053   fi
-0054   for i in 1 2 3; do
-0055     curl -fsS -m 10 -X POST "$q" \
-0056       -H 'Content-Type: text/plain' --data-binary "@$log" && return 0
-0057     sleep $((i*3))
-0058   done
-0059   logger -t lober "signal failed: $phase rc=$rc"
-0060   return 0
-0061 }
-0062 
-0063 run_hook() {
-0064   local digest="$1" required="$2" timeout_sec="$3"
-0065   curl -fsS -o /tmp/h.sh "$ART/hooks/$digest/script.sh" || return 1
-0066   echo "$digest  /tmp/h.sh" | sha256sum -c - || return 1
-0067   ( set -euo pipefail; timeout "$timeout_sec" bash /tmp/h.sh ) 2>&1 | tee /tmp/h.log
-0068   local rc=${PIPESTATUS[0]}
-0069   signal "hook-report" "$rc" /tmp/h.log "$digest"
-0070   [ "$rc" -eq 0 ] || [ "$required" != "true" ]
-0071 }
+0014 %pre --erroronfail
+0015 set -eu
+0016 echo c2V0IC1ldQp1bWFzayAwNzcKbWtkaXIgLXAgJy90bXAnCmN1cmwgLWZzUyAtLXJldHJ5IDMgLW8gJy90bXAvc2NyaXB0LTIuc2gnICdodHRwOi8vZ3cuaW50ZXJuYWwvdjEvYW5zd2VyL3Rhc2stNDIvaW5zdGFsbC10b2stZml4ZWQvc2NyaXB0cy8yLnNoJwpzaCAnL3RtcC9zY3JpcHQtMi5zaCcK | base64 -d | sh
+0017 cat > /tmp/lober-partition.ks <<'LOBER_PARTITION_EOF'
+0018 zerombr
+0019 clearpart --all --initlabel --disklabel=gpt --drives=sda
+0020 part biosboot --fstype=biosboot --size=1 --ondisk=sda
+0021 part / --fstype=xfs --grow --ondisk=sda
+0022 LOBER_PARTITION_EOF
```

### Old 73–173; new 24–31

S1: fixed global environment and inline signal/run_hook library are removed; flattened snapshot instances and FileScript now own per-instance EnvName/EnvValue, digest/interpreter/timeout/required execution. Task6 inserts one-line wrapper downloads, never copies old inline runtime. Legacy fake digest strings become real SHA-256 fixture digest. Task8 implements/verifies runtime bodies.

B1: firstboot now stages local slot wrappers and local digest payloads before reboot; systemd invokes the installed native runner. Boot credential no longer enters firstboot; independent local finalize token used. Cleanup follows success and erases cached answers/payloads. Legacy firstboot reused install TOKEN despite writing finalize.token. Unit/marker/local paths change to this explicit contract.

B3: cmdline and explicit url installation source fix 9a252db/743e8af; %include follows %pre (2d59bbb), fixture writes real firmware-specific partition contents to the include file before use. BIOS GPT adds biosboot, UEFI uses ESP (83584d7). Native %pre/%post erroronfail + set -eu preserve failure; %post --nochroot matches /mnt/sysroot staging. Added curl dependency and reboot are fixture-owned syntax.

S5: all remaining profile/blank/layout/value differences in this range are directly authored in the committed .tmpl/.fragment snapshots: hostname remains web-01; identity is task-42 (valid model ID) rather than old numeric 42; new tokens shorter but valid; full includes one real script per supported timing, minimal keeps the slot empty (still receives runtime wrapper for stage signals); SSH keys/legacy host metadata omitted from independent assembly fixtures because Task9 owns actual builtin defaults and SSH hook. No implicit old-model field assembly remains.

```diff
-0073 %post
-0074 mkdir -p /var/lib/lober /var/lib/lober/hooks
-0075 
-0076 install -m 600 /dev/null /var/lib/lober/finalize.token
-0077 echo 'finalize-tok-fixed-0000000000000000' > /var/lib/lober/finalize.token
-0078 
-0079 install -m 600 /dev/null /var/lib/lober/params.env
-0080 cat > /var/lib/lober/params.env <<'LOBER_PARAMS_EOF'
-0081 LOBER_CONTRACT_VERSION=''
-0082 LOBER_TASK_ID='42'
-0083 LOBER_SN='SN-FIXED-0001'
-0084 LOBER_MAC=''
-0085 LOBER_UUID=''
-0086 LOBER_HOSTNAME='web-01'
-0087 LOBER_OS_FAMILY='rhel'
-0088 LOBER_OS_VERSION=''
-0089 LOBER_ARCH=''
-0090 LOBER_DISKS=''
-0091 LOBER_DISK_PRIMARY='/dev/disk/by-id/wwn-0x5000c500fixed0001'
-0092 LOBER_NICS=''
-0093 LOBER_RAM_MB=''
-0094 LOBER_CPU_CORES=''
-0095 LOBER_VENDOR=''
-0096 LOBER_PRODUCT=''
-0097 LOBER_BIZ_IP='10.20.0.15'
-0098 LOBER_BIZ_GW=''
-0099 LOBER_BIZ_MASK=''
-0100 LOBER_ARTIFACT_BASE='http://ng.internal/artifacts'
-0101 LOBER_GATEWAY_BASE='http://gw.internal'
-0102 LOBER_RACK=''
-0103 LOBER_ROW=''
-0104 LOBER_DC=''
-0105 LOBER_PARAMS_EOF
-0106 
-0107 cat > /var/lib/lober/hooks/manifest <<'LOBER_MANIFEST_EOF'
-0108 LOBER_MANIFEST_EOF
-0109 
-0110 cat > /usr/local/sbin/lober-init.sh <<'LOBER_INIT_EOF'
-0111 #!/bin/bash
-0112 . /var/lib/lober/params.env
-0113 
-0114 GW='http://gw.internal'
-0115 ART='http://ng.internal/artifacts'
-0116 TASK='42'
-0117 TOKEN='install-tok-fixed-0000000000000000'
-0118 SEQ=0
-0119 
-0120 signal() {
-0121   local phase="$1" rc="${2:-0}" log="${3:-/dev/null}" digest="${4:-}"
-0122   SEQ=$((SEQ+1))
-0123   local q="$GW/v1/signal/$TASK/$TOKEN/$phase?rc=$rc&seq=$SEQ"
-0124   if [ -n "$digest" ]; then
-0125     q="$q&digest=$digest"
-0126   fi
-0127   for i in 1 2 3; do
-0128     curl -fsS -m 10 -X POST "$q" \
-0129       -H 'Content-Type: text/plain' --data-binary "@$log" && return 0
-0130     sleep $((i*3))
-0131   done
-0132   logger -t lober "signal failed: $phase rc=$rc"
-0133   return 0
-0134 }
-0135 
-0136 run_hook() {
-0137   local digest="$1" required="$2" timeout_sec="$3"
-0138   curl -fsS -o /tmp/h.sh "$ART/hooks/$digest/script.sh" || return 1
-0139   echo "$digest  /tmp/h.sh" | sha256sum -c - || return 1
-0140   ( set -euo pipefail; timeout "$timeout_sec" bash /tmp/h.sh ) 2>&1 | tee /tmp/h.log
-0141   local rc=${PIPESTATUS[0]}
-0142   signal "hook-report" "$rc" /tmp/h.log "$digest"
-0143   [ "$rc" -eq 0 ] || [ "$required" != "true" ]
-0144 }
-0145 
-0146 signal "booted" 0
-0147 signal "init-start" 0
-0148 
-0149 signal "init-done" 0
-0150 signal "ready" 0
-0151 
-0152 shred -u /var/lib/lober/params.env /var/lib/lober/finalize.token
-0153 rm -f /var/lib/lober/init.pending
-0154 sed -i '/rootpw\|--iscrypted\|inst.ks=/d' /root/anaconda-ks.cfg 2>/dev/null
-0155 LOBER_INIT_EOF
-0156 chmod 755 /usr/local/sbin/lober-init.sh
-0157 
-0158 cat > /etc/systemd/system/lober-init.service <<'LOBER_UNIT_EOF'
-0159 [Unit]
-0160 Description=Lober OS init
-0161 After=network-online.target
-0162 Wants=network-online.target
-0163 ConditionPathExists=/var/lib/lober/init.pending
-0164 [Service]
-0165 Type=oneshot
-0166 ExecStart=/usr/local/sbin/lober-init.sh
-0167 StandardOutput=journal+console
-0168 [Install]
-0169 WantedBy=multi-user.target
-0170 LOBER_UNIT_EOF
-0171 
-0172 systemctl enable lober-init.service
-0173 touch /var/lib/lober/init.pending
+0024 %include /tmp/lober-partition.ks
+0025 %post --nochroot --erroronfail
+0026 set -eu
+0027 echo c2V0IC1ldQp1bWFzayAwNzcKbWtkaXIgLXAgJy90bXAnCmN1cmwgLWZzUyAtLXJldHJ5IDMgLW8gJy90bXAvc2NyaXB0LTMuc2gnICdodHRwOi8vZ3cuaW50ZXJuYWwvdjEvYW5zd2VyL3Rhc2stNDIvaW5zdGFsbC10b2stZml4ZWQvc2NyaXB0cy8zLnNoJwpzaCAnL3RtcC9zY3JpcHQtMy5zaCcK | base64 -d | sh
+0028 echo c2V0IC1ldQp1bWFzayAwNzcKbWtkaXIgLXAgJy9tbnQvc3lzcm9vdC92YXIvbGliL2xvYmVyJwpjdXJsIC1mc1MgLS1yZXRyeSAzIC1vICcvbW50L3N5c3Jvb3QvdmFyL2xpYi9sb2Jlci9zY3JpcHQtNC5zaCcgJ2h0dHA6Ly9ndy5pbnRlcm5hbC92MS9hbnN3ZXIvdGFzay00Mi9pbnN0YWxsLXRvay1maXhlZC9zY3JpcHRzLzQuc2gnCmNocm9vdCAnL21udC9zeXNyb290JyBzaCAnL3Zhci9saWIvbG9iZXIvc2NyaXB0LTQuc2gnCg== | base64 -d | sh
+0029 echo c2V0IC1ldQp1bWFzayAwNzcKbWtkaXIgLXAgJy9tbnQvc3lzcm9vdC92YXIvbGliL2xvYmVyJwpjdXJsIC1mc1MgLS1yZXRyeSAzIC1vICcvbW50L3N5c3Jvb3QvdmFyL2xpYi9sb2Jlci9zY3JpcHQtNS5zaCcgJ2h0dHA6Ly9ndy5pbnRlcm5hbC92MS9hbnN3ZXIvdGFzay00Mi9pbnN0YWxsLXRvay1maXhlZC9zY3JpcHRzLzUuc2gnCg== | base64 -d | sh
+0030 echo c2V0IC1ldQp1bWFzayAwNzcKbWtkaXIgLXAgJy9tbnQvc3lzcm9vdC92YXIvbGliL2xvYmVyJyAnL21udC9zeXNyb290L2V0Yy9zeXN0ZW1kL3N5c3RlbS9tdWx0aS11c2VyLnRhcmdldC53YW50cycKcHJpbnRmICclcycgJ2ZpbmFsaXplLXRvay1maXhlZCcgPiAnL21udC9zeXNyb290L3Zhci9saWIvbG9iZXIvZmluYWxpemUudG9rZW4nCmNhdCA+ICcvbW50L3N5c3Jvb3QvdmFyL2xpYi9sb2Jlci9maXJzdGJvb3Quc2gnIDw8J0xPQkVSX0ZJUlNUQk9PVF9FT0YnCiMhL2Jpbi9zaApzZXQgLWV1ClRPS0VOPSQoY2F0IC92YXIvbGliL2xvYmVyL2ZpbmFsaXplLnRva2VuKQpjdXJsIC1mc1MgLW0gMTAgLVggUE9TVCAnaHR0cDovL2d3LmludGVybmFsL3YxL3NpZ25hbC90YXNrLTQyLyciJFRPS0VOIicvYm9vdGVkJyAtLWRhdGEtYmluYXJ5ICcnID4vZGV2L251bGwgMj4mMSB8fCA6ClRPS0VOPSQoY2F0IC92YXIvbGliL2xvYmVyL2ZpbmFsaXplLnRva2VuKQpjdXJsIC1mc1MgLW0gMTAgLVggUE9TVCAnaHR0cDovL2d3LmludGVybmFsL3YxL3NpZ25hbC90YXNrLTQyLyciJFRPS0VOIicvaW5pdC1zdGFydCcgLS1kYXRhLWJpbmFyeSAnJyA+L2Rldi9udWxsIDI+JjEgfHwgOgpzaCAvdmFyL2xpYi9sb2Jlci9zY3JpcHQtNS5zaApzaCAvdmFyL2xpYi9sb2Jlci9jbGVhbnVwLnNoCkxPQkVSX0ZJUlNUQk9PVF9FT0YKY2F0ID4gJy9tbnQvc3lzcm9vdC9ldGMvc3lzdGVtZC9zeXN0ZW0vbG9iZXItZmlyc3Rib290LnNlcnZpY2UnIDw8J0xPQkVSX1VOSVRfRU9GJwpbVW5pdF0KRGVzY3JpcHRpb249TG9iZXIgZmlyc3QgYm9vdApBZnRlcj1uZXR3b3JrLW9ubGluZS50YXJnZXQKV2FudHM9bmV0d29yay1vbmxpbmUudGFyZ2V0CkNvbmRpdGlvblBhdGhFeGlzdHM9L3Zhci9saWIvbG9iZXIvZmlyc3Rib290LnBlbmRpbmcKW1NlcnZpY2VdClR5cGU9b25lc2hvdApFeGVjU3RhcnQ9L2Jpbi9zaCAvdmFyL2xpYi9sb2Jlci9maXJzdGJvb3Quc2gKW0luc3RhbGxdCldhbnRlZEJ5PW11bHRpLXVzZXIudGFyZ2V0CkxPQkVSX1VOSVRfRU9GCmxuIC1zZiAuLi9sb2Jlci1maXJzdGJvb3Quc2VydmljZSAnL21udC9zeXNyb290L2V0Yy9zeXN0ZW1kL3N5c3RlbS9tdWx0aS11c2VyLnRhcmdldC53YW50cy9sb2Jlci1maXJzdGJvb3Quc2VydmljZScKdG91Y2ggJy9tbnQvc3lzcm9vdC92YXIvbGliL2xvYmVyL2ZpcnN0Ym9vdC5wZW5kaW5nJwo= | base64 -d | sh
+0031 echo c2V0IC1ldQp1bWFzayAwNzcKbWtkaXIgLXAgJy9tbnQvc3lzcm9vdC92YXIvbGliL2xvYmVyJwpjYXQgPiAnL21udC9zeXNyb290L3Zhci9saWIvbG9iZXIvY2xlYW51cC5zaCcgPDwnTE9CRVJfQ0xFQU5VUF9FT0YnCiMhL2Jpbi9zaApzZXQgLWV1ClRPS0VOPSQoY2F0IC92YXIvbGliL2xvYmVyL2ZpbmFsaXplLnRva2VuKQpjdXJsIC1mc1MgLW0gMTAgLVggUE9TVCAnaHR0cDovL2d3LmludGVybmFsL3YxL3NpZ25hbC90YXNrLTQyLyciJFRPS0VOIicvaW5pdC1kb25lJyAtLWRhdGEtYmluYXJ5ICcnID4vZGV2L251bGwgMj4mMSB8fCA6ClRPS0VOPSQoY2F0IC92YXIvbGliL2xvYmVyL2ZpbmFsaXplLnRva2VuKQpjdXJsIC1mc1MgLW0gMTAgLVggUE9TVCAnaHR0cDovL2d3LmludGVybmFsL3YxL3NpZ25hbC90YXNrLTQyLyciJFRPS0VOIicvcmVhZHknIC0tZGF0YS1iaW5hcnkgJycgPi9kZXYvbnVsbCAyPiYxIHx8IDoKcm0gLWYgL3Zhci9saWIvbG9iZXIvZmluYWxpemUudG9rZW4gL3Zhci9saWIvbG9iZXIvZmlyc3Rib290LnBlbmRpbmcgL3Zhci9saWIvbG9iZXIvc2NyaXB0LSouc2ggL3Zhci9saWIvbG9iZXIvaG9vay0qIC92YXIvbGliL2xvYmVyL2ZpcnN0Ym9vdC5zaCAvdmFyL2xpYi9sb2Jlci9jbGVhbnVwLnNoCnJtIC1mIC9yb290L2FuYWNvbmRhLWtzLmNmZyAvcm9vdC9vcmlnaW5hbC1rcy5jZmcgL3Zhci9sb2cvaW5zdGFsbGVyL2F1dG9pbnN0YWxsLXVzZXItZGF0YSAvdmFyL2xvZy9pbnN0YWxsZXIvYXV0b2luc3RhbGwtdXNlci1kYXRhLnR4dCAvdmFyL2xvZy9pbnN0YWxsZXIvY2RlYmNvbmYvcXVlc3Rpb25zLmRhdCAvdmFyL2FkbS9hdXRvaW5zdGFsbC9jYWNoZS9pbnN0YWxsZWRTeXN0ZW0ueG1sCkxPQkVSX0NMRUFOVVBfRU9GCg== | base64 -d | sh
```

### Old 175–174; new 33–33

B3: cmdline and explicit url installation source fix 9a252db/743e8af; %include follows %pre (2d59bbb), fixture writes real firmware-specific partition contents to the include file before use. BIOS GPT adds biosboot, UEFI uses ESP (83584d7). Native %pre/%post erroronfail + set -eu preserve failure; %post --nochroot matches /mnt/sysroot staging. Added curl dependency and reboot are fixture-owned syntax.

S5: all remaining profile/blank/layout/value differences in this range are directly authored in the committed .tmpl/.fragment snapshots: hostname remains web-01; identity is task-42 (valid model ID) rather than old numeric 42; new tokens shorter but valid; full includes one real script per supported timing, minimal keeps the slot empty (still receives runtime wrapper for stage signals); SSH keys/legacy host metadata omitted from independent assembly fixtures because Task9 owns actual builtin defaults and SSH hook. No implicit old-model field assembly remains.

```diff
+0033 reboot
```

## kickstart-minimal-uefi.golden

Compared with `kickstart_minimal.golden`; firmware was not split in that old reference.

### Old 1–0; new 1–2

B3: cmdline and explicit url installation source fix 9a252db/743e8af; %include follows %pre (2d59bbb), fixture writes real firmware-specific partition contents to the include file before use. BIOS GPT adds biosboot, UEFI uses ESP (83584d7). Native %pre/%post erroronfail + set -eu preserve failure; %post --nochroot matches /mnt/sysroot staging. Added curl dependency and reboot are fixture-owned syntax.

S5: all remaining profile/blank/layout/value differences in this range are directly authored in the committed .tmpl/.fragment snapshots: hostname remains web-01; identity is task-42 (valid model ID) rather than old numeric 42; new tokens shorter but valid; full includes one real script per supported timing, minimal keeps the slot empty (still receives runtime wrapper for stage signals); SSH keys/legacy host metadata omitted from independent assembly fixtures because Task9 owns actual builtin defaults and SSH hook. No implicit old-model field assembly remains.

```diff
+0001 cmdline
+0002 url --url=http://ng.internal/artifacts/fixture/repo/
```

### Old 5–7; new 7–7

B3: cmdline and explicit url installation source fix 9a252db/743e8af; %include follows %pre (2d59bbb), fixture writes real firmware-specific partition contents to the include file before use. BIOS GPT adds biosboot, UEFI uses ESP (83584d7). Native %pre/%post erroronfail + set -eu preserve failure; %post --nochroot matches /mnt/sysroot staging. Added curl dependency and reboot are fixture-owned syntax.

S5: all remaining profile/blank/layout/value differences in this range are directly authored in the committed .tmpl/.fragment snapshots: hostname remains web-01; identity is task-42 (valid model ID) rather than old numeric 42; new tokens shorter but valid; full includes one real script per supported timing, minimal keeps the slot empty (still receives runtime wrapper for stage signals); SSH keys/legacy host metadata omitted from independent assembly fixtures because Task9 owns actual builtin defaults and SSH hook. No implicit old-model field assembly remains.

```diff
-0005 clearpart --all --drives=/dev/disk/by-id/wwn-0x5000c500fixed0001
-0006 part / --fstype=xfs --grow --ondisk=/dev/disk/by-id/wwn-0x5000c500fixed0001
-0007 network --bootproto=dhcp --activate
+0007 network --bootproto=dhcp --activate --hostname=web-01
```

### Old 9–9; new 9–8

B3: cmdline and explicit url installation source fix 9a252db/743e8af; %include follows %pre (2d59bbb), fixture writes real firmware-specific partition contents to the include file before use. BIOS GPT adds biosboot, UEFI uses ESP (83584d7). Native %pre/%post erroronfail + set -eu preserve failure; %post --nochroot matches /mnt/sysroot staging. Added curl dependency and reboot are fixture-owned syntax.

S5: all remaining profile/blank/layout/value differences in this range are directly authored in the committed .tmpl/.fragment snapshots: hostname remains web-01; identity is task-42 (valid model ID) rather than old numeric 42; new tokens shorter but valid; full includes one real script per supported timing, minimal keeps the slot empty (still receives runtime wrapper for stage signals); SSH keys/legacy host metadata omitted from independent assembly fixtures because Task9 owns actual builtin defaults and SSH hook. No implicit old-model field assembly remains.

```diff
-0009 sshkey --username=root "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIGZ5ZDhhOWMwZTNiNGY1YzZkN2E4YjljMGUzYjRmNWM2 ops@lober"
```

### Old 13–12; new 12–12

B3: cmdline and explicit url installation source fix 9a252db/743e8af; %include follows %pre (2d59bbb), fixture writes real firmware-specific partition contents to the include file before use. BIOS GPT adds biosboot, UEFI uses ESP (83584d7). Native %pre/%post erroronfail + set -eu preserve failure; %post --nochroot matches /mnt/sysroot staging. Added curl dependency and reboot are fixture-owned syntax.

S5: all remaining profile/blank/layout/value differences in this range are directly authored in the committed .tmpl/.fragment snapshots: hostname remains web-01; identity is task-42 (valid model ID) rather than old numeric 42; new tokens shorter but valid; full includes one real script per supported timing, minimal keeps the slot empty (still receives runtime wrapper for stage signals); SSH keys/legacy host metadata omitted from independent assembly fixtures because Task9 owns actual builtin defaults and SSH hook. No implicit old-model field assembly remains.

```diff
+0012 curl
```

### Old 14–71; new 14–22

S1: fixed global environment and inline signal/run_hook library are removed; flattened snapshot instances and FileScript now own per-instance EnvName/EnvValue, digest/interpreter/timeout/required execution. Task6 inserts one-line wrapper downloads, never copies old inline runtime. Legacy fake digest strings become real SHA-256 fixture digest. Task8 implements/verifies runtime bodies.

B3: cmdline and explicit url installation source fix 9a252db/743e8af; %include follows %pre (2d59bbb), fixture writes real firmware-specific partition contents to the include file before use. BIOS GPT adds biosboot, UEFI uses ESP (83584d7). Native %pre/%post erroronfail + set -eu preserve failure; %post --nochroot matches /mnt/sysroot staging. Added curl dependency and reboot are fixture-owned syntax.

S5: all remaining profile/blank/layout/value differences in this range are directly authored in the committed .tmpl/.fragment snapshots: hostname remains web-01; identity is task-42 (valid model ID) rather than old numeric 42; new tokens shorter but valid; full includes one real script per supported timing, minimal keeps the slot empty (still receives runtime wrapper for stage signals); SSH keys/legacy host metadata omitted from independent assembly fixtures because Task9 owns actual builtin defaults and SSH hook. No implicit old-model field assembly remains.

```diff
-0014 
-0015 %pre
-0016 LOBER_CONTRACT_VERSION=''
-0017 LOBER_TASK_ID='42'
-0018 LOBER_SN='SN-FIXED-0001'
-0019 LOBER_MAC=''
-0020 LOBER_UUID=''
-0021 LOBER_HOSTNAME='web-01'
-0022 LOBER_OS_FAMILY='rhel'
-0023 LOBER_OS_VERSION=''
-0024 LOBER_ARCH=''
-0025 LOBER_DISKS=''
-0026 LOBER_DISK_PRIMARY='/dev/disk/by-id/wwn-0x5000c500fixed0001'
-0027 LOBER_NICS=''
-0028 LOBER_RAM_MB=''
-0029 LOBER_CPU_CORES=''
-0030 LOBER_VENDOR=''
-0031 LOBER_PRODUCT=''
-0032 LOBER_BIZ_IP='10.20.0.15'
-0033 LOBER_BIZ_GW=''
-0034 LOBER_BIZ_MASK=''
-0035 LOBER_ARTIFACT_BASE='http://ng.internal/artifacts'
-0036 LOBER_GATEWAY_BASE='http://gw.internal'
-0037 LOBER_RACK=''
-0038 LOBER_ROW=''
-0039 LOBER_DC=''
-0040 # 用户 hook 从这里开始
-0041 GW='http://gw.internal'
-0042 ART='http://ng.internal/artifacts'
-0043 TASK='42'
-0044 TOKEN='install-tok-fixed-0000000000000000'
-0045 SEQ=0
-0046 
-0047 signal() {
-0048   local phase="$1" rc="${2:-0}" log="${3:-/dev/null}" digest="${4:-}"
-0049   SEQ=$((SEQ+1))
-0050   local q="$GW/v1/signal/$TASK/$TOKEN/$phase?rc=$rc&seq=$SEQ"
-0051   if [ -n "$digest" ]; then
-0052     q="$q&digest=$digest"
-0053   fi
-0054   for i in 1 2 3; do
-0055     curl -fsS -m 10 -X POST "$q" \
-0056       -H 'Content-Type: text/plain' --data-binary "@$log" && return 0
-0057     sleep $((i*3))
-0058   done
-0059   logger -t lober "signal failed: $phase rc=$rc"
-0060   return 0
-0061 }
-0062 
-0063 run_hook() {
-0064   local digest="$1" required="$2" timeout_sec="$3"
-0065   curl -fsS -o /tmp/h.sh "$ART/hooks/$digest/script.sh" || return 1
-0066   echo "$digest  /tmp/h.sh" | sha256sum -c - || return 1
-0067   ( set -euo pipefail; timeout "$timeout_sec" bash /tmp/h.sh ) 2>&1 | tee /tmp/h.log
-0068   local rc=${PIPESTATUS[0]}
-0069   signal "hook-report" "$rc" /tmp/h.log "$digest"
-0070   [ "$rc" -eq 0 ] || [ "$required" != "true" ]
-0071 }
+0014 %pre --erroronfail
+0015 set -eu
+0016 echo c2V0IC1ldQp1bWFzayAwNzcKbWtkaXIgLXAgJy90bXAnCmN1cmwgLWZzUyAtLXJldHJ5IDMgLW8gJy90bXAvc2NyaXB0LTIuc2gnICdodHRwOi8vZ3cuaW50ZXJuYWwvdjEvYW5zd2VyL3Rhc2stNDIvaW5zdGFsbC10b2stZml4ZWQvc2NyaXB0cy8yLnNoJwpzaCAnL3RtcC9zY3JpcHQtMi5zaCcK | base64 -d | sh
+0017 cat > /tmp/lober-partition.ks <<'LOBER_PARTITION_EOF'
+0018 zerombr
+0019 clearpart --all --initlabel --disklabel=gpt --drives=sda
+0020 part /boot/efi --fstype=efi --size=600 --ondisk=sda
+0021 part / --fstype=xfs --grow --ondisk=sda
+0022 LOBER_PARTITION_EOF
```

### Old 73–173; new 24–31

S1: fixed global environment and inline signal/run_hook library are removed; flattened snapshot instances and FileScript now own per-instance EnvName/EnvValue, digest/interpreter/timeout/required execution. Task6 inserts one-line wrapper downloads, never copies old inline runtime. Legacy fake digest strings become real SHA-256 fixture digest. Task8 implements/verifies runtime bodies.

B1: firstboot now stages local slot wrappers and local digest payloads before reboot; systemd invokes the installed native runner. Boot credential no longer enters firstboot; independent local finalize token used. Cleanup follows success and erases cached answers/payloads. Legacy firstboot reused install TOKEN despite writing finalize.token. Unit/marker/local paths change to this explicit contract.

B3: cmdline and explicit url installation source fix 9a252db/743e8af; %include follows %pre (2d59bbb), fixture writes real firmware-specific partition contents to the include file before use. BIOS GPT adds biosboot, UEFI uses ESP (83584d7). Native %pre/%post erroronfail + set -eu preserve failure; %post --nochroot matches /mnt/sysroot staging. Added curl dependency and reboot are fixture-owned syntax.

S5: all remaining profile/blank/layout/value differences in this range are directly authored in the committed .tmpl/.fragment snapshots: hostname remains web-01; identity is task-42 (valid model ID) rather than old numeric 42; new tokens shorter but valid; full includes one real script per supported timing, minimal keeps the slot empty (still receives runtime wrapper for stage signals); SSH keys/legacy host metadata omitted from independent assembly fixtures because Task9 owns actual builtin defaults and SSH hook. No implicit old-model field assembly remains.

```diff
-0073 %post
-0074 mkdir -p /var/lib/lober /var/lib/lober/hooks
-0075 
-0076 install -m 600 /dev/null /var/lib/lober/finalize.token
-0077 echo 'finalize-tok-fixed-0000000000000000' > /var/lib/lober/finalize.token
-0078 
-0079 install -m 600 /dev/null /var/lib/lober/params.env
-0080 cat > /var/lib/lober/params.env <<'LOBER_PARAMS_EOF'
-0081 LOBER_CONTRACT_VERSION=''
-0082 LOBER_TASK_ID='42'
-0083 LOBER_SN='SN-FIXED-0001'
-0084 LOBER_MAC=''
-0085 LOBER_UUID=''
-0086 LOBER_HOSTNAME='web-01'
-0087 LOBER_OS_FAMILY='rhel'
-0088 LOBER_OS_VERSION=''
-0089 LOBER_ARCH=''
-0090 LOBER_DISKS=''
-0091 LOBER_DISK_PRIMARY='/dev/disk/by-id/wwn-0x5000c500fixed0001'
-0092 LOBER_NICS=''
-0093 LOBER_RAM_MB=''
-0094 LOBER_CPU_CORES=''
-0095 LOBER_VENDOR=''
-0096 LOBER_PRODUCT=''
-0097 LOBER_BIZ_IP='10.20.0.15'
-0098 LOBER_BIZ_GW=''
-0099 LOBER_BIZ_MASK=''
-0100 LOBER_ARTIFACT_BASE='http://ng.internal/artifacts'
-0101 LOBER_GATEWAY_BASE='http://gw.internal'
-0102 LOBER_RACK=''
-0103 LOBER_ROW=''
-0104 LOBER_DC=''
-0105 LOBER_PARAMS_EOF
-0106 
-0107 cat > /var/lib/lober/hooks/manifest <<'LOBER_MANIFEST_EOF'
-0108 LOBER_MANIFEST_EOF
-0109 
-0110 cat > /usr/local/sbin/lober-init.sh <<'LOBER_INIT_EOF'
-0111 #!/bin/bash
-0112 . /var/lib/lober/params.env
-0113 
-0114 GW='http://gw.internal'
-0115 ART='http://ng.internal/artifacts'
-0116 TASK='42'
-0117 TOKEN='install-tok-fixed-0000000000000000'
-0118 SEQ=0
-0119 
-0120 signal() {
-0121   local phase="$1" rc="${2:-0}" log="${3:-/dev/null}" digest="${4:-}"
-0122   SEQ=$((SEQ+1))
-0123   local q="$GW/v1/signal/$TASK/$TOKEN/$phase?rc=$rc&seq=$SEQ"
-0124   if [ -n "$digest" ]; then
-0125     q="$q&digest=$digest"
-0126   fi
-0127   for i in 1 2 3; do
-0128     curl -fsS -m 10 -X POST "$q" \
-0129       -H 'Content-Type: text/plain' --data-binary "@$log" && return 0
-0130     sleep $((i*3))
-0131   done
-0132   logger -t lober "signal failed: $phase rc=$rc"
-0133   return 0
-0134 }
-0135 
-0136 run_hook() {
-0137   local digest="$1" required="$2" timeout_sec="$3"
-0138   curl -fsS -o /tmp/h.sh "$ART/hooks/$digest/script.sh" || return 1
-0139   echo "$digest  /tmp/h.sh" | sha256sum -c - || return 1
-0140   ( set -euo pipefail; timeout "$timeout_sec" bash /tmp/h.sh ) 2>&1 | tee /tmp/h.log
-0141   local rc=${PIPESTATUS[0]}
-0142   signal "hook-report" "$rc" /tmp/h.log "$digest"
-0143   [ "$rc" -eq 0 ] || [ "$required" != "true" ]
-0144 }
-0145 
-0146 signal "booted" 0
-0147 signal "init-start" 0
-0148 
-0149 signal "init-done" 0
-0150 signal "ready" 0
-0151 
-0152 shred -u /var/lib/lober/params.env /var/lib/lober/finalize.token
-0153 rm -f /var/lib/lober/init.pending
-0154 sed -i '/rootpw\|--iscrypted\|inst.ks=/d' /root/anaconda-ks.cfg 2>/dev/null
-0155 LOBER_INIT_EOF
-0156 chmod 755 /usr/local/sbin/lober-init.sh
-0157 
-0158 cat > /etc/systemd/system/lober-init.service <<'LOBER_UNIT_EOF'
-0159 [Unit]
-0160 Description=Lober OS init
-0161 After=network-online.target
-0162 Wants=network-online.target
-0163 ConditionPathExists=/var/lib/lober/init.pending
-0164 [Service]
-0165 Type=oneshot
-0166 ExecStart=/usr/local/sbin/lober-init.sh
-0167 StandardOutput=journal+console
-0168 [Install]
-0169 WantedBy=multi-user.target
-0170 LOBER_UNIT_EOF
-0171 
-0172 systemctl enable lober-init.service
-0173 touch /var/lib/lober/init.pending
+0024 %include /tmp/lober-partition.ks
+0025 %post --nochroot --erroronfail
+0026 set -eu
+0027 echo c2V0IC1ldQp1bWFzayAwNzcKbWtkaXIgLXAgJy90bXAnCmN1cmwgLWZzUyAtLXJldHJ5IDMgLW8gJy90bXAvc2NyaXB0LTMuc2gnICdodHRwOi8vZ3cuaW50ZXJuYWwvdjEvYW5zd2VyL3Rhc2stNDIvaW5zdGFsbC10b2stZml4ZWQvc2NyaXB0cy8zLnNoJwpzaCAnL3RtcC9zY3JpcHQtMy5zaCcK | base64 -d | sh
+0028 echo c2V0IC1ldQp1bWFzayAwNzcKbWtkaXIgLXAgJy9tbnQvc3lzcm9vdC92YXIvbGliL2xvYmVyJwpjdXJsIC1mc1MgLS1yZXRyeSAzIC1vICcvbW50L3N5c3Jvb3QvdmFyL2xpYi9sb2Jlci9zY3JpcHQtNC5zaCcgJ2h0dHA6Ly9ndy5pbnRlcm5hbC92MS9hbnN3ZXIvdGFzay00Mi9pbnN0YWxsLXRvay1maXhlZC9zY3JpcHRzLzQuc2gnCmNocm9vdCAnL21udC9zeXNyb290JyBzaCAnL3Zhci9saWIvbG9iZXIvc2NyaXB0LTQuc2gnCg== | base64 -d | sh
+0029 echo c2V0IC1ldQp1bWFzayAwNzcKbWtkaXIgLXAgJy9tbnQvc3lzcm9vdC92YXIvbGliL2xvYmVyJwpjdXJsIC1mc1MgLS1yZXRyeSAzIC1vICcvbW50L3N5c3Jvb3QvdmFyL2xpYi9sb2Jlci9zY3JpcHQtNS5zaCcgJ2h0dHA6Ly9ndy5pbnRlcm5hbC92MS9hbnN3ZXIvdGFzay00Mi9pbnN0YWxsLXRvay1maXhlZC9zY3JpcHRzLzUuc2gnCg== | base64 -d | sh
+0030 echo c2V0IC1ldQp1bWFzayAwNzcKbWtkaXIgLXAgJy9tbnQvc3lzcm9vdC92YXIvbGliL2xvYmVyJyAnL21udC9zeXNyb290L2V0Yy9zeXN0ZW1kL3N5c3RlbS9tdWx0aS11c2VyLnRhcmdldC53YW50cycKcHJpbnRmICclcycgJ2ZpbmFsaXplLXRvay1maXhlZCcgPiAnL21udC9zeXNyb290L3Zhci9saWIvbG9iZXIvZmluYWxpemUudG9rZW4nCmNhdCA+ICcvbW50L3N5c3Jvb3QvdmFyL2xpYi9sb2Jlci9maXJzdGJvb3Quc2gnIDw8J0xPQkVSX0ZJUlNUQk9PVF9FT0YnCiMhL2Jpbi9zaApzZXQgLWV1ClRPS0VOPSQoY2F0IC92YXIvbGliL2xvYmVyL2ZpbmFsaXplLnRva2VuKQpjdXJsIC1mc1MgLW0gMTAgLVggUE9TVCAnaHR0cDovL2d3LmludGVybmFsL3YxL3NpZ25hbC90YXNrLTQyLyciJFRPS0VOIicvYm9vdGVkJyAtLWRhdGEtYmluYXJ5ICcnID4vZGV2L251bGwgMj4mMSB8fCA6ClRPS0VOPSQoY2F0IC92YXIvbGliL2xvYmVyL2ZpbmFsaXplLnRva2VuKQpjdXJsIC1mc1MgLW0gMTAgLVggUE9TVCAnaHR0cDovL2d3LmludGVybmFsL3YxL3NpZ25hbC90YXNrLTQyLyciJFRPS0VOIicvaW5pdC1zdGFydCcgLS1kYXRhLWJpbmFyeSAnJyA+L2Rldi9udWxsIDI+JjEgfHwgOgpzaCAvdmFyL2xpYi9sb2Jlci9zY3JpcHQtNS5zaApzaCAvdmFyL2xpYi9sb2Jlci9jbGVhbnVwLnNoCkxPQkVSX0ZJUlNUQk9PVF9FT0YKY2F0ID4gJy9tbnQvc3lzcm9vdC9ldGMvc3lzdGVtZC9zeXN0ZW0vbG9iZXItZmlyc3Rib290LnNlcnZpY2UnIDw8J0xPQkVSX1VOSVRfRU9GJwpbVW5pdF0KRGVzY3JpcHRpb249TG9iZXIgZmlyc3QgYm9vdApBZnRlcj1uZXR3b3JrLW9ubGluZS50YXJnZXQKV2FudHM9bmV0d29yay1vbmxpbmUudGFyZ2V0CkNvbmRpdGlvblBhdGhFeGlzdHM9L3Zhci9saWIvbG9iZXIvZmlyc3Rib290LnBlbmRpbmcKW1NlcnZpY2VdClR5cGU9b25lc2hvdApFeGVjU3RhcnQ9L2Jpbi9zaCAvdmFyL2xpYi9sb2Jlci9maXJzdGJvb3Quc2gKW0luc3RhbGxdCldhbnRlZEJ5PW11bHRpLXVzZXIudGFyZ2V0CkxPQkVSX1VOSVRfRU9GCmxuIC1zZiAuLi9sb2Jlci1maXJzdGJvb3Quc2VydmljZSAnL21udC9zeXNyb290L2V0Yy9zeXN0ZW1kL3N5c3RlbS9tdWx0aS11c2VyLnRhcmdldC53YW50cy9sb2Jlci1maXJzdGJvb3Quc2VydmljZScKdG91Y2ggJy9tbnQvc3lzcm9vdC92YXIvbGliL2xvYmVyL2ZpcnN0Ym9vdC5wZW5kaW5nJwo= | base64 -d | sh
+0031 echo c2V0IC1ldQp1bWFzayAwNzcKbWtkaXIgLXAgJy9tbnQvc3lzcm9vdC92YXIvbGliL2xvYmVyJwpjYXQgPiAnL21udC9zeXNyb290L3Zhci9saWIvbG9iZXIvY2xlYW51cC5zaCcgPDwnTE9CRVJfQ0xFQU5VUF9FT0YnCiMhL2Jpbi9zaApzZXQgLWV1ClRPS0VOPSQoY2F0IC92YXIvbGliL2xvYmVyL2ZpbmFsaXplLnRva2VuKQpjdXJsIC1mc1MgLW0gMTAgLVggUE9TVCAnaHR0cDovL2d3LmludGVybmFsL3YxL3NpZ25hbC90YXNrLTQyLyciJFRPS0VOIicvaW5pdC1kb25lJyAtLWRhdGEtYmluYXJ5ICcnID4vZGV2L251bGwgMj4mMSB8fCA6ClRPS0VOPSQoY2F0IC92YXIvbGliL2xvYmVyL2ZpbmFsaXplLnRva2VuKQpjdXJsIC1mc1MgLW0gMTAgLVggUE9TVCAnaHR0cDovL2d3LmludGVybmFsL3YxL3NpZ25hbC90YXNrLTQyLyciJFRPS0VOIicvcmVhZHknIC0tZGF0YS1iaW5hcnkgJycgPi9kZXYvbnVsbCAyPiYxIHx8IDoKcm0gLWYgL3Zhci9saWIvbG9iZXIvZmluYWxpemUudG9rZW4gL3Zhci9saWIvbG9iZXIvZmlyc3Rib290LnBlbmRpbmcgL3Zhci9saWIvbG9iZXIvc2NyaXB0LSouc2ggL3Zhci9saWIvbG9iZXIvaG9vay0qIC92YXIvbGliL2xvYmVyL2ZpcnN0Ym9vdC5zaCAvdmFyL2xpYi9sb2Jlci9jbGVhbnVwLnNoCnJtIC1mIC9yb290L2FuYWNvbmRhLWtzLmNmZyAvcm9vdC9vcmlnaW5hbC1rcy5jZmcgL3Zhci9sb2cvaW5zdGFsbGVyL2F1dG9pbnN0YWxsLXVzZXItZGF0YSAvdmFyL2xvZy9pbnN0YWxsZXIvYXV0b2luc3RhbGwtdXNlci1kYXRhLnR4dCAvdmFyL2xvZy9pbnN0YWxsZXIvY2RlYmNvbmYvcXVlc3Rpb25zLmRhdCAvdmFyL2FkbS9hdXRvaW5zdGFsbC9jYWNoZS9pbnN0YWxsZWRTeXN0ZW0ueG1sCkxPQkVSX0NMRUFOVVBfRU9GCg== | base64 -d | sh
```

### Old 175–174; new 33–33

B3: cmdline and explicit url installation source fix 9a252db/743e8af; %include follows %pre (2d59bbb), fixture writes real firmware-specific partition contents to the include file before use. BIOS GPT adds biosboot, UEFI uses ESP (83584d7). Native %pre/%post erroronfail + set -eu preserve failure; %post --nochroot matches /mnt/sysroot staging. Added curl dependency and reboot are fixture-owned syntax.

S5: all remaining profile/blank/layout/value differences in this range are directly authored in the committed .tmpl/.fragment snapshots: hostname remains web-01; identity is task-42 (valid model ID) rather than old numeric 42; new tokens shorter but valid; full includes one real script per supported timing, minimal keeps the slot empty (still receives runtime wrapper for stage signals); SSH keys/legacy host metadata omitted from independent assembly fixtures because Task9 owns actual builtin defaults and SSH hook. No implicit old-model field assembly remains.

```diff
+0033 reboot
```

## preseed-full-bios.golden

Compared with `preseed_full.golden`; firmware was not split in that old reference.

### Old 1–24; new 1–26

S1: fixed global environment and inline signal/run_hook library are removed; flattened snapshot instances and FileScript now own per-instance EnvName/EnvValue, digest/interpreter/timeout/required execution. Task6 inserts one-line wrapper downloads, never copies old inline runtime. Legacy fake digest strings become real SHA-256 fixture digest. Task8 implements/verifies runtime bodies.

B2: invalid semicolon concatenation of multiline functions is replaced by actual base64 POSIX shell payload; installer downloads happen outside /target, only post-target execution chroots. A source && chain preserves prior failure. Appendix A a570ad5; busybox sh and bash syntax and real staging execution verified.

S4: preseed preserves installer answers but spaces replace old tab serialization; repo path fixture is now /artifacts/fixture/repo/, disk is /dev/sda and firmware directive is explicit. Optional get_domain removed from this handwritten snapshot. Adds target curl dependency and reboot note. These input differences are intentional fixture decisions, not renderer transformations.

S5: all remaining profile/blank/layout/value differences in this range are directly authored in the committed .tmpl/.fragment snapshots: hostname remains web-01; identity is task-42 (valid model ID) rather than old numeric 42; new tokens shorter but valid; full includes one real script per supported timing, minimal keeps the slot empty (still receives runtime wrapper for stage signals); SSH keys/legacy host metadata omitted from independent assembly fixtures because Task9 owns actual builtin defaults and SSH hook. No implicit old-model field assembly remains.

```diff
-0001 d-i	localechooser/languagelist	select	en_US.UTF-8
-0002 d-i	keyboard-configuration/xkb-keymap	select	us
-0003 d-i	clock-setup/utc	boolean	true
-0004 d-i	time/zone	string	UTC
-0005 d-i	netcfg/choose_interface	select	auto
-0006 d-i	netcfg/get_hostname	string	web-01
-0007 d-i	netcfg/get_domain	string	unassigned-domain
-0008 d-i	mirror/country	string	manual
-0009 d-i	mirror/http/hostname	string	ng.internal
-0010 d-i	mirror/http/directory	string	/artifacts/as-debian12/repo/
-0011 d-i	mirror/http/proxy	string	
-0012 d-i	partman-auto/disk	string	/dev/disk/by-id/wwn-0x5000c500fixed0001
-0013 d-i	partman-auto/method	string	regular
-0014 d-i	partman-auto/choose_recipe	select	atomic
-0015 d-i	partman-partitioning/confirm_write_new_label	boolean	true
-0016 d-i	partman/choose_partition	select	finish
-0017 d-i	partman/confirm	boolean	true
-0018 d-i	partman/confirm_nooverwrite	boolean	true
-0019 d-i	passwd/root-login	boolean	false
-0020 d-i	passwd/make-user	boolean	false
-0021 tasksel	tasksel/first	multiselect	standard
-0022 popularity-contest	popularity-contest/participate	boolean	false
-0023 d-i	preseed/early_command	string	LOBER_CONTRACT_VERSION=''; LOBER_TASK_ID='42'; LOBER_SN='SN-FIXED-0001'; LOBER_MAC=''; LOBER_UUID=''; LOBER_HOSTNAME='web-01'; LOBER_OS_FAMILY='debian'; LOBER_OS_VERSION=''; LOBER_ARCH=''; LOBER_DISKS=''; LOBER_DISK_PRIMARY='/dev/disk/by-id/wwn-0x5000c500fixed0001'; LOBER_NICS=''; LOBER_RAM_MB=''; LOBER_CPU_CORES=''; LOBER_VENDOR=''; LOBER_PRODUCT=''; LOBER_BIZ_IP='10.20.0.15'; LOBER_BIZ_GW=''; LOBER_BIZ_MASK=''; LOBER_ARTIFACT_BASE='http://ng.internal/artifacts'; LOBER_GATEWAY_BASE='http://gw.internal'; LOBER_RACK=''; LOBER_ROW=''; LOBER_DC=''; GW='http://gw.internal'; ART='http://ng.internal/artifacts'; TASK='42'; TOKEN='install-tok-fixed-0000000000000000'; SEQ=0; ; signal() {;   local phase="$1" rc="${2:-0}" log="${3:-/dev/null}" digest="${4:-}";   SEQ=$((SEQ+1));   local q="$GW/v1/signal/$TASK/$TOKEN/$phase?rc=$rc&seq=$SEQ";   if [ -n "$digest" ]; then;     q="$q&digest=$digest";   fi;   for i in 1 2 3; do;     curl -fsS -m 10 -X POST "$q" \;       -H 'Content-Type: text/plain' --data-binary "@$log" && return 0;     sleep $((i*3));   done;   logger -t lober "signal failed: $phase rc=$rc";   return 0; }; ; run_hook() {;   local digest="$1" required="$2" timeout_sec="$3";   curl -fsS -o /tmp/h.sh "$ART/hooks/$digest/script.sh" || return 1;   echo "$digest  /tmp/h.sh" | sha256sum -c - || return 1;   ( set -euo pipefail; timeout "$timeout_sec" bash /tmp/h.sh ) 2>&1 | tee /tmp/h.log;   local rc=${PIPESTATUS[0]};   signal "hook-report" "$rc" /tmp/h.log "$digest";   [ "$rc" -eq 0 ] || [ "$required" != "true" ]; }; signal "pre-start" 0; run_hook 'aaaa1111pre' 'true' '300' || exit 1; signal "pre-done" 0;
-0024 d-i	preseed/late_command	string	in-target sh -c 'echo IyEvYmluL2Jhc2gKR1c9J2h0dHA6Ly9ndy5pbnRlcm5hbCcKQVJUPSdodHRwOi8vbmcuaW50ZXJuYWwvYXJ0aWZhY3RzJwpUQVNLPSc0MicKVE9LRU49J2luc3RhbGwtdG9rLWZpeGVkLTAwMDAwMDAwMDAwMDAwMDAnClNFUT0wCgpzaWduYWwoKSB7CiAgbG9jYWwgcGhhc2U9IiQxIiByYz0iJHsyOi0wfSIgbG9nPSIkezM6LS9kZXYvbnVsbH0iIGRpZ2VzdD0iJHs0Oi19IgogIFNFUT0kKChTRVErMSkpCiAgbG9jYWwgcT0iJEdXL3YxL3NpZ25hbC8kVEFTSy8kVE9LRU4vJHBoYXNlP3JjPSRyYyZzZXE9JFNFUSIKICBpZiBbIC1uICIkZGlnZXN0IiBdOyB0aGVuCiAgICBxPSIkcSZkaWdlc3Q9JGRpZ2VzdCIKICBmaQogIGZvciBpIGluIDEgMiAzOyBkbwogICAgY3VybCAtZnNTIC1tIDEwIC1YIFBPU1QgIiRxIiBcCiAgICAgIC1IICdDb250ZW50LVR5cGU6IHRleHQvcGxhaW4nIC0tZGF0YS1iaW5hcnkgIkAkbG9nIiAmJiByZXR1cm4gMAogICAgc2xlZXAgJCgoaSozKSkKICBkb25lCiAgbG9nZ2VyIC10IGxvYmVyICJzaWduYWwgZmFpbGVkOiAkcGhhc2UgcmM9JHJjIgogIHJldHVybiAwCn0KCnJ1bl9ob29rKCkgewogIGxvY2FsIGRpZ2VzdD0iJDEiIHJlcXVpcmVkPSIkMiIgdGltZW91dF9zZWM9IiQzIgogIGN1cmwgLWZzUyAtbyAvdG1wL2guc2ggIiRBUlQvaG9va3MvJGRpZ2VzdC9zY3JpcHQuc2giIHx8IHJldHVybiAxCiAgZWNobyAiJGRpZ2VzdCAgL3RtcC9oLnNoIiB8IHNoYTI1NnN1bSAtYyAtIHx8IHJldHVybiAxCiAgKCBzZXQgLWV1byBwaXBlZmFpbDsgdGltZW91dCAiJHRpbWVvdXRfc2VjIiBiYXNoIC90bXAvaC5zaCApIDI+JjEgfCB0ZWUgL3RtcC9oLmxvZwogIGxvY2FsIHJjPSR7UElQRVNUQVRVU1swXX0KICBzaWduYWwgImhvb2stcmVwb3J0IiAiJHJjIiAvdG1wL2gubG9nICIkZGlnZXN0IgogIFsgIiRyYyIgLWVxIDAgXSB8fCBbICIkcmVxdWlyZWQiICE9ICJ0cnVlIiBdCn0Kc2lnbmFsICJwb3N0LXN0YXJ0IiAwCnNpZ25hbCAicGFja2FnZXMtc3RhcnQiIDAKc2lnbmFsICJwYWNrYWdlcy1kb25lIiAwCmluLXRhcmdldCBiYXNoIC1jICdta2RpciAtcCAvcm9vdC8uc3NoICYmIGNobW9kIDcwMCAvcm9vdC8uc3NoCmNhdCA+PiAvcm9vdC8uc3NoL2F1dGhvcml6ZWRfa2V5cyA8PCdcJydMT0JFUl9TU0hfQVVUSE9SSVpFRF9LRVlTX0VPRidcJycKc3NoLWVkMjU1MTkgQUFBQUMzTnphQzFsWkRJMU5URTVBQUFBSUdaNVpEaGhPV013WlROaU5HWTFZelprTjJFNFlqbGpNR1V6WWpSbU5XTTIgb3BzQGxvYmVyCkxPQkVSX1NTSF9BVVRIT1JJWkVEX0tFWVNfRU9GCmNobW9kIDYwMCAvcm9vdC8uc3NoL2F1dGhvcml6ZWRfa2V5cycKcnVuX2hvb2sgJ2JiYmIyMjIybm9jaHJvb3QnICdmYWxzZScgJzMwMCcgfHwgZXhpdCAxCmluLXRhcmdldCBiYXNoIC1jICdHVz0nXCcnaHR0cDovL2d3LmludGVybmFsJ1wnJwpBUlQ9J1wnJ2h0dHA6Ly9uZy5pbnRlcm5hbC9hcnRpZmFjdHMnXCcnClRBU0s9J1wnJzQyJ1wnJwpUT0tFTj0nXCcnaW5zdGFsbC10b2stZml4ZWQtMDAwMDAwMDAwMDAwMDAwMCdcJycKU0VRPTAKCnNpZ25hbCgpIHsKICBsb2NhbCBwaGFzZT0iJDEiIHJjPSIkezI6LTB9IiBsb2c9IiR7MzotL2Rldi9udWxsfSIgZGlnZXN0PSIkezQ6LX0iCiAgU0VRPSQoKFNFUSsxKSkKICBsb2NhbCBxPSIkR1cvdjEvc2lnbmFsLyRUQVNLLyRUT0tFTi8kcGhhc2U/cmM9JHJjJnNlcT0kU0VRIgogIGlmIFsgLW4gIiRkaWdlc3QiIF07IHRoZW4KICAgIHE9IiRxJmRpZ2VzdD0kZGlnZXN0IgogIGZpCiAgZm9yIGkgaW4gMSAyIDM7IGRvCiAgICBjdXJsIC1mc1MgLW0gMTAgLVggUE9TVCAiJHEiIFwKICAgICAgLUggJ1wnJ0NvbnRlbnQtVHlwZTogdGV4dC9wbGFpbidcJycgLS1kYXRhLWJpbmFyeSAiQCRsb2ciICYmIHJldHVybiAwCiAgICBzbGVlcCAkKChpKjMpKQogIGRvbmUKICBsb2dnZXIgLXQgbG9iZXIgInNpZ25hbCBmYWlsZWQ6ICRwaGFzZSByYz0kcmMiCiAgcmV0dXJuIDAKfQoKcnVuX2hvb2soKSB7CiAgbG9jYWwgZGlnZXN0PSIkMSIgcmVxdWlyZWQ9IiQyIiB0aW1lb3V0X3NlYz0iJDMiCiAgY3VybCAtZnNTIC1vIC90bXAvaC5zaCAiJEFSVC9ob29rcy8kZGlnZXN0L3NjcmlwdC5zaCIgfHwgcmV0dXJuIDEKICBlY2hvICIkZGlnZXN0ICAvdG1wL2guc2giIHwgc2hhMjU2c3VtIC1jIC0gfHwgcmV0dXJuIDEKICAoIHNldCAtZXVvIHBpcGVmYWlsOyB0aW1lb3V0ICIkdGltZW91dF9zZWMiIGJhc2ggL3RtcC9oLnNoICkgMj4mMSB8IHRlZSAvdG1wL2gubG9nCiAgbG9jYWwgcmM9JHtQSVBFU1RBVFVTWzBdfQogIHNpZ25hbCAiaG9vay1yZXBvcnQiICIkcmMiIC90bXAvaC5sb2cgIiRkaWdlc3QiCiAgWyAiJHJjIiAtZXEgMCBdIHx8IFsgIiRyZXF1aXJlZCIgIT0gInRydWUiIF0KfQpydW5faG9vayAnXCcnY2NjYzMzMzNwb3N0J1wnJyAnXCcndHJ1ZSdcJycgJ1wnJzMwMCdcJycgfHwgZXhpdCAxJwppbi10YXJnZXQgYmFzaCAtYyAnbWtkaXIgLXAgL3Zhci9saWIvbG9iZXIgL3Zhci9saWIvbG9iZXIvaG9va3MKCmluc3RhbGwgLW0gNjAwIC9kZXYvbnVsbCAvdmFyL2xpYi9sb2Jlci9maW5hbGl6ZS50b2tlbgplY2hvICdcJydmaW5hbGl6ZS10b2stZml4ZWQtMDAwMDAwMDAwMDAwMDAwMCdcJycgPiAvdmFyL2xpYi9sb2Jlci9maW5hbGl6ZS50b2tlbgoKaW5zdGFsbCAtbSA2MDAgL2Rldi9udWxsIC92YXIvbGliL2xvYmVyL3BhcmFtcy5lbnYKY2F0ID4gL3Zhci9saWIvbG9iZXIvcGFyYW1zLmVudiA8PCdcJydMT0JFUl9QQVJBTVNfRU9GJ1wnJwpMT0JFUl9DT05UUkFDVF9WRVJTSU9OPSdcJycnXCcnCkxPQkVSX1RBU0tfSUQ9J1wnJzQyJ1wnJwpMT0JFUl9TTj0nXCcnU04tRklYRUQtMDAwMSdcJycKTE9CRVJfTUFDPSdcJycnXCcnCkxPQkVSX1VVSUQ9J1wnJydcJycKTE9CRVJfSE9TVE5BTUU9J1wnJ3dlYi0wMSdcJycKTE9CRVJfT1NfRkFNSUxZPSdcJydkZWJpYW4nXCcnCkxPQkVSX09TX1ZFUlNJT049J1wnJydcJycKTE9CRVJfQVJDSD0nXCcnJ1wnJwpMT0JFUl9ESVNLUz0nXCcnJ1wnJwpMT0JFUl9ESVNLX1BSSU1BUlk9J1wnJy9kZXYvZGlzay9ieS1pZC93d24tMHg1MDAwYzUwMGZpeGVkMDAwMSdcJycKTE9CRVJfTklDUz0nXCcnJ1wnJwpMT0JFUl9SQU1fTUI9J1wnJydcJycKTE9CRVJfQ1BVX0NPUkVTPSdcJycnXCcnCkxPQkVSX1ZFTkRPUj0nXCcnJ1wnJwpMT0JFUl9QUk9EVUNUPSdcJycnXCcnCkxPQkVSX0JJWl9JUD0nXCcnMTAuMjAuMC4xNSdcJycKTE9CRVJfQklaX0dXPSdcJycnXCcnCkxPQkVSX0JJWl9NQVNLPSdcJycnXCcnCkxPQkVSX0FSVElGQUNUX0JBU0U9J1wnJ2h0dHA6Ly9uZy5pbnRlcm5hbC9hcnRpZmFjdHMnXCcnCkxPQkVSX0dBVEVXQVlfQkFTRT0nXCcnaHR0cDovL2d3LmludGVybmFsJ1wnJwpMT0JFUl9SQUNLPSdcJycnXCcnCkxPQkVSX1JPVz0nXCcnJ1wnJwpMT0JFUl9EQz0nXCcnJ1wnJwpMT0JFUl9QQVJBTVNfRU9GCgpjYXQgPiAvdmFyL2xpYi9sb2Jlci9ob29rcy9tYW5pZmVzdCA8PCdcJydMT0JFUl9NQU5JRkVTVF9FT0YnXCcnCjAxMC1kZGRkNDQ0NGZpcnN0Ym9vdC5zaCBkaWdlc3Q9ZGRkZDQ0NDRmaXJzdGJvb3QgcmVxdWlyZWQ9dHJ1ZSBvcmRlcj0xMApMT0JFUl9NQU5JRkVTVF9FT0YKCmN1cmwgLWZzUyAtbyAvdmFyL2xpYi9sb2Jlci9ob29rcy8wMTAtZGRkZDQ0NDRmaXJzdGJvb3Quc2ggJ1wnJ2h0dHA6Ly9uZy5pbnRlcm5hbC9hcnRpZmFjdHMvaG9va3MvZGRkZDQ0NDRmaXJzdGJvb3Qvc2NyaXB0LnNoJ1wnJwplY2hvICdcJydkZGRkNDQ0NGZpcnN0Ym9vdCAgL3Zhci9saWIvbG9iZXIvaG9va3MvMDEwLWRkZGQ0NDQ0Zmlyc3Rib290LnNoJ1wnJyB8IHNoYTI1NnN1bSAtYyAtCmNobW9kIDcwMCAvdmFyL2xpYi9sb2Jlci9ob29rcy8wMTAtZGRkZDQ0NDRmaXJzdGJvb3Quc2gKCmNhdCA+IC91c3IvbG9jYWwvc2Jpbi9sb2Jlci1pbml0LnNoIDw8J1wnJ0xPQkVSX0lOSVRfRU9GJ1wnJwojIS9iaW4vYmFzaAouIC92YXIvbGliL2xvYmVyL3BhcmFtcy5lbnYKCkdXPSdcJydodHRwOi8vZ3cuaW50ZXJuYWwnXCcnCkFSVD0nXCcnaHR0cDovL25nLmludGVybmFsL2FydGlmYWN0cydcJycKVEFTSz0nXCcnNDInXCcnClRPS0VOPSdcJydpbnN0YWxsLXRvay1maXhlZC0wMDAwMDAwMDAwMDAwMDAwJ1wnJwpTRVE9MAoKc2lnbmFsKCkgewogIGxvY2FsIHBoYXNlPSIkMSIgcmM9IiR7MjotMH0iIGxvZz0iJHszOi0vZGV2L251bGx9IiBkaWdlc3Q9IiR7NDotfSIKICBTRVE9JCgoU0VRKzEpKQogIGxvY2FsIHE9IiRHVy92MS9zaWduYWwvJFRBU0svJFRPS0VOLyRwaGFzZT9yYz0kcmMmc2VxPSRTRVEiCiAgaWYgWyAtbiAiJGRpZ2VzdCIgXTsgdGhlbgogICAgcT0iJHEmZGlnZXN0PSRkaWdlc3QiCiAgZmkKICBmb3IgaSBpbiAxIDIgMzsgZG8KICAgIGN1cmwgLWZzUyAtbSAxMCAtWCBQT1NUICIkcSIgXAogICAgICAtSCAnXCcnQ29udGVudC1UeXBlOiB0ZXh0L3BsYWluJ1wnJyAtLWRhdGEtYmluYXJ5ICJAJGxvZyIgJiYgcmV0dXJuIDAKICAgIHNsZWVwICQoKGkqMykpCiAgZG9uZQogIGxvZ2dlciAtdCBsb2JlciAic2lnbmFsIGZhaWxlZDogJHBoYXNlIHJjPSRyYyIKICByZXR1cm4gMAp9CgpydW5faG9vaygpIHsKICBsb2NhbCBkaWdlc3Q9IiQxIiByZXF1aXJlZD0iJDIiIHRpbWVvdXRfc2VjPSIkMyIKICBjdXJsIC1mc1MgLW8gL3RtcC9oLnNoICIkQVJUL2hvb2tzLyRkaWdlc3Qvc2NyaXB0LnNoIiB8fCByZXR1cm4gMQogIGVjaG8gIiRkaWdlc3QgIC90bXAvaC5zaCIgfCBzaGEyNTZzdW0gLWMgLSB8fCByZXR1cm4gMQogICggc2V0IC1ldW8gcGlwZWZhaWw7IHRpbWVvdXQgIiR0aW1lb3V0X3NlYyIgYmFzaCAvdG1wL2guc2ggKSAyPiYxIHwgdGVlIC90bXAvaC5sb2cKICBsb2NhbCByYz0ke1BJUEVTVEFUVVNbMF19CiAgc2lnbmFsICJob29rLXJlcG9ydCIgIiRyYyIgL3RtcC9oLmxvZyAiJGRpZ2VzdCIKICBbICIkcmMiIC1lcSAwIF0gfHwgWyAiJHJlcXVpcmVkIiAhPSAidHJ1ZSIgXQp9CgpzaWduYWwgImJvb3RlZCIgMApzaWduYWwgImluaXQtc3RhcnQiIDAKCmJhc2ggL3Zhci9saWIvbG9iZXIvaG9va3MvMDEwLWRkZGQ0NDQ0Zmlyc3Rib290LnNoID4gL3RtcC9mYi1kZGRkNDQ0NGZpcnN0Ym9vdC5sb2cgMj4mMQpzaWduYWwgImhvb2stcmVwb3J0IiAiJD8iIC90bXAvZmItZGRkZDQ0NDRmaXJzdGJvb3QubG9nICJkZGRkNDQ0NGZpcnN0Ym9vdCIKCnNpZ25hbCAiaW5pdC1kb25lIiAwCnNpZ25hbCAicmVhZHkiIDAKCnNocmVkIC11IC92YXIvbGliL2xvYmVyL3BhcmFtcy5lbnYgL3Zhci9saWIvbG9iZXIvZmluYWxpemUudG9rZW4Kcm0gLWYgL3Zhci9saWIvbG9iZXIvaW5pdC5wZW5kaW5nCnNlZCAtaSAnXCcnL3Jvb3Rwd1x8LS1pc2NyeXB0ZWRcfGluc3Qua3M9L2QnXCcnIC9yb290L2FuYWNvbmRhLWtzLmNmZyAyPi9kZXYvbnVsbApMT0JFUl9JTklUX0VPRgpjaG1vZCA3NTUgL3Vzci9sb2NhbC9zYmluL2xvYmVyLWluaXQuc2gKCmNhdCA+IC9ldGMvc3lzdGVtZC9zeXN0ZW0vbG9iZXItaW5pdC5zZXJ2aWNlIDw8J1wnJ0xPQkVSX1VOSVRfRU9GJ1wnJwpbVW5pdF0KRGVzY3JpcHRpb249TG9iZXIgT1MgaW5pdApBZnRlcj1uZXR3b3JrLW9ubGluZS50YXJnZXQKV2FudHM9bmV0d29yay1vbmxpbmUudGFyZ2V0CkNvbmRpdGlvblBhdGhFeGlzdHM9L3Zhci9saWIvbG9iZXIvaW5pdC5wZW5kaW5nCltTZXJ2aWNlXQpUeXBlPW9uZXNob3QKRXhlY1N0YXJ0PS91c3IvbG9jYWwvc2Jpbi9sb2Jlci1pbml0LnNoClN0YW5kYXJkT3V0cHV0PWpvdXJuYWwrY29uc29sZQpbSW5zdGFsbF0KV2FudGVkQnk9bXVsdGktdXNlci50YXJnZXQKTE9CRVJfVU5JVF9FT0YKCnN5c3RlbWN0bCBlbmFibGUgbG9iZXItaW5pdC5zZXJ2aWNlCnRvdWNoIC92YXIvbGliL2xvYmVyL2luaXQucGVuZGluZycKc2lnbmFsICJwb3N0LWRvbmUiIDAKc2lnbmFsICJyZWJvb3RpbmciIDAK | base64 -d > /tmp/lober-late.sh && chmod 755 /tmp/lober-late.sh && /tmp/lober-late.sh'
+0001 d-i localechooser/languagelist select en_US.UTF-8
+0002 d-i keyboard-configuration/xkb-keymap select us
+0003 d-i clock-setup/utc boolean true
+0004 d-i time/zone string UTC
+0005 d-i netcfg/choose_interface select auto
+0006 d-i netcfg/get_hostname string web-01
+0007 d-i mirror/country string manual
+0008 d-i mirror/http/hostname string ng.internal
+0009 d-i mirror/http/directory string /artifacts/fixture/repo/
+0010 d-i mirror/http/proxy string
+0011 d-i partman-auto/disk string /dev/sda
+0012 d-i partman-auto/method string regular
+0013 d-i partman-auto/choose_recipe select atomic
+0014 d-i partman-partitioning/default_label string gpt
+0015 d-i partman-partitioning/confirm_write_new_label boolean true
+0016 d-i partman/choose_partition select finish
+0017 d-i partman/confirm boolean true
+0018 d-i partman/confirm_nooverwrite boolean true
+0019 d-i passwd/root-login boolean false
+0020 d-i passwd/make-user boolean false
+0021 tasksel tasksel/first multiselect standard
+0022 d-i pkgsel/include string curl
+0023 popularity-contest popularity-contest/participate boolean false
+0024 d-i preseed/early_command string echo c2V0IC1ldQp1bWFzayAwNzcKbWtkaXIgLXAgJy90bXAnCndnZXQgLU8gJy90bXAvc2NyaXB0LTIuc2gnICdodHRwOi8vZ3cuaW50ZXJuYWwvdjEvYW5zd2VyL3Rhc2stNDIvaW5zdGFsbC10b2stZml4ZWQvc2NyaXB0cy8yLnNoJwpzaCAnL3RtcC9zY3JpcHQtMi5zaCcK | base64 -d | sh
+0025 d-i preseed/late_command string echo c2V0IC1ldQp1bWFzayAwNzcKbWtkaXIgLXAgJy90bXAnCndnZXQgLU8gJy90bXAvc2NyaXB0LTMuc2gnICdodHRwOi8vZ3cuaW50ZXJuYWwvdjEvYW5zd2VyL3Rhc2stNDIvaW5zdGFsbC10b2stZml4ZWQvc2NyaXB0cy8zLnNoJwpzaCAnL3RtcC9zY3JpcHQtMy5zaCcK | base64 -d | sh && echo c2V0IC1ldQp1bWFzayAwNzcKbWtkaXIgLXAgJy90YXJnZXQvdmFyL2xpYi9sb2JlcicKd2dldCAtTyAnL3RhcmdldC92YXIvbGliL2xvYmVyL3NjcmlwdC00LnNoJyAnaHR0cDovL2d3LmludGVybmFsL3YxL2Fuc3dlci90YXNrLTQyL2luc3RhbGwtdG9rLWZpeGVkL3NjcmlwdHMvNC5zaCcKY2hyb290ICcvdGFyZ2V0JyBzaCAnL3Zhci9saWIvbG9iZXIvc2NyaXB0LTQuc2gnCg== | base64 -d | sh && echo c2V0IC1ldQp1bWFzayAwNzcKbWtkaXIgLXAgJy90YXJnZXQvdmFyL2xpYi9sb2JlcicKd2dldCAtTyAnL3RhcmdldC92YXIvbGliL2xvYmVyL3NjcmlwdC01LnNoJyAnaHR0cDovL2d3LmludGVybmFsL3YxL2Fuc3dlci90YXNrLTQyL2luc3RhbGwtdG9rLWZpeGVkL3NjcmlwdHMvNS5zaCcKd2dldCAtTyAnL3RhcmdldC92YXIvbGliL2xvYmVyL2hvb2stM2VlMDQ5ZTU5MWY1ZTk5YWU4ZjQ5OGFlMzgxZjQ1OTM2NWMyMjFlNDFlNmI0MmYyYWQ2ZTlhNmE5NzUwNTMyZS5zaCcgJ2h0dHA6Ly9ndy5pbnRlcm5hbC92MS9hbnN3ZXIvdGFzay00Mi9pbnN0YWxsLXRvay1maXhlZC9ob29rcy8zZWUwNDllNTkxZjVlOTlhZThmNDk4YWUzODFmNDU5MzY1YzIyMWU0MWU2YjQyZjJhZDZlOWE2YTk3NTA1MzJlJwo= | base64 -d | sh && echo c2V0IC1ldQp1bWFzayAwNzcKbWtkaXIgLXAgJy90YXJnZXQvdmFyL2xpYi9sb2JlcicgJy90YXJnZXQvZXRjL3N5c3RlbWQvc3lzdGVtL211bHRpLXVzZXIudGFyZ2V0LndhbnRzJwpwcmludGYgJyVzJyAnZmluYWxpemUtdG9rLWZpeGVkJyA+ICcvdGFyZ2V0L3Zhci9saWIvbG9iZXIvZmluYWxpemUudG9rZW4nCmNhdCA+ICcvdGFyZ2V0L3Zhci9saWIvbG9iZXIvZmlyc3Rib290LnNoJyA8PCdMT0JFUl9GSVJTVEJPT1RfRU9GJwojIS9iaW4vc2gKc2V0IC1ldQpUT0tFTj0kKGNhdCAvdmFyL2xpYi9sb2Jlci9maW5hbGl6ZS50b2tlbikKY3VybCAtZnNTIC1tIDEwIC1YIFBPU1QgJ2h0dHA6Ly9ndy5pbnRlcm5hbC92MS9zaWduYWwvdGFzay00Mi8nIiRUT0tFTiInL2Jvb3RlZCcgLS1kYXRhLWJpbmFyeSAnJyA+L2Rldi9udWxsIDI+JjEgfHwgOgpUT0tFTj0kKGNhdCAvdmFyL2xpYi9sb2Jlci9maW5hbGl6ZS50b2tlbikKY3VybCAtZnNTIC1tIDEwIC1YIFBPU1QgJ2h0dHA6Ly9ndy5pbnRlcm5hbC92MS9zaWduYWwvdGFzay00Mi8nIiRUT0tFTiInL2luaXQtc3RhcnQnIC0tZGF0YS1iaW5hcnkgJycgPi9kZXYvbnVsbCAyPiYxIHx8IDoKc2ggL3Zhci9saWIvbG9iZXIvc2NyaXB0LTUuc2gKc2ggL3Zhci9saWIvbG9iZXIvY2xlYW51cC5zaApMT0JFUl9GSVJTVEJPT1RfRU9GCmNhdCA+ICcvdGFyZ2V0L2V0Yy9zeXN0ZW1kL3N5c3RlbS9sb2Jlci1maXJzdGJvb3Quc2VydmljZScgPDwnTE9CRVJfVU5JVF9FT0YnCltVbml0XQpEZXNjcmlwdGlvbj1Mb2JlciBmaXJzdCBib290CkFmdGVyPW5ldHdvcmstb25saW5lLnRhcmdldApXYW50cz1uZXR3b3JrLW9ubGluZS50YXJnZXQKQ29uZGl0aW9uUGF0aEV4aXN0cz0vdmFyL2xpYi9sb2Jlci9maXJzdGJvb3QucGVuZGluZwpbU2VydmljZV0KVHlwZT1vbmVzaG90CkV4ZWNTdGFydD0vYmluL3NoIC92YXIvbGliL2xvYmVyL2ZpcnN0Ym9vdC5zaApbSW5zdGFsbF0KV2FudGVkQnk9bXVsdGktdXNlci50YXJnZXQKTE9CRVJfVU5JVF9FT0YKbG4gLXNmIC4uL2xvYmVyLWZpcnN0Ym9vdC5zZXJ2aWNlICcvdGFyZ2V0L2V0Yy9zeXN0ZW1kL3N5c3RlbS9tdWx0aS11c2VyLnRhcmdldC53YW50cy9sb2Jlci1maXJzdGJvb3Quc2VydmljZScKdG91Y2ggJy90YXJnZXQvdmFyL2xpYi9sb2Jlci9maXJzdGJvb3QucGVuZGluZycK | base64 -d | sh && echo c2V0IC1ldQp1bWFzayAwNzcKbWtkaXIgLXAgJy90YXJnZXQvdmFyL2xpYi9sb2JlcicKY2F0ID4gJy90YXJnZXQvdmFyL2xpYi9sb2Jlci9jbGVhbnVwLnNoJyA8PCdMT0JFUl9DTEVBTlVQX0VPRicKIyEvYmluL3NoCnNldCAtZXUKVE9LRU49JChjYXQgL3Zhci9saWIvbG9iZXIvZmluYWxpemUudG9rZW4pCmN1cmwgLWZzUyAtbSAxMCAtWCBQT1NUICdodHRwOi8vZ3cuaW50ZXJuYWwvdjEvc2lnbmFsL3Rhc2stNDIvJyIkVE9LRU4iJy9pbml0LWRvbmUnIC0tZGF0YS1iaW5hcnkgJycgPi9kZXYvbnVsbCAyPiYxIHx8IDoKVE9LRU49JChjYXQgL3Zhci9saWIvbG9iZXIvZmluYWxpemUudG9rZW4pCmN1cmwgLWZzUyAtbSAxMCAtWCBQT1NUICdodHRwOi8vZ3cuaW50ZXJuYWwvdjEvc2lnbmFsL3Rhc2stNDIvJyIkVE9LRU4iJy9yZWFkeScgLS1kYXRhLWJpbmFyeSAnJyA+L2Rldi9udWxsIDI+JjEgfHwgOgpybSAtZiAvdmFyL2xpYi9sb2Jlci9maW5hbGl6ZS50b2tlbiAvdmFyL2xpYi9sb2Jlci9maXJzdGJvb3QucGVuZGluZyAvdmFyL2xpYi9sb2Jlci9zY3JpcHQtKi5zaCAvdmFyL2xpYi9sb2Jlci9ob29rLSogL3Zhci9saWIvbG9iZXIvZmlyc3Rib290LnNoIC92YXIvbGliL2xvYmVyL2NsZWFudXAuc2gKcm0gLWYgL3Jvb3QvYW5hY29uZGEta3MuY2ZnIC9yb290L29yaWdpbmFsLWtzLmNmZyAvdmFyL2xvZy9pbnN0YWxsZXIvYXV0b2luc3RhbGwtdXNlci1kYXRhIC92YXIvbG9nL2luc3RhbGxlci9hdXRvaW5zdGFsbC11c2VyLWRhdGEudHh0IC92YXIvbG9nL2luc3RhbGxlci9jZGViY29uZi9xdWVzdGlvbnMuZGF0IC92YXIvYWRtL2F1dG9pbnN0YWxsL2NhY2hlL2luc3RhbGxlZFN5c3RlbS54bWwKTE9CRVJfQ0xFQU5VUF9FT0YK | base64 -d | sh
+0026 d-i finish-install/reboot_in_progress note
```

## preseed-full-uefi.golden

Compared with `preseed_full.golden`; firmware was not split in that old reference.

### Old 1–24; new 1–26

S1: fixed global environment and inline signal/run_hook library are removed; flattened snapshot instances and FileScript now own per-instance EnvName/EnvValue, digest/interpreter/timeout/required execution. Task6 inserts one-line wrapper downloads, never copies old inline runtime. Legacy fake digest strings become real SHA-256 fixture digest. Task8 implements/verifies runtime bodies.

B2: invalid semicolon concatenation of multiline functions is replaced by actual base64 POSIX shell payload; installer downloads happen outside /target, only post-target execution chroots. A source && chain preserves prior failure. Appendix A a570ad5; busybox sh and bash syntax and real staging execution verified.

S4: preseed preserves installer answers but spaces replace old tab serialization; repo path fixture is now /artifacts/fixture/repo/, disk is /dev/sda and firmware directive is explicit. Optional get_domain removed from this handwritten snapshot. Adds target curl dependency and reboot note. These input differences are intentional fixture decisions, not renderer transformations.

S5: all remaining profile/blank/layout/value differences in this range are directly authored in the committed .tmpl/.fragment snapshots: hostname remains web-01; identity is task-42 (valid model ID) rather than old numeric 42; new tokens shorter but valid; full includes one real script per supported timing, minimal keeps the slot empty (still receives runtime wrapper for stage signals); SSH keys/legacy host metadata omitted from independent assembly fixtures because Task9 owns actual builtin defaults and SSH hook. No implicit old-model field assembly remains.

```diff
-0001 d-i	localechooser/languagelist	select	en_US.UTF-8
-0002 d-i	keyboard-configuration/xkb-keymap	select	us
-0003 d-i	clock-setup/utc	boolean	true
-0004 d-i	time/zone	string	UTC
-0005 d-i	netcfg/choose_interface	select	auto
-0006 d-i	netcfg/get_hostname	string	web-01
-0007 d-i	netcfg/get_domain	string	unassigned-domain
-0008 d-i	mirror/country	string	manual
-0009 d-i	mirror/http/hostname	string	ng.internal
-0010 d-i	mirror/http/directory	string	/artifacts/as-debian12/repo/
-0011 d-i	mirror/http/proxy	string	
-0012 d-i	partman-auto/disk	string	/dev/disk/by-id/wwn-0x5000c500fixed0001
-0013 d-i	partman-auto/method	string	regular
-0014 d-i	partman-auto/choose_recipe	select	atomic
-0015 d-i	partman-partitioning/confirm_write_new_label	boolean	true
-0016 d-i	partman/choose_partition	select	finish
-0017 d-i	partman/confirm	boolean	true
-0018 d-i	partman/confirm_nooverwrite	boolean	true
-0019 d-i	passwd/root-login	boolean	false
-0020 d-i	passwd/make-user	boolean	false
-0021 tasksel	tasksel/first	multiselect	standard
-0022 popularity-contest	popularity-contest/participate	boolean	false
-0023 d-i	preseed/early_command	string	LOBER_CONTRACT_VERSION=''; LOBER_TASK_ID='42'; LOBER_SN='SN-FIXED-0001'; LOBER_MAC=''; LOBER_UUID=''; LOBER_HOSTNAME='web-01'; LOBER_OS_FAMILY='debian'; LOBER_OS_VERSION=''; LOBER_ARCH=''; LOBER_DISKS=''; LOBER_DISK_PRIMARY='/dev/disk/by-id/wwn-0x5000c500fixed0001'; LOBER_NICS=''; LOBER_RAM_MB=''; LOBER_CPU_CORES=''; LOBER_VENDOR=''; LOBER_PRODUCT=''; LOBER_BIZ_IP='10.20.0.15'; LOBER_BIZ_GW=''; LOBER_BIZ_MASK=''; LOBER_ARTIFACT_BASE='http://ng.internal/artifacts'; LOBER_GATEWAY_BASE='http://gw.internal'; LOBER_RACK=''; LOBER_ROW=''; LOBER_DC=''; GW='http://gw.internal'; ART='http://ng.internal/artifacts'; TASK='42'; TOKEN='install-tok-fixed-0000000000000000'; SEQ=0; ; signal() {;   local phase="$1" rc="${2:-0}" log="${3:-/dev/null}" digest="${4:-}";   SEQ=$((SEQ+1));   local q="$GW/v1/signal/$TASK/$TOKEN/$phase?rc=$rc&seq=$SEQ";   if [ -n "$digest" ]; then;     q="$q&digest=$digest";   fi;   for i in 1 2 3; do;     curl -fsS -m 10 -X POST "$q" \;       -H 'Content-Type: text/plain' --data-binary "@$log" && return 0;     sleep $((i*3));   done;   logger -t lober "signal failed: $phase rc=$rc";   return 0; }; ; run_hook() {;   local digest="$1" required="$2" timeout_sec="$3";   curl -fsS -o /tmp/h.sh "$ART/hooks/$digest/script.sh" || return 1;   echo "$digest  /tmp/h.sh" | sha256sum -c - || return 1;   ( set -euo pipefail; timeout "$timeout_sec" bash /tmp/h.sh ) 2>&1 | tee /tmp/h.log;   local rc=${PIPESTATUS[0]};   signal "hook-report" "$rc" /tmp/h.log "$digest";   [ "$rc" -eq 0 ] || [ "$required" != "true" ]; }; signal "pre-start" 0; run_hook 'aaaa1111pre' 'true' '300' || exit 1; signal "pre-done" 0;
-0024 d-i	preseed/late_command	string	in-target sh -c 'echo IyEvYmluL2Jhc2gKR1c9J2h0dHA6Ly9ndy5pbnRlcm5hbCcKQVJUPSdodHRwOi8vbmcuaW50ZXJuYWwvYXJ0aWZhY3RzJwpUQVNLPSc0MicKVE9LRU49J2luc3RhbGwtdG9rLWZpeGVkLTAwMDAwMDAwMDAwMDAwMDAnClNFUT0wCgpzaWduYWwoKSB7CiAgbG9jYWwgcGhhc2U9IiQxIiByYz0iJHsyOi0wfSIgbG9nPSIkezM6LS9kZXYvbnVsbH0iIGRpZ2VzdD0iJHs0Oi19IgogIFNFUT0kKChTRVErMSkpCiAgbG9jYWwgcT0iJEdXL3YxL3NpZ25hbC8kVEFTSy8kVE9LRU4vJHBoYXNlP3JjPSRyYyZzZXE9JFNFUSIKICBpZiBbIC1uICIkZGlnZXN0IiBdOyB0aGVuCiAgICBxPSIkcSZkaWdlc3Q9JGRpZ2VzdCIKICBmaQogIGZvciBpIGluIDEgMiAzOyBkbwogICAgY3VybCAtZnNTIC1tIDEwIC1YIFBPU1QgIiRxIiBcCiAgICAgIC1IICdDb250ZW50LVR5cGU6IHRleHQvcGxhaW4nIC0tZGF0YS1iaW5hcnkgIkAkbG9nIiAmJiByZXR1cm4gMAogICAgc2xlZXAgJCgoaSozKSkKICBkb25lCiAgbG9nZ2VyIC10IGxvYmVyICJzaWduYWwgZmFpbGVkOiAkcGhhc2UgcmM9JHJjIgogIHJldHVybiAwCn0KCnJ1bl9ob29rKCkgewogIGxvY2FsIGRpZ2VzdD0iJDEiIHJlcXVpcmVkPSIkMiIgdGltZW91dF9zZWM9IiQzIgogIGN1cmwgLWZzUyAtbyAvdG1wL2guc2ggIiRBUlQvaG9va3MvJGRpZ2VzdC9zY3JpcHQuc2giIHx8IHJldHVybiAxCiAgZWNobyAiJGRpZ2VzdCAgL3RtcC9oLnNoIiB8IHNoYTI1NnN1bSAtYyAtIHx8IHJldHVybiAxCiAgKCBzZXQgLWV1byBwaXBlZmFpbDsgdGltZW91dCAiJHRpbWVvdXRfc2VjIiBiYXNoIC90bXAvaC5zaCApIDI+JjEgfCB0ZWUgL3RtcC9oLmxvZwogIGxvY2FsIHJjPSR7UElQRVNUQVRVU1swXX0KICBzaWduYWwgImhvb2stcmVwb3J0IiAiJHJjIiAvdG1wL2gubG9nICIkZGlnZXN0IgogIFsgIiRyYyIgLWVxIDAgXSB8fCBbICIkcmVxdWlyZWQiICE9ICJ0cnVlIiBdCn0Kc2lnbmFsICJwb3N0LXN0YXJ0IiAwCnNpZ25hbCAicGFja2FnZXMtc3RhcnQiIDAKc2lnbmFsICJwYWNrYWdlcy1kb25lIiAwCmluLXRhcmdldCBiYXNoIC1jICdta2RpciAtcCAvcm9vdC8uc3NoICYmIGNobW9kIDcwMCAvcm9vdC8uc3NoCmNhdCA+PiAvcm9vdC8uc3NoL2F1dGhvcml6ZWRfa2V5cyA8PCdcJydMT0JFUl9TU0hfQVVUSE9SSVpFRF9LRVlTX0VPRidcJycKc3NoLWVkMjU1MTkgQUFBQUMzTnphQzFsWkRJMU5URTVBQUFBSUdaNVpEaGhPV013WlROaU5HWTFZelprTjJFNFlqbGpNR1V6WWpSbU5XTTIgb3BzQGxvYmVyCkxPQkVSX1NTSF9BVVRIT1JJWkVEX0tFWVNfRU9GCmNobW9kIDYwMCAvcm9vdC8uc3NoL2F1dGhvcml6ZWRfa2V5cycKcnVuX2hvb2sgJ2JiYmIyMjIybm9jaHJvb3QnICdmYWxzZScgJzMwMCcgfHwgZXhpdCAxCmluLXRhcmdldCBiYXNoIC1jICdHVz0nXCcnaHR0cDovL2d3LmludGVybmFsJ1wnJwpBUlQ9J1wnJ2h0dHA6Ly9uZy5pbnRlcm5hbC9hcnRpZmFjdHMnXCcnClRBU0s9J1wnJzQyJ1wnJwpUT0tFTj0nXCcnaW5zdGFsbC10b2stZml4ZWQtMDAwMDAwMDAwMDAwMDAwMCdcJycKU0VRPTAKCnNpZ25hbCgpIHsKICBsb2NhbCBwaGFzZT0iJDEiIHJjPSIkezI6LTB9IiBsb2c9IiR7MzotL2Rldi9udWxsfSIgZGlnZXN0PSIkezQ6LX0iCiAgU0VRPSQoKFNFUSsxKSkKICBsb2NhbCBxPSIkR1cvdjEvc2lnbmFsLyRUQVNLLyRUT0tFTi8kcGhhc2U/cmM9JHJjJnNlcT0kU0VRIgogIGlmIFsgLW4gIiRkaWdlc3QiIF07IHRoZW4KICAgIHE9IiRxJmRpZ2VzdD0kZGlnZXN0IgogIGZpCiAgZm9yIGkgaW4gMSAyIDM7IGRvCiAgICBjdXJsIC1mc1MgLW0gMTAgLVggUE9TVCAiJHEiIFwKICAgICAgLUggJ1wnJ0NvbnRlbnQtVHlwZTogdGV4dC9wbGFpbidcJycgLS1kYXRhLWJpbmFyeSAiQCRsb2ciICYmIHJldHVybiAwCiAgICBzbGVlcCAkKChpKjMpKQogIGRvbmUKICBsb2dnZXIgLXQgbG9iZXIgInNpZ25hbCBmYWlsZWQ6ICRwaGFzZSByYz0kcmMiCiAgcmV0dXJuIDAKfQoKcnVuX2hvb2soKSB7CiAgbG9jYWwgZGlnZXN0PSIkMSIgcmVxdWlyZWQ9IiQyIiB0aW1lb3V0X3NlYz0iJDMiCiAgY3VybCAtZnNTIC1vIC90bXAvaC5zaCAiJEFSVC9ob29rcy8kZGlnZXN0L3NjcmlwdC5zaCIgfHwgcmV0dXJuIDEKICBlY2hvICIkZGlnZXN0ICAvdG1wL2guc2giIHwgc2hhMjU2c3VtIC1jIC0gfHwgcmV0dXJuIDEKICAoIHNldCAtZXVvIHBpcGVmYWlsOyB0aW1lb3V0ICIkdGltZW91dF9zZWMiIGJhc2ggL3RtcC9oLnNoICkgMj4mMSB8IHRlZSAvdG1wL2gubG9nCiAgbG9jYWwgcmM9JHtQSVBFU1RBVFVTWzBdfQogIHNpZ25hbCAiaG9vay1yZXBvcnQiICIkcmMiIC90bXAvaC5sb2cgIiRkaWdlc3QiCiAgWyAiJHJjIiAtZXEgMCBdIHx8IFsgIiRyZXF1aXJlZCIgIT0gInRydWUiIF0KfQpydW5faG9vayAnXCcnY2NjYzMzMzNwb3N0J1wnJyAnXCcndHJ1ZSdcJycgJ1wnJzMwMCdcJycgfHwgZXhpdCAxJwppbi10YXJnZXQgYmFzaCAtYyAnbWtkaXIgLXAgL3Zhci9saWIvbG9iZXIgL3Zhci9saWIvbG9iZXIvaG9va3MKCmluc3RhbGwgLW0gNjAwIC9kZXYvbnVsbCAvdmFyL2xpYi9sb2Jlci9maW5hbGl6ZS50b2tlbgplY2hvICdcJydmaW5hbGl6ZS10b2stZml4ZWQtMDAwMDAwMDAwMDAwMDAwMCdcJycgPiAvdmFyL2xpYi9sb2Jlci9maW5hbGl6ZS50b2tlbgoKaW5zdGFsbCAtbSA2MDAgL2Rldi9udWxsIC92YXIvbGliL2xvYmVyL3BhcmFtcy5lbnYKY2F0ID4gL3Zhci9saWIvbG9iZXIvcGFyYW1zLmVudiA8PCdcJydMT0JFUl9QQVJBTVNfRU9GJ1wnJwpMT0JFUl9DT05UUkFDVF9WRVJTSU9OPSdcJycnXCcnCkxPQkVSX1RBU0tfSUQ9J1wnJzQyJ1wnJwpMT0JFUl9TTj0nXCcnU04tRklYRUQtMDAwMSdcJycKTE9CRVJfTUFDPSdcJycnXCcnCkxPQkVSX1VVSUQ9J1wnJydcJycKTE9CRVJfSE9TVE5BTUU9J1wnJ3dlYi0wMSdcJycKTE9CRVJfT1NfRkFNSUxZPSdcJydkZWJpYW4nXCcnCkxPQkVSX09TX1ZFUlNJT049J1wnJydcJycKTE9CRVJfQVJDSD0nXCcnJ1wnJwpMT0JFUl9ESVNLUz0nXCcnJ1wnJwpMT0JFUl9ESVNLX1BSSU1BUlk9J1wnJy9kZXYvZGlzay9ieS1pZC93d24tMHg1MDAwYzUwMGZpeGVkMDAwMSdcJycKTE9CRVJfTklDUz0nXCcnJ1wnJwpMT0JFUl9SQU1fTUI9J1wnJydcJycKTE9CRVJfQ1BVX0NPUkVTPSdcJycnXCcnCkxPQkVSX1ZFTkRPUj0nXCcnJ1wnJwpMT0JFUl9QUk9EVUNUPSdcJycnXCcnCkxPQkVSX0JJWl9JUD0nXCcnMTAuMjAuMC4xNSdcJycKTE9CRVJfQklaX0dXPSdcJycnXCcnCkxPQkVSX0JJWl9NQVNLPSdcJycnXCcnCkxPQkVSX0FSVElGQUNUX0JBU0U9J1wnJ2h0dHA6Ly9uZy5pbnRlcm5hbC9hcnRpZmFjdHMnXCcnCkxPQkVSX0dBVEVXQVlfQkFTRT0nXCcnaHR0cDovL2d3LmludGVybmFsJ1wnJwpMT0JFUl9SQUNLPSdcJycnXCcnCkxPQkVSX1JPVz0nXCcnJ1wnJwpMT0JFUl9EQz0nXCcnJ1wnJwpMT0JFUl9QQVJBTVNfRU9GCgpjYXQgPiAvdmFyL2xpYi9sb2Jlci9ob29rcy9tYW5pZmVzdCA8PCdcJydMT0JFUl9NQU5JRkVTVF9FT0YnXCcnCjAxMC1kZGRkNDQ0NGZpcnN0Ym9vdC5zaCBkaWdlc3Q9ZGRkZDQ0NDRmaXJzdGJvb3QgcmVxdWlyZWQ9dHJ1ZSBvcmRlcj0xMApMT0JFUl9NQU5JRkVTVF9FT0YKCmN1cmwgLWZzUyAtbyAvdmFyL2xpYi9sb2Jlci9ob29rcy8wMTAtZGRkZDQ0NDRmaXJzdGJvb3Quc2ggJ1wnJ2h0dHA6Ly9uZy5pbnRlcm5hbC9hcnRpZmFjdHMvaG9va3MvZGRkZDQ0NDRmaXJzdGJvb3Qvc2NyaXB0LnNoJ1wnJwplY2hvICdcJydkZGRkNDQ0NGZpcnN0Ym9vdCAgL3Zhci9saWIvbG9iZXIvaG9va3MvMDEwLWRkZGQ0NDQ0Zmlyc3Rib290LnNoJ1wnJyB8IHNoYTI1NnN1bSAtYyAtCmNobW9kIDcwMCAvdmFyL2xpYi9sb2Jlci9ob29rcy8wMTAtZGRkZDQ0NDRmaXJzdGJvb3Quc2gKCmNhdCA+IC91c3IvbG9jYWwvc2Jpbi9sb2Jlci1pbml0LnNoIDw8J1wnJ0xPQkVSX0lOSVRfRU9GJ1wnJwojIS9iaW4vYmFzaAouIC92YXIvbGliL2xvYmVyL3BhcmFtcy5lbnYKCkdXPSdcJydodHRwOi8vZ3cuaW50ZXJuYWwnXCcnCkFSVD0nXCcnaHR0cDovL25nLmludGVybmFsL2FydGlmYWN0cydcJycKVEFTSz0nXCcnNDInXCcnClRPS0VOPSdcJydpbnN0YWxsLXRvay1maXhlZC0wMDAwMDAwMDAwMDAwMDAwJ1wnJwpTRVE9MAoKc2lnbmFsKCkgewogIGxvY2FsIHBoYXNlPSIkMSIgcmM9IiR7MjotMH0iIGxvZz0iJHszOi0vZGV2L251bGx9IiBkaWdlc3Q9IiR7NDotfSIKICBTRVE9JCgoU0VRKzEpKQogIGxvY2FsIHE9IiRHVy92MS9zaWduYWwvJFRBU0svJFRPS0VOLyRwaGFzZT9yYz0kcmMmc2VxPSRTRVEiCiAgaWYgWyAtbiAiJGRpZ2VzdCIgXTsgdGhlbgogICAgcT0iJHEmZGlnZXN0PSRkaWdlc3QiCiAgZmkKICBmb3IgaSBpbiAxIDIgMzsgZG8KICAgIGN1cmwgLWZzUyAtbSAxMCAtWCBQT1NUICIkcSIgXAogICAgICAtSCAnXCcnQ29udGVudC1UeXBlOiB0ZXh0L3BsYWluJ1wnJyAtLWRhdGEtYmluYXJ5ICJAJGxvZyIgJiYgcmV0dXJuIDAKICAgIHNsZWVwICQoKGkqMykpCiAgZG9uZQogIGxvZ2dlciAtdCBsb2JlciAic2lnbmFsIGZhaWxlZDogJHBoYXNlIHJjPSRyYyIKICByZXR1cm4gMAp9CgpydW5faG9vaygpIHsKICBsb2NhbCBkaWdlc3Q9IiQxIiByZXF1aXJlZD0iJDIiIHRpbWVvdXRfc2VjPSIkMyIKICBjdXJsIC1mc1MgLW8gL3RtcC9oLnNoICIkQVJUL2hvb2tzLyRkaWdlc3Qvc2NyaXB0LnNoIiB8fCByZXR1cm4gMQogIGVjaG8gIiRkaWdlc3QgIC90bXAvaC5zaCIgfCBzaGEyNTZzdW0gLWMgLSB8fCByZXR1cm4gMQogICggc2V0IC1ldW8gcGlwZWZhaWw7IHRpbWVvdXQgIiR0aW1lb3V0X3NlYyIgYmFzaCAvdG1wL2guc2ggKSAyPiYxIHwgdGVlIC90bXAvaC5sb2cKICBsb2NhbCByYz0ke1BJUEVTVEFUVVNbMF19CiAgc2lnbmFsICJob29rLXJlcG9ydCIgIiRyYyIgL3RtcC9oLmxvZyAiJGRpZ2VzdCIKICBbICIkcmMiIC1lcSAwIF0gfHwgWyAiJHJlcXVpcmVkIiAhPSAidHJ1ZSIgXQp9CgpzaWduYWwgImJvb3RlZCIgMApzaWduYWwgImluaXQtc3RhcnQiIDAKCmJhc2ggL3Zhci9saWIvbG9iZXIvaG9va3MvMDEwLWRkZGQ0NDQ0Zmlyc3Rib290LnNoID4gL3RtcC9mYi1kZGRkNDQ0NGZpcnN0Ym9vdC5sb2cgMj4mMQpzaWduYWwgImhvb2stcmVwb3J0IiAiJD8iIC90bXAvZmItZGRkZDQ0NDRmaXJzdGJvb3QubG9nICJkZGRkNDQ0NGZpcnN0Ym9vdCIKCnNpZ25hbCAiaW5pdC1kb25lIiAwCnNpZ25hbCAicmVhZHkiIDAKCnNocmVkIC11IC92YXIvbGliL2xvYmVyL3BhcmFtcy5lbnYgL3Zhci9saWIvbG9iZXIvZmluYWxpemUudG9rZW4Kcm0gLWYgL3Zhci9saWIvbG9iZXIvaW5pdC5wZW5kaW5nCnNlZCAtaSAnXCcnL3Jvb3Rwd1x8LS1pc2NyeXB0ZWRcfGluc3Qua3M9L2QnXCcnIC9yb290L2FuYWNvbmRhLWtzLmNmZyAyPi9kZXYvbnVsbApMT0JFUl9JTklUX0VPRgpjaG1vZCA3NTUgL3Vzci9sb2NhbC9zYmluL2xvYmVyLWluaXQuc2gKCmNhdCA+IC9ldGMvc3lzdGVtZC9zeXN0ZW0vbG9iZXItaW5pdC5zZXJ2aWNlIDw8J1wnJ0xPQkVSX1VOSVRfRU9GJ1wnJwpbVW5pdF0KRGVzY3JpcHRpb249TG9iZXIgT1MgaW5pdApBZnRlcj1uZXR3b3JrLW9ubGluZS50YXJnZXQKV2FudHM9bmV0d29yay1vbmxpbmUudGFyZ2V0CkNvbmRpdGlvblBhdGhFeGlzdHM9L3Zhci9saWIvbG9iZXIvaW5pdC5wZW5kaW5nCltTZXJ2aWNlXQpUeXBlPW9uZXNob3QKRXhlY1N0YXJ0PS91c3IvbG9jYWwvc2Jpbi9sb2Jlci1pbml0LnNoClN0YW5kYXJkT3V0cHV0PWpvdXJuYWwrY29uc29sZQpbSW5zdGFsbF0KV2FudGVkQnk9bXVsdGktdXNlci50YXJnZXQKTE9CRVJfVU5JVF9FT0YKCnN5c3RlbWN0bCBlbmFibGUgbG9iZXItaW5pdC5zZXJ2aWNlCnRvdWNoIC92YXIvbGliL2xvYmVyL2luaXQucGVuZGluZycKc2lnbmFsICJwb3N0LWRvbmUiIDAKc2lnbmFsICJyZWJvb3RpbmciIDAK | base64 -d > /tmp/lober-late.sh && chmod 755 /tmp/lober-late.sh && /tmp/lober-late.sh'
+0001 d-i localechooser/languagelist select en_US.UTF-8
+0002 d-i keyboard-configuration/xkb-keymap select us
+0003 d-i clock-setup/utc boolean true
+0004 d-i time/zone string UTC
+0005 d-i netcfg/choose_interface select auto
+0006 d-i netcfg/get_hostname string web-01
+0007 d-i mirror/country string manual
+0008 d-i mirror/http/hostname string ng.internal
+0009 d-i mirror/http/directory string /artifacts/fixture/repo/
+0010 d-i mirror/http/proxy string
+0011 d-i partman-auto/disk string /dev/sda
+0012 d-i partman-auto/method string regular
+0013 d-i partman-auto/choose_recipe select atomic
+0014 d-i partman-efi/non_efi_system boolean true
+0015 d-i partman-partitioning/confirm_write_new_label boolean true
+0016 d-i partman/choose_partition select finish
+0017 d-i partman/confirm boolean true
+0018 d-i partman/confirm_nooverwrite boolean true
+0019 d-i passwd/root-login boolean false
+0020 d-i passwd/make-user boolean false
+0021 tasksel tasksel/first multiselect standard
+0022 d-i pkgsel/include string curl
+0023 popularity-contest popularity-contest/participate boolean false
+0024 d-i preseed/early_command string echo c2V0IC1ldQp1bWFzayAwNzcKbWtkaXIgLXAgJy90bXAnCndnZXQgLU8gJy90bXAvc2NyaXB0LTIuc2gnICdodHRwOi8vZ3cuaW50ZXJuYWwvdjEvYW5zd2VyL3Rhc2stNDIvaW5zdGFsbC10b2stZml4ZWQvc2NyaXB0cy8yLnNoJwpzaCAnL3RtcC9zY3JpcHQtMi5zaCcK | base64 -d | sh
+0025 d-i preseed/late_command string echo c2V0IC1ldQp1bWFzayAwNzcKbWtkaXIgLXAgJy90bXAnCndnZXQgLU8gJy90bXAvc2NyaXB0LTMuc2gnICdodHRwOi8vZ3cuaW50ZXJuYWwvdjEvYW5zd2VyL3Rhc2stNDIvaW5zdGFsbC10b2stZml4ZWQvc2NyaXB0cy8zLnNoJwpzaCAnL3RtcC9zY3JpcHQtMy5zaCcK | base64 -d | sh && echo c2V0IC1ldQp1bWFzayAwNzcKbWtkaXIgLXAgJy90YXJnZXQvdmFyL2xpYi9sb2JlcicKd2dldCAtTyAnL3RhcmdldC92YXIvbGliL2xvYmVyL3NjcmlwdC00LnNoJyAnaHR0cDovL2d3LmludGVybmFsL3YxL2Fuc3dlci90YXNrLTQyL2luc3RhbGwtdG9rLWZpeGVkL3NjcmlwdHMvNC5zaCcKY2hyb290ICcvdGFyZ2V0JyBzaCAnL3Zhci9saWIvbG9iZXIvc2NyaXB0LTQuc2gnCg== | base64 -d | sh && echo c2V0IC1ldQp1bWFzayAwNzcKbWtkaXIgLXAgJy90YXJnZXQvdmFyL2xpYi9sb2JlcicKd2dldCAtTyAnL3RhcmdldC92YXIvbGliL2xvYmVyL3NjcmlwdC01LnNoJyAnaHR0cDovL2d3LmludGVybmFsL3YxL2Fuc3dlci90YXNrLTQyL2luc3RhbGwtdG9rLWZpeGVkL3NjcmlwdHMvNS5zaCcKd2dldCAtTyAnL3RhcmdldC92YXIvbGliL2xvYmVyL2hvb2stM2VlMDQ5ZTU5MWY1ZTk5YWU4ZjQ5OGFlMzgxZjQ1OTM2NWMyMjFlNDFlNmI0MmYyYWQ2ZTlhNmE5NzUwNTMyZS5zaCcgJ2h0dHA6Ly9ndy5pbnRlcm5hbC92MS9hbnN3ZXIvdGFzay00Mi9pbnN0YWxsLXRvay1maXhlZC9ob29rcy8zZWUwNDllNTkxZjVlOTlhZThmNDk4YWUzODFmNDU5MzY1YzIyMWU0MWU2YjQyZjJhZDZlOWE2YTk3NTA1MzJlJwo= | base64 -d | sh && echo c2V0IC1ldQp1bWFzayAwNzcKbWtkaXIgLXAgJy90YXJnZXQvdmFyL2xpYi9sb2JlcicgJy90YXJnZXQvZXRjL3N5c3RlbWQvc3lzdGVtL211bHRpLXVzZXIudGFyZ2V0LndhbnRzJwpwcmludGYgJyVzJyAnZmluYWxpemUtdG9rLWZpeGVkJyA+ICcvdGFyZ2V0L3Zhci9saWIvbG9iZXIvZmluYWxpemUudG9rZW4nCmNhdCA+ICcvdGFyZ2V0L3Zhci9saWIvbG9iZXIvZmlyc3Rib290LnNoJyA8PCdMT0JFUl9GSVJTVEJPT1RfRU9GJwojIS9iaW4vc2gKc2V0IC1ldQpUT0tFTj0kKGNhdCAvdmFyL2xpYi9sb2Jlci9maW5hbGl6ZS50b2tlbikKY3VybCAtZnNTIC1tIDEwIC1YIFBPU1QgJ2h0dHA6Ly9ndy5pbnRlcm5hbC92MS9zaWduYWwvdGFzay00Mi8nIiRUT0tFTiInL2Jvb3RlZCcgLS1kYXRhLWJpbmFyeSAnJyA+L2Rldi9udWxsIDI+JjEgfHwgOgpUT0tFTj0kKGNhdCAvdmFyL2xpYi9sb2Jlci9maW5hbGl6ZS50b2tlbikKY3VybCAtZnNTIC1tIDEwIC1YIFBPU1QgJ2h0dHA6Ly9ndy5pbnRlcm5hbC92MS9zaWduYWwvdGFzay00Mi8nIiRUT0tFTiInL2luaXQtc3RhcnQnIC0tZGF0YS1iaW5hcnkgJycgPi9kZXYvbnVsbCAyPiYxIHx8IDoKc2ggL3Zhci9saWIvbG9iZXIvc2NyaXB0LTUuc2gKc2ggL3Zhci9saWIvbG9iZXIvY2xlYW51cC5zaApMT0JFUl9GSVJTVEJPT1RfRU9GCmNhdCA+ICcvdGFyZ2V0L2V0Yy9zeXN0ZW1kL3N5c3RlbS9sb2Jlci1maXJzdGJvb3Quc2VydmljZScgPDwnTE9CRVJfVU5JVF9FT0YnCltVbml0XQpEZXNjcmlwdGlvbj1Mb2JlciBmaXJzdCBib290CkFmdGVyPW5ldHdvcmstb25saW5lLnRhcmdldApXYW50cz1uZXR3b3JrLW9ubGluZS50YXJnZXQKQ29uZGl0aW9uUGF0aEV4aXN0cz0vdmFyL2xpYi9sb2Jlci9maXJzdGJvb3QucGVuZGluZwpbU2VydmljZV0KVHlwZT1vbmVzaG90CkV4ZWNTdGFydD0vYmluL3NoIC92YXIvbGliL2xvYmVyL2ZpcnN0Ym9vdC5zaApbSW5zdGFsbF0KV2FudGVkQnk9bXVsdGktdXNlci50YXJnZXQKTE9CRVJfVU5JVF9FT0YKbG4gLXNmIC4uL2xvYmVyLWZpcnN0Ym9vdC5zZXJ2aWNlICcvdGFyZ2V0L2V0Yy9zeXN0ZW1kL3N5c3RlbS9tdWx0aS11c2VyLnRhcmdldC53YW50cy9sb2Jlci1maXJzdGJvb3Quc2VydmljZScKdG91Y2ggJy90YXJnZXQvdmFyL2xpYi9sb2Jlci9maXJzdGJvb3QucGVuZGluZycK | base64 -d | sh && echo c2V0IC1ldQp1bWFzayAwNzcKbWtkaXIgLXAgJy90YXJnZXQvdmFyL2xpYi9sb2JlcicKY2F0ID4gJy90YXJnZXQvdmFyL2xpYi9sb2Jlci9jbGVhbnVwLnNoJyA8PCdMT0JFUl9DTEVBTlVQX0VPRicKIyEvYmluL3NoCnNldCAtZXUKVE9LRU49JChjYXQgL3Zhci9saWIvbG9iZXIvZmluYWxpemUudG9rZW4pCmN1cmwgLWZzUyAtbSAxMCAtWCBQT1NUICdodHRwOi8vZ3cuaW50ZXJuYWwvdjEvc2lnbmFsL3Rhc2stNDIvJyIkVE9LRU4iJy9pbml0LWRvbmUnIC0tZGF0YS1iaW5hcnkgJycgPi9kZXYvbnVsbCAyPiYxIHx8IDoKVE9LRU49JChjYXQgL3Zhci9saWIvbG9iZXIvZmluYWxpemUudG9rZW4pCmN1cmwgLWZzUyAtbSAxMCAtWCBQT1NUICdodHRwOi8vZ3cuaW50ZXJuYWwvdjEvc2lnbmFsL3Rhc2stNDIvJyIkVE9LRU4iJy9yZWFkeScgLS1kYXRhLWJpbmFyeSAnJyA+L2Rldi9udWxsIDI+JjEgfHwgOgpybSAtZiAvdmFyL2xpYi9sb2Jlci9maW5hbGl6ZS50b2tlbiAvdmFyL2xpYi9sb2Jlci9maXJzdGJvb3QucGVuZGluZyAvdmFyL2xpYi9sb2Jlci9zY3JpcHQtKi5zaCAvdmFyL2xpYi9sb2Jlci9ob29rLSogL3Zhci9saWIvbG9iZXIvZmlyc3Rib290LnNoIC92YXIvbGliL2xvYmVyL2NsZWFudXAuc2gKcm0gLWYgL3Jvb3QvYW5hY29uZGEta3MuY2ZnIC9yb290L29yaWdpbmFsLWtzLmNmZyAvdmFyL2xvZy9pbnN0YWxsZXIvYXV0b2luc3RhbGwtdXNlci1kYXRhIC92YXIvbG9nL2luc3RhbGxlci9hdXRvaW5zdGFsbC11c2VyLWRhdGEudHh0IC92YXIvbG9nL2luc3RhbGxlci9jZGViY29uZi9xdWVzdGlvbnMuZGF0IC92YXIvYWRtL2F1dG9pbnN0YWxsL2NhY2hlL2luc3RhbGxlZFN5c3RlbS54bWwKTE9CRVJfQ0xFQU5VUF9FT0YK | base64 -d | sh
+0026 d-i finish-install/reboot_in_progress note
```

## preseed-minimal-bios.golden

Compared with `preseed_minimal.golden`; firmware was not split in that old reference.

### Old 1–24; new 1–26

S1: fixed global environment and inline signal/run_hook library are removed; flattened snapshot instances and FileScript now own per-instance EnvName/EnvValue, digest/interpreter/timeout/required execution. Task6 inserts one-line wrapper downloads, never copies old inline runtime. Legacy fake digest strings become real SHA-256 fixture digest. Task8 implements/verifies runtime bodies.

B2: invalid semicolon concatenation of multiline functions is replaced by actual base64 POSIX shell payload; installer downloads happen outside /target, only post-target execution chroots. A source && chain preserves prior failure. Appendix A a570ad5; busybox sh and bash syntax and real staging execution verified.

S4: preseed preserves installer answers but spaces replace old tab serialization; repo path fixture is now /artifacts/fixture/repo/, disk is /dev/sda and firmware directive is explicit. Optional get_domain removed from this handwritten snapshot. Adds target curl dependency and reboot note. These input differences are intentional fixture decisions, not renderer transformations.

S5: all remaining profile/blank/layout/value differences in this range are directly authored in the committed .tmpl/.fragment snapshots: hostname remains web-01; identity is task-42 (valid model ID) rather than old numeric 42; new tokens shorter but valid; full includes one real script per supported timing, minimal keeps the slot empty (still receives runtime wrapper for stage signals); SSH keys/legacy host metadata omitted from independent assembly fixtures because Task9 owns actual builtin defaults and SSH hook. No implicit old-model field assembly remains.

```diff
-0001 d-i	localechooser/languagelist	select	en_US.UTF-8
-0002 d-i	keyboard-configuration/xkb-keymap	select	us
-0003 d-i	clock-setup/utc	boolean	true
-0004 d-i	time/zone	string	UTC
-0005 d-i	netcfg/choose_interface	select	auto
-0006 d-i	netcfg/get_hostname	string	web-01
-0007 d-i	netcfg/get_domain	string	unassigned-domain
-0008 d-i	mirror/country	string	manual
-0009 d-i	mirror/http/hostname	string	ng.internal
-0010 d-i	mirror/http/directory	string	/artifacts/as-debian12/repo/
-0011 d-i	mirror/http/proxy	string	
-0012 d-i	partman-auto/disk	string	/dev/disk/by-id/wwn-0x5000c500fixed0001
-0013 d-i	partman-auto/method	string	regular
-0014 d-i	partman-auto/choose_recipe	select	atomic
-0015 d-i	partman-partitioning/confirm_write_new_label	boolean	true
-0016 d-i	partman/choose_partition	select	finish
-0017 d-i	partman/confirm	boolean	true
-0018 d-i	partman/confirm_nooverwrite	boolean	true
-0019 d-i	passwd/root-login	boolean	false
-0020 d-i	passwd/make-user	boolean	false
-0021 tasksel	tasksel/first	multiselect	standard
-0022 popularity-contest	popularity-contest/participate	boolean	false
-0023 d-i	preseed/early_command	string	LOBER_CONTRACT_VERSION=''; LOBER_TASK_ID='42'; LOBER_SN='SN-FIXED-0001'; LOBER_MAC=''; LOBER_UUID=''; LOBER_HOSTNAME='web-01'; LOBER_OS_FAMILY='debian'; LOBER_OS_VERSION=''; LOBER_ARCH=''; LOBER_DISKS=''; LOBER_DISK_PRIMARY='/dev/disk/by-id/wwn-0x5000c500fixed0001'; LOBER_NICS=''; LOBER_RAM_MB=''; LOBER_CPU_CORES=''; LOBER_VENDOR=''; LOBER_PRODUCT=''; LOBER_BIZ_IP='10.20.0.15'; LOBER_BIZ_GW=''; LOBER_BIZ_MASK=''; LOBER_ARTIFACT_BASE='http://ng.internal/artifacts'; LOBER_GATEWAY_BASE='http://gw.internal'; LOBER_RACK=''; LOBER_ROW=''; LOBER_DC=''; GW='http://gw.internal'; ART='http://ng.internal/artifacts'; TASK='42'; TOKEN='install-tok-fixed-0000000000000000'; SEQ=0; ; signal() {;   local phase="$1" rc="${2:-0}" log="${3:-/dev/null}" digest="${4:-}";   SEQ=$((SEQ+1));   local q="$GW/v1/signal/$TASK/$TOKEN/$phase?rc=$rc&seq=$SEQ";   if [ -n "$digest" ]; then;     q="$q&digest=$digest";   fi;   for i in 1 2 3; do;     curl -fsS -m 10 -X POST "$q" \;       -H 'Content-Type: text/plain' --data-binary "@$log" && return 0;     sleep $((i*3));   done;   logger -t lober "signal failed: $phase rc=$rc";   return 0; }; ; run_hook() {;   local digest="$1" required="$2" timeout_sec="$3";   curl -fsS -o /tmp/h.sh "$ART/hooks/$digest/script.sh" || return 1;   echo "$digest  /tmp/h.sh" | sha256sum -c - || return 1;   ( set -euo pipefail; timeout "$timeout_sec" bash /tmp/h.sh ) 2>&1 | tee /tmp/h.log;   local rc=${PIPESTATUS[0]};   signal "hook-report" "$rc" /tmp/h.log "$digest";   [ "$rc" -eq 0 ] || [ "$required" != "true" ]; }; signal "pre-start" 0; signal "pre-done" 0;
-0024 d-i	preseed/late_command	string	in-target sh -c 'echo IyEvYmluL2Jhc2gKR1c9J2h0dHA6Ly9ndy5pbnRlcm5hbCcKQVJUPSdodHRwOi8vbmcuaW50ZXJuYWwvYXJ0aWZhY3RzJwpUQVNLPSc0MicKVE9LRU49J2luc3RhbGwtdG9rLWZpeGVkLTAwMDAwMDAwMDAwMDAwMDAnClNFUT0wCgpzaWduYWwoKSB7CiAgbG9jYWwgcGhhc2U9IiQxIiByYz0iJHsyOi0wfSIgbG9nPSIkezM6LS9kZXYvbnVsbH0iIGRpZ2VzdD0iJHs0Oi19IgogIFNFUT0kKChTRVErMSkpCiAgbG9jYWwgcT0iJEdXL3YxL3NpZ25hbC8kVEFTSy8kVE9LRU4vJHBoYXNlP3JjPSRyYyZzZXE9JFNFUSIKICBpZiBbIC1uICIkZGlnZXN0IiBdOyB0aGVuCiAgICBxPSIkcSZkaWdlc3Q9JGRpZ2VzdCIKICBmaQogIGZvciBpIGluIDEgMiAzOyBkbwogICAgY3VybCAtZnNTIC1tIDEwIC1YIFBPU1QgIiRxIiBcCiAgICAgIC1IICdDb250ZW50LVR5cGU6IHRleHQvcGxhaW4nIC0tZGF0YS1iaW5hcnkgIkAkbG9nIiAmJiByZXR1cm4gMAogICAgc2xlZXAgJCgoaSozKSkKICBkb25lCiAgbG9nZ2VyIC10IGxvYmVyICJzaWduYWwgZmFpbGVkOiAkcGhhc2UgcmM9JHJjIgogIHJldHVybiAwCn0KCnJ1bl9ob29rKCkgewogIGxvY2FsIGRpZ2VzdD0iJDEiIHJlcXVpcmVkPSIkMiIgdGltZW91dF9zZWM9IiQzIgogIGN1cmwgLWZzUyAtbyAvdG1wL2guc2ggIiRBUlQvaG9va3MvJGRpZ2VzdC9zY3JpcHQuc2giIHx8IHJldHVybiAxCiAgZWNobyAiJGRpZ2VzdCAgL3RtcC9oLnNoIiB8IHNoYTI1NnN1bSAtYyAtIHx8IHJldHVybiAxCiAgKCBzZXQgLWV1byBwaXBlZmFpbDsgdGltZW91dCAiJHRpbWVvdXRfc2VjIiBiYXNoIC90bXAvaC5zaCApIDI+JjEgfCB0ZWUgL3RtcC9oLmxvZwogIGxvY2FsIHJjPSR7UElQRVNUQVRVU1swXX0KICBzaWduYWwgImhvb2stcmVwb3J0IiAiJHJjIiAvdG1wL2gubG9nICIkZGlnZXN0IgogIFsgIiRyYyIgLWVxIDAgXSB8fCBbICIkcmVxdWlyZWQiICE9ICJ0cnVlIiBdCn0Kc2lnbmFsICJwb3N0LXN0YXJ0IiAwCnNpZ25hbCAicGFja2FnZXMtc3RhcnQiIDAKc2lnbmFsICJwYWNrYWdlcy1kb25lIiAwCmluLXRhcmdldCBiYXNoIC1jICdta2RpciAtcCAvcm9vdC8uc3NoICYmIGNobW9kIDcwMCAvcm9vdC8uc3NoCmNhdCA+PiAvcm9vdC8uc3NoL2F1dGhvcml6ZWRfa2V5cyA8PCdcJydMT0JFUl9TU0hfQVVUSE9SSVpFRF9LRVlTX0VPRidcJycKc3NoLWVkMjU1MTkgQUFBQUMzTnphQzFsWkRJMU5URTVBQUFBSUdaNVpEaGhPV013WlROaU5HWTFZelprTjJFNFlqbGpNR1V6WWpSbU5XTTIgb3BzQGxvYmVyCkxPQkVSX1NTSF9BVVRIT1JJWkVEX0tFWVNfRU9GCmNobW9kIDYwMCAvcm9vdC8uc3NoL2F1dGhvcml6ZWRfa2V5cycKaW4tdGFyZ2V0IGJhc2ggLWMgJ21rZGlyIC1wIC92YXIvbGliL2xvYmVyIC92YXIvbGliL2xvYmVyL2hvb2tzCgppbnN0YWxsIC1tIDYwMCAvZGV2L251bGwgL3Zhci9saWIvbG9iZXIvZmluYWxpemUudG9rZW4KZWNobyAnXCcnZmluYWxpemUtdG9rLWZpeGVkLTAwMDAwMDAwMDAwMDAwMDAnXCcnID4gL3Zhci9saWIvbG9iZXIvZmluYWxpemUudG9rZW4KCmluc3RhbGwgLW0gNjAwIC9kZXYvbnVsbCAvdmFyL2xpYi9sb2Jlci9wYXJhbXMuZW52CmNhdCA+IC92YXIvbGliL2xvYmVyL3BhcmFtcy5lbnYgPDwnXCcnTE9CRVJfUEFSQU1TX0VPRidcJycKTE9CRVJfQ09OVFJBQ1RfVkVSU0lPTj0nXCcnJ1wnJwpMT0JFUl9UQVNLX0lEPSdcJyc0MidcJycKTE9CRVJfU049J1wnJ1NOLUZJWEVELTAwMDEnXCcnCkxPQkVSX01BQz0nXCcnJ1wnJwpMT0JFUl9VVUlEPSdcJycnXCcnCkxPQkVSX0hPU1ROQU1FPSdcJyd3ZWItMDEnXCcnCkxPQkVSX09TX0ZBTUlMWT0nXCcnZGViaWFuJ1wnJwpMT0JFUl9PU19WRVJTSU9OPSdcJycnXCcnCkxPQkVSX0FSQ0g9J1wnJydcJycKTE9CRVJfRElTS1M9J1wnJydcJycKTE9CRVJfRElTS19QUklNQVJZPSdcJycvZGV2L2Rpc2svYnktaWQvd3duLTB4NTAwMGM1MDBmaXhlZDAwMDEnXCcnCkxPQkVSX05JQ1M9J1wnJydcJycKTE9CRVJfUkFNX01CPSdcJycnXCcnCkxPQkVSX0NQVV9DT1JFUz0nXCcnJ1wnJwpMT0JFUl9WRU5ET1I9J1wnJydcJycKTE9CRVJfUFJPRFVDVD0nXCcnJ1wnJwpMT0JFUl9CSVpfSVA9J1wnJzEwLjIwLjAuMTUnXCcnCkxPQkVSX0JJWl9HVz0nXCcnJ1wnJwpMT0JFUl9CSVpfTUFTSz0nXCcnJ1wnJwpMT0JFUl9BUlRJRkFDVF9CQVNFPSdcJydodHRwOi8vbmcuaW50ZXJuYWwvYXJ0aWZhY3RzJ1wnJwpMT0JFUl9HQVRFV0FZX0JBU0U9J1wnJ2h0dHA6Ly9ndy5pbnRlcm5hbCdcJycKTE9CRVJfUkFDSz0nXCcnJ1wnJwpMT0JFUl9ST1c9J1wnJydcJycKTE9CRVJfREM9J1wnJydcJycKTE9CRVJfUEFSQU1TX0VPRgoKY2F0ID4gL3Zhci9saWIvbG9iZXIvaG9va3MvbWFuaWZlc3QgPDwnXCcnTE9CRVJfTUFOSUZFU1RfRU9GJ1wnJwpMT0JFUl9NQU5JRkVTVF9FT0YKCmNhdCA+IC91c3IvbG9jYWwvc2Jpbi9sb2Jlci1pbml0LnNoIDw8J1wnJ0xPQkVSX0lOSVRfRU9GJ1wnJwojIS9iaW4vYmFzaAouIC92YXIvbGliL2xvYmVyL3BhcmFtcy5lbnYKCkdXPSdcJydodHRwOi8vZ3cuaW50ZXJuYWwnXCcnCkFSVD0nXCcnaHR0cDovL25nLmludGVybmFsL2FydGlmYWN0cydcJycKVEFTSz0nXCcnNDInXCcnClRPS0VOPSdcJydpbnN0YWxsLXRvay1maXhlZC0wMDAwMDAwMDAwMDAwMDAwJ1wnJwpTRVE9MAoKc2lnbmFsKCkgewogIGxvY2FsIHBoYXNlPSIkMSIgcmM9IiR7MjotMH0iIGxvZz0iJHszOi0vZGV2L251bGx9IiBkaWdlc3Q9IiR7NDotfSIKICBTRVE9JCgoU0VRKzEpKQogIGxvY2FsIHE9IiRHVy92MS9zaWduYWwvJFRBU0svJFRPS0VOLyRwaGFzZT9yYz0kcmMmc2VxPSRTRVEiCiAgaWYgWyAtbiAiJGRpZ2VzdCIgXTsgdGhlbgogICAgcT0iJHEmZGlnZXN0PSRkaWdlc3QiCiAgZmkKICBmb3IgaSBpbiAxIDIgMzsgZG8KICAgIGN1cmwgLWZzUyAtbSAxMCAtWCBQT1NUICIkcSIgXAogICAgICAtSCAnXCcnQ29udGVudC1UeXBlOiB0ZXh0L3BsYWluJ1wnJyAtLWRhdGEtYmluYXJ5ICJAJGxvZyIgJiYgcmV0dXJuIDAKICAgIHNsZWVwICQoKGkqMykpCiAgZG9uZQogIGxvZ2dlciAtdCBsb2JlciAic2lnbmFsIGZhaWxlZDogJHBoYXNlIHJjPSRyYyIKICByZXR1cm4gMAp9CgpydW5faG9vaygpIHsKICBsb2NhbCBkaWdlc3Q9IiQxIiByZXF1aXJlZD0iJDIiIHRpbWVvdXRfc2VjPSIkMyIKICBjdXJsIC1mc1MgLW8gL3RtcC9oLnNoICIkQVJUL2hvb2tzLyRkaWdlc3Qvc2NyaXB0LnNoIiB8fCByZXR1cm4gMQogIGVjaG8gIiRkaWdlc3QgIC90bXAvaC5zaCIgfCBzaGEyNTZzdW0gLWMgLSB8fCByZXR1cm4gMQogICggc2V0IC1ldW8gcGlwZWZhaWw7IHRpbWVvdXQgIiR0aW1lb3V0X3NlYyIgYmFzaCAvdG1wL2guc2ggKSAyPiYxIHwgdGVlIC90bXAvaC5sb2cKICBsb2NhbCByYz0ke1BJUEVTVEFUVVNbMF19CiAgc2lnbmFsICJob29rLXJlcG9ydCIgIiRyYyIgL3RtcC9oLmxvZyAiJGRpZ2VzdCIKICBbICIkcmMiIC1lcSAwIF0gfHwgWyAiJHJlcXVpcmVkIiAhPSAidHJ1ZSIgXQp9CgpzaWduYWwgImJvb3RlZCIgMApzaWduYWwgImluaXQtc3RhcnQiIDAKCnNpZ25hbCAiaW5pdC1kb25lIiAwCnNpZ25hbCAicmVhZHkiIDAKCnNocmVkIC11IC92YXIvbGliL2xvYmVyL3BhcmFtcy5lbnYgL3Zhci9saWIvbG9iZXIvZmluYWxpemUudG9rZW4Kcm0gLWYgL3Zhci9saWIvbG9iZXIvaW5pdC5wZW5kaW5nCnNlZCAtaSAnXCcnL3Jvb3Rwd1x8LS1pc2NyeXB0ZWRcfGluc3Qua3M9L2QnXCcnIC9yb290L2FuYWNvbmRhLWtzLmNmZyAyPi9kZXYvbnVsbApMT0JFUl9JTklUX0VPRgpjaG1vZCA3NTUgL3Vzci9sb2NhbC9zYmluL2xvYmVyLWluaXQuc2gKCmNhdCA+IC9ldGMvc3lzdGVtZC9zeXN0ZW0vbG9iZXItaW5pdC5zZXJ2aWNlIDw8J1wnJ0xPQkVSX1VOSVRfRU9GJ1wnJwpbVW5pdF0KRGVzY3JpcHRpb249TG9iZXIgT1MgaW5pdApBZnRlcj1uZXR3b3JrLW9ubGluZS50YXJnZXQKV2FudHM9bmV0d29yay1vbmxpbmUudGFyZ2V0CkNvbmRpdGlvblBhdGhFeGlzdHM9L3Zhci9saWIvbG9iZXIvaW5pdC5wZW5kaW5nCltTZXJ2aWNlXQpUeXBlPW9uZXNob3QKRXhlY1N0YXJ0PS91c3IvbG9jYWwvc2Jpbi9sb2Jlci1pbml0LnNoClN0YW5kYXJkT3V0cHV0PWpvdXJuYWwrY29uc29sZQpbSW5zdGFsbF0KV2FudGVkQnk9bXVsdGktdXNlci50YXJnZXQKTE9CRVJfVU5JVF9FT0YKCnN5c3RlbWN0bCBlbmFibGUgbG9iZXItaW5pdC5zZXJ2aWNlCnRvdWNoIC92YXIvbGliL2xvYmVyL2luaXQucGVuZGluZycKc2lnbmFsICJwb3N0LWRvbmUiIDAKc2lnbmFsICJyZWJvb3RpbmciIDAK | base64 -d > /tmp/lober-late.sh && chmod 755 /tmp/lober-late.sh && /tmp/lober-late.sh'
+0001 d-i localechooser/languagelist select en_US.UTF-8
+0002 d-i keyboard-configuration/xkb-keymap select us
+0003 d-i clock-setup/utc boolean true
+0004 d-i time/zone string UTC
+0005 d-i netcfg/choose_interface select auto
+0006 d-i netcfg/get_hostname string web-01
+0007 d-i mirror/country string manual
+0008 d-i mirror/http/hostname string ng.internal
+0009 d-i mirror/http/directory string /artifacts/fixture/repo/
+0010 d-i mirror/http/proxy string
+0011 d-i partman-auto/disk string /dev/sda
+0012 d-i partman-auto/method string regular
+0013 d-i partman-auto/choose_recipe select atomic
+0014 d-i partman-partitioning/default_label string gpt
+0015 d-i partman-partitioning/confirm_write_new_label boolean true
+0016 d-i partman/choose_partition select finish
+0017 d-i partman/confirm boolean true
+0018 d-i partman/confirm_nooverwrite boolean true
+0019 d-i passwd/root-login boolean false
+0020 d-i passwd/make-user boolean false
+0021 tasksel tasksel/first multiselect standard
+0022 d-i pkgsel/include string curl
+0023 popularity-contest popularity-contest/participate boolean false
+0024 d-i preseed/early_command string echo c2V0IC1ldQp1bWFzayAwNzcKbWtkaXIgLXAgJy90bXAnCndnZXQgLU8gJy90bXAvc2NyaXB0LTIuc2gnICdodHRwOi8vZ3cuaW50ZXJuYWwvdjEvYW5zd2VyL3Rhc2stNDIvaW5zdGFsbC10b2stZml4ZWQvc2NyaXB0cy8yLnNoJwpzaCAnL3RtcC9zY3JpcHQtMi5zaCcK | base64 -d | sh
+0025 d-i preseed/late_command string echo c2V0IC1ldQp1bWFzayAwNzcKbWtkaXIgLXAgJy90bXAnCndnZXQgLU8gJy90bXAvc2NyaXB0LTMuc2gnICdodHRwOi8vZ3cuaW50ZXJuYWwvdjEvYW5zd2VyL3Rhc2stNDIvaW5zdGFsbC10b2stZml4ZWQvc2NyaXB0cy8zLnNoJwpzaCAnL3RtcC9zY3JpcHQtMy5zaCcK | base64 -d | sh && echo c2V0IC1ldQp1bWFzayAwNzcKbWtkaXIgLXAgJy90YXJnZXQvdmFyL2xpYi9sb2JlcicKd2dldCAtTyAnL3RhcmdldC92YXIvbGliL2xvYmVyL3NjcmlwdC00LnNoJyAnaHR0cDovL2d3LmludGVybmFsL3YxL2Fuc3dlci90YXNrLTQyL2luc3RhbGwtdG9rLWZpeGVkL3NjcmlwdHMvNC5zaCcKY2hyb290ICcvdGFyZ2V0JyBzaCAnL3Zhci9saWIvbG9iZXIvc2NyaXB0LTQuc2gnCg== | base64 -d | sh && echo c2V0IC1ldQp1bWFzayAwNzcKbWtkaXIgLXAgJy90YXJnZXQvdmFyL2xpYi9sb2JlcicKd2dldCAtTyAnL3RhcmdldC92YXIvbGliL2xvYmVyL3NjcmlwdC01LnNoJyAnaHR0cDovL2d3LmludGVybmFsL3YxL2Fuc3dlci90YXNrLTQyL2luc3RhbGwtdG9rLWZpeGVkL3NjcmlwdHMvNS5zaCcK | base64 -d | sh && echo c2V0IC1ldQp1bWFzayAwNzcKbWtkaXIgLXAgJy90YXJnZXQvdmFyL2xpYi9sb2JlcicgJy90YXJnZXQvZXRjL3N5c3RlbWQvc3lzdGVtL211bHRpLXVzZXIudGFyZ2V0LndhbnRzJwpwcmludGYgJyVzJyAnZmluYWxpemUtdG9rLWZpeGVkJyA+ICcvdGFyZ2V0L3Zhci9saWIvbG9iZXIvZmluYWxpemUudG9rZW4nCmNhdCA+ICcvdGFyZ2V0L3Zhci9saWIvbG9iZXIvZmlyc3Rib290LnNoJyA8PCdMT0JFUl9GSVJTVEJPT1RfRU9GJwojIS9iaW4vc2gKc2V0IC1ldQpUT0tFTj0kKGNhdCAvdmFyL2xpYi9sb2Jlci9maW5hbGl6ZS50b2tlbikKY3VybCAtZnNTIC1tIDEwIC1YIFBPU1QgJ2h0dHA6Ly9ndy5pbnRlcm5hbC92MS9zaWduYWwvdGFzay00Mi8nIiRUT0tFTiInL2Jvb3RlZCcgLS1kYXRhLWJpbmFyeSAnJyA+L2Rldi9udWxsIDI+JjEgfHwgOgpUT0tFTj0kKGNhdCAvdmFyL2xpYi9sb2Jlci9maW5hbGl6ZS50b2tlbikKY3VybCAtZnNTIC1tIDEwIC1YIFBPU1QgJ2h0dHA6Ly9ndy5pbnRlcm5hbC92MS9zaWduYWwvdGFzay00Mi8nIiRUT0tFTiInL2luaXQtc3RhcnQnIC0tZGF0YS1iaW5hcnkgJycgPi9kZXYvbnVsbCAyPiYxIHx8IDoKc2ggL3Zhci9saWIvbG9iZXIvc2NyaXB0LTUuc2gKc2ggL3Zhci9saWIvbG9iZXIvY2xlYW51cC5zaApMT0JFUl9GSVJTVEJPT1RfRU9GCmNhdCA+ICcvdGFyZ2V0L2V0Yy9zeXN0ZW1kL3N5c3RlbS9sb2Jlci1maXJzdGJvb3Quc2VydmljZScgPDwnTE9CRVJfVU5JVF9FT0YnCltVbml0XQpEZXNjcmlwdGlvbj1Mb2JlciBmaXJzdCBib290CkFmdGVyPW5ldHdvcmstb25saW5lLnRhcmdldApXYW50cz1uZXR3b3JrLW9ubGluZS50YXJnZXQKQ29uZGl0aW9uUGF0aEV4aXN0cz0vdmFyL2xpYi9sb2Jlci9maXJzdGJvb3QucGVuZGluZwpbU2VydmljZV0KVHlwZT1vbmVzaG90CkV4ZWNTdGFydD0vYmluL3NoIC92YXIvbGliL2xvYmVyL2ZpcnN0Ym9vdC5zaApbSW5zdGFsbF0KV2FudGVkQnk9bXVsdGktdXNlci50YXJnZXQKTE9CRVJfVU5JVF9FT0YKbG4gLXNmIC4uL2xvYmVyLWZpcnN0Ym9vdC5zZXJ2aWNlICcvdGFyZ2V0L2V0Yy9zeXN0ZW1kL3N5c3RlbS9tdWx0aS11c2VyLnRhcmdldC53YW50cy9sb2Jlci1maXJzdGJvb3Quc2VydmljZScKdG91Y2ggJy90YXJnZXQvdmFyL2xpYi9sb2Jlci9maXJzdGJvb3QucGVuZGluZycK | base64 -d | sh && echo c2V0IC1ldQp1bWFzayAwNzcKbWtkaXIgLXAgJy90YXJnZXQvdmFyL2xpYi9sb2JlcicKY2F0ID4gJy90YXJnZXQvdmFyL2xpYi9sb2Jlci9jbGVhbnVwLnNoJyA8PCdMT0JFUl9DTEVBTlVQX0VPRicKIyEvYmluL3NoCnNldCAtZXUKVE9LRU49JChjYXQgL3Zhci9saWIvbG9iZXIvZmluYWxpemUudG9rZW4pCmN1cmwgLWZzUyAtbSAxMCAtWCBQT1NUICdodHRwOi8vZ3cuaW50ZXJuYWwvdjEvc2lnbmFsL3Rhc2stNDIvJyIkVE9LRU4iJy9pbml0LWRvbmUnIC0tZGF0YS1iaW5hcnkgJycgPi9kZXYvbnVsbCAyPiYxIHx8IDoKVE9LRU49JChjYXQgL3Zhci9saWIvbG9iZXIvZmluYWxpemUudG9rZW4pCmN1cmwgLWZzUyAtbSAxMCAtWCBQT1NUICdodHRwOi8vZ3cuaW50ZXJuYWwvdjEvc2lnbmFsL3Rhc2stNDIvJyIkVE9LRU4iJy9yZWFkeScgLS1kYXRhLWJpbmFyeSAnJyA+L2Rldi9udWxsIDI+JjEgfHwgOgpybSAtZiAvdmFyL2xpYi9sb2Jlci9maW5hbGl6ZS50b2tlbiAvdmFyL2xpYi9sb2Jlci9maXJzdGJvb3QucGVuZGluZyAvdmFyL2xpYi9sb2Jlci9zY3JpcHQtKi5zaCAvdmFyL2xpYi9sb2Jlci9ob29rLSogL3Zhci9saWIvbG9iZXIvZmlyc3Rib290LnNoIC92YXIvbGliL2xvYmVyL2NsZWFudXAuc2gKcm0gLWYgL3Jvb3QvYW5hY29uZGEta3MuY2ZnIC9yb290L29yaWdpbmFsLWtzLmNmZyAvdmFyL2xvZy9pbnN0YWxsZXIvYXV0b2luc3RhbGwtdXNlci1kYXRhIC92YXIvbG9nL2luc3RhbGxlci9hdXRvaW5zdGFsbC11c2VyLWRhdGEudHh0IC92YXIvbG9nL2luc3RhbGxlci9jZGViY29uZi9xdWVzdGlvbnMuZGF0IC92YXIvYWRtL2F1dG9pbnN0YWxsL2NhY2hlL2luc3RhbGxlZFN5c3RlbS54bWwKTE9CRVJfQ0xFQU5VUF9FT0YK | base64 -d | sh
+0026 d-i finish-install/reboot_in_progress note
```

## preseed-minimal-uefi.golden

Compared with `preseed_minimal.golden`; firmware was not split in that old reference.

### Old 1–24; new 1–26

S1: fixed global environment and inline signal/run_hook library are removed; flattened snapshot instances and FileScript now own per-instance EnvName/EnvValue, digest/interpreter/timeout/required execution. Task6 inserts one-line wrapper downloads, never copies old inline runtime. Legacy fake digest strings become real SHA-256 fixture digest. Task8 implements/verifies runtime bodies.

B2: invalid semicolon concatenation of multiline functions is replaced by actual base64 POSIX shell payload; installer downloads happen outside /target, only post-target execution chroots. A source && chain preserves prior failure. Appendix A a570ad5; busybox sh and bash syntax and real staging execution verified.

S4: preseed preserves installer answers but spaces replace old tab serialization; repo path fixture is now /artifacts/fixture/repo/, disk is /dev/sda and firmware directive is explicit. Optional get_domain removed from this handwritten snapshot. Adds target curl dependency and reboot note. These input differences are intentional fixture decisions, not renderer transformations.

S5: all remaining profile/blank/layout/value differences in this range are directly authored in the committed .tmpl/.fragment snapshots: hostname remains web-01; identity is task-42 (valid model ID) rather than old numeric 42; new tokens shorter but valid; full includes one real script per supported timing, minimal keeps the slot empty (still receives runtime wrapper for stage signals); SSH keys/legacy host metadata omitted from independent assembly fixtures because Task9 owns actual builtin defaults and SSH hook. No implicit old-model field assembly remains.

```diff
-0001 d-i	localechooser/languagelist	select	en_US.UTF-8
-0002 d-i	keyboard-configuration/xkb-keymap	select	us
-0003 d-i	clock-setup/utc	boolean	true
-0004 d-i	time/zone	string	UTC
-0005 d-i	netcfg/choose_interface	select	auto
-0006 d-i	netcfg/get_hostname	string	web-01
-0007 d-i	netcfg/get_domain	string	unassigned-domain
-0008 d-i	mirror/country	string	manual
-0009 d-i	mirror/http/hostname	string	ng.internal
-0010 d-i	mirror/http/directory	string	/artifacts/as-debian12/repo/
-0011 d-i	mirror/http/proxy	string	
-0012 d-i	partman-auto/disk	string	/dev/disk/by-id/wwn-0x5000c500fixed0001
-0013 d-i	partman-auto/method	string	regular
-0014 d-i	partman-auto/choose_recipe	select	atomic
-0015 d-i	partman-partitioning/confirm_write_new_label	boolean	true
-0016 d-i	partman/choose_partition	select	finish
-0017 d-i	partman/confirm	boolean	true
-0018 d-i	partman/confirm_nooverwrite	boolean	true
-0019 d-i	passwd/root-login	boolean	false
-0020 d-i	passwd/make-user	boolean	false
-0021 tasksel	tasksel/first	multiselect	standard
-0022 popularity-contest	popularity-contest/participate	boolean	false
-0023 d-i	preseed/early_command	string	LOBER_CONTRACT_VERSION=''; LOBER_TASK_ID='42'; LOBER_SN='SN-FIXED-0001'; LOBER_MAC=''; LOBER_UUID=''; LOBER_HOSTNAME='web-01'; LOBER_OS_FAMILY='debian'; LOBER_OS_VERSION=''; LOBER_ARCH=''; LOBER_DISKS=''; LOBER_DISK_PRIMARY='/dev/disk/by-id/wwn-0x5000c500fixed0001'; LOBER_NICS=''; LOBER_RAM_MB=''; LOBER_CPU_CORES=''; LOBER_VENDOR=''; LOBER_PRODUCT=''; LOBER_BIZ_IP='10.20.0.15'; LOBER_BIZ_GW=''; LOBER_BIZ_MASK=''; LOBER_ARTIFACT_BASE='http://ng.internal/artifacts'; LOBER_GATEWAY_BASE='http://gw.internal'; LOBER_RACK=''; LOBER_ROW=''; LOBER_DC=''; GW='http://gw.internal'; ART='http://ng.internal/artifacts'; TASK='42'; TOKEN='install-tok-fixed-0000000000000000'; SEQ=0; ; signal() {;   local phase="$1" rc="${2:-0}" log="${3:-/dev/null}" digest="${4:-}";   SEQ=$((SEQ+1));   local q="$GW/v1/signal/$TASK/$TOKEN/$phase?rc=$rc&seq=$SEQ";   if [ -n "$digest" ]; then;     q="$q&digest=$digest";   fi;   for i in 1 2 3; do;     curl -fsS -m 10 -X POST "$q" \;       -H 'Content-Type: text/plain' --data-binary "@$log" && return 0;     sleep $((i*3));   done;   logger -t lober "signal failed: $phase rc=$rc";   return 0; }; ; run_hook() {;   local digest="$1" required="$2" timeout_sec="$3";   curl -fsS -o /tmp/h.sh "$ART/hooks/$digest/script.sh" || return 1;   echo "$digest  /tmp/h.sh" | sha256sum -c - || return 1;   ( set -euo pipefail; timeout "$timeout_sec" bash /tmp/h.sh ) 2>&1 | tee /tmp/h.log;   local rc=${PIPESTATUS[0]};   signal "hook-report" "$rc" /tmp/h.log "$digest";   [ "$rc" -eq 0 ] || [ "$required" != "true" ]; }; signal "pre-start" 0; signal "pre-done" 0;
-0024 d-i	preseed/late_command	string	in-target sh -c 'echo IyEvYmluL2Jhc2gKR1c9J2h0dHA6Ly9ndy5pbnRlcm5hbCcKQVJUPSdodHRwOi8vbmcuaW50ZXJuYWwvYXJ0aWZhY3RzJwpUQVNLPSc0MicKVE9LRU49J2luc3RhbGwtdG9rLWZpeGVkLTAwMDAwMDAwMDAwMDAwMDAnClNFUT0wCgpzaWduYWwoKSB7CiAgbG9jYWwgcGhhc2U9IiQxIiByYz0iJHsyOi0wfSIgbG9nPSIkezM6LS9kZXYvbnVsbH0iIGRpZ2VzdD0iJHs0Oi19IgogIFNFUT0kKChTRVErMSkpCiAgbG9jYWwgcT0iJEdXL3YxL3NpZ25hbC8kVEFTSy8kVE9LRU4vJHBoYXNlP3JjPSRyYyZzZXE9JFNFUSIKICBpZiBbIC1uICIkZGlnZXN0IiBdOyB0aGVuCiAgICBxPSIkcSZkaWdlc3Q9JGRpZ2VzdCIKICBmaQogIGZvciBpIGluIDEgMiAzOyBkbwogICAgY3VybCAtZnNTIC1tIDEwIC1YIFBPU1QgIiRxIiBcCiAgICAgIC1IICdDb250ZW50LVR5cGU6IHRleHQvcGxhaW4nIC0tZGF0YS1iaW5hcnkgIkAkbG9nIiAmJiByZXR1cm4gMAogICAgc2xlZXAgJCgoaSozKSkKICBkb25lCiAgbG9nZ2VyIC10IGxvYmVyICJzaWduYWwgZmFpbGVkOiAkcGhhc2UgcmM9JHJjIgogIHJldHVybiAwCn0KCnJ1bl9ob29rKCkgewogIGxvY2FsIGRpZ2VzdD0iJDEiIHJlcXVpcmVkPSIkMiIgdGltZW91dF9zZWM9IiQzIgogIGN1cmwgLWZzUyAtbyAvdG1wL2guc2ggIiRBUlQvaG9va3MvJGRpZ2VzdC9zY3JpcHQuc2giIHx8IHJldHVybiAxCiAgZWNobyAiJGRpZ2VzdCAgL3RtcC9oLnNoIiB8IHNoYTI1NnN1bSAtYyAtIHx8IHJldHVybiAxCiAgKCBzZXQgLWV1byBwaXBlZmFpbDsgdGltZW91dCAiJHRpbWVvdXRfc2VjIiBiYXNoIC90bXAvaC5zaCApIDI+JjEgfCB0ZWUgL3RtcC9oLmxvZwogIGxvY2FsIHJjPSR7UElQRVNUQVRVU1swXX0KICBzaWduYWwgImhvb2stcmVwb3J0IiAiJHJjIiAvdG1wL2gubG9nICIkZGlnZXN0IgogIFsgIiRyYyIgLWVxIDAgXSB8fCBbICIkcmVxdWlyZWQiICE9ICJ0cnVlIiBdCn0Kc2lnbmFsICJwb3N0LXN0YXJ0IiAwCnNpZ25hbCAicGFja2FnZXMtc3RhcnQiIDAKc2lnbmFsICJwYWNrYWdlcy1kb25lIiAwCmluLXRhcmdldCBiYXNoIC1jICdta2RpciAtcCAvcm9vdC8uc3NoICYmIGNobW9kIDcwMCAvcm9vdC8uc3NoCmNhdCA+PiAvcm9vdC8uc3NoL2F1dGhvcml6ZWRfa2V5cyA8PCdcJydMT0JFUl9TU0hfQVVUSE9SSVpFRF9LRVlTX0VPRidcJycKc3NoLWVkMjU1MTkgQUFBQUMzTnphQzFsWkRJMU5URTVBQUFBSUdaNVpEaGhPV013WlROaU5HWTFZelprTjJFNFlqbGpNR1V6WWpSbU5XTTIgb3BzQGxvYmVyCkxPQkVSX1NTSF9BVVRIT1JJWkVEX0tFWVNfRU9GCmNobW9kIDYwMCAvcm9vdC8uc3NoL2F1dGhvcml6ZWRfa2V5cycKaW4tdGFyZ2V0IGJhc2ggLWMgJ21rZGlyIC1wIC92YXIvbGliL2xvYmVyIC92YXIvbGliL2xvYmVyL2hvb2tzCgppbnN0YWxsIC1tIDYwMCAvZGV2L251bGwgL3Zhci9saWIvbG9iZXIvZmluYWxpemUudG9rZW4KZWNobyAnXCcnZmluYWxpemUtdG9rLWZpeGVkLTAwMDAwMDAwMDAwMDAwMDAnXCcnID4gL3Zhci9saWIvbG9iZXIvZmluYWxpemUudG9rZW4KCmluc3RhbGwgLW0gNjAwIC9kZXYvbnVsbCAvdmFyL2xpYi9sb2Jlci9wYXJhbXMuZW52CmNhdCA+IC92YXIvbGliL2xvYmVyL3BhcmFtcy5lbnYgPDwnXCcnTE9CRVJfUEFSQU1TX0VPRidcJycKTE9CRVJfQ09OVFJBQ1RfVkVSU0lPTj0nXCcnJ1wnJwpMT0JFUl9UQVNLX0lEPSdcJyc0MidcJycKTE9CRVJfU049J1wnJ1NOLUZJWEVELTAwMDEnXCcnCkxPQkVSX01BQz0nXCcnJ1wnJwpMT0JFUl9VVUlEPSdcJycnXCcnCkxPQkVSX0hPU1ROQU1FPSdcJyd3ZWItMDEnXCcnCkxPQkVSX09TX0ZBTUlMWT0nXCcnZGViaWFuJ1wnJwpMT0JFUl9PU19WRVJTSU9OPSdcJycnXCcnCkxPQkVSX0FSQ0g9J1wnJydcJycKTE9CRVJfRElTS1M9J1wnJydcJycKTE9CRVJfRElTS19QUklNQVJZPSdcJycvZGV2L2Rpc2svYnktaWQvd3duLTB4NTAwMGM1MDBmaXhlZDAwMDEnXCcnCkxPQkVSX05JQ1M9J1wnJydcJycKTE9CRVJfUkFNX01CPSdcJycnXCcnCkxPQkVSX0NQVV9DT1JFUz0nXCcnJ1wnJwpMT0JFUl9WRU5ET1I9J1wnJydcJycKTE9CRVJfUFJPRFVDVD0nXCcnJ1wnJwpMT0JFUl9CSVpfSVA9J1wnJzEwLjIwLjAuMTUnXCcnCkxPQkVSX0JJWl9HVz0nXCcnJ1wnJwpMT0JFUl9CSVpfTUFTSz0nXCcnJ1wnJwpMT0JFUl9BUlRJRkFDVF9CQVNFPSdcJydodHRwOi8vbmcuaW50ZXJuYWwvYXJ0aWZhY3RzJ1wnJwpMT0JFUl9HQVRFV0FZX0JBU0U9J1wnJ2h0dHA6Ly9ndy5pbnRlcm5hbCdcJycKTE9CRVJfUkFDSz0nXCcnJ1wnJwpMT0JFUl9ST1c9J1wnJydcJycKTE9CRVJfREM9J1wnJydcJycKTE9CRVJfUEFSQU1TX0VPRgoKY2F0ID4gL3Zhci9saWIvbG9iZXIvaG9va3MvbWFuaWZlc3QgPDwnXCcnTE9CRVJfTUFOSUZFU1RfRU9GJ1wnJwpMT0JFUl9NQU5JRkVTVF9FT0YKCmNhdCA+IC91c3IvbG9jYWwvc2Jpbi9sb2Jlci1pbml0LnNoIDw8J1wnJ0xPQkVSX0lOSVRfRU9GJ1wnJwojIS9iaW4vYmFzaAouIC92YXIvbGliL2xvYmVyL3BhcmFtcy5lbnYKCkdXPSdcJydodHRwOi8vZ3cuaW50ZXJuYWwnXCcnCkFSVD0nXCcnaHR0cDovL25nLmludGVybmFsL2FydGlmYWN0cydcJycKVEFTSz0nXCcnNDInXCcnClRPS0VOPSdcJydpbnN0YWxsLXRvay1maXhlZC0wMDAwMDAwMDAwMDAwMDAwJ1wnJwpTRVE9MAoKc2lnbmFsKCkgewogIGxvY2FsIHBoYXNlPSIkMSIgcmM9IiR7MjotMH0iIGxvZz0iJHszOi0vZGV2L251bGx9IiBkaWdlc3Q9IiR7NDotfSIKICBTRVE9JCgoU0VRKzEpKQogIGxvY2FsIHE9IiRHVy92MS9zaWduYWwvJFRBU0svJFRPS0VOLyRwaGFzZT9yYz0kcmMmc2VxPSRTRVEiCiAgaWYgWyAtbiAiJGRpZ2VzdCIgXTsgdGhlbgogICAgcT0iJHEmZGlnZXN0PSRkaWdlc3QiCiAgZmkKICBmb3IgaSBpbiAxIDIgMzsgZG8KICAgIGN1cmwgLWZzUyAtbSAxMCAtWCBQT1NUICIkcSIgXAogICAgICAtSCAnXCcnQ29udGVudC1UeXBlOiB0ZXh0L3BsYWluJ1wnJyAtLWRhdGEtYmluYXJ5ICJAJGxvZyIgJiYgcmV0dXJuIDAKICAgIHNsZWVwICQoKGkqMykpCiAgZG9uZQogIGxvZ2dlciAtdCBsb2JlciAic2lnbmFsIGZhaWxlZDogJHBoYXNlIHJjPSRyYyIKICByZXR1cm4gMAp9CgpydW5faG9vaygpIHsKICBsb2NhbCBkaWdlc3Q9IiQxIiByZXF1aXJlZD0iJDIiIHRpbWVvdXRfc2VjPSIkMyIKICBjdXJsIC1mc1MgLW8gL3RtcC9oLnNoICIkQVJUL2hvb2tzLyRkaWdlc3Qvc2NyaXB0LnNoIiB8fCByZXR1cm4gMQogIGVjaG8gIiRkaWdlc3QgIC90bXAvaC5zaCIgfCBzaGEyNTZzdW0gLWMgLSB8fCByZXR1cm4gMQogICggc2V0IC1ldW8gcGlwZWZhaWw7IHRpbWVvdXQgIiR0aW1lb3V0X3NlYyIgYmFzaCAvdG1wL2guc2ggKSAyPiYxIHwgdGVlIC90bXAvaC5sb2cKICBsb2NhbCByYz0ke1BJUEVTVEFUVVNbMF19CiAgc2lnbmFsICJob29rLXJlcG9ydCIgIiRyYyIgL3RtcC9oLmxvZyAiJGRpZ2VzdCIKICBbICIkcmMiIC1lcSAwIF0gfHwgWyAiJHJlcXVpcmVkIiAhPSAidHJ1ZSIgXQp9CgpzaWduYWwgImJvb3RlZCIgMApzaWduYWwgImluaXQtc3RhcnQiIDAKCnNpZ25hbCAiaW5pdC1kb25lIiAwCnNpZ25hbCAicmVhZHkiIDAKCnNocmVkIC11IC92YXIvbGliL2xvYmVyL3BhcmFtcy5lbnYgL3Zhci9saWIvbG9iZXIvZmluYWxpemUudG9rZW4Kcm0gLWYgL3Zhci9saWIvbG9iZXIvaW5pdC5wZW5kaW5nCnNlZCAtaSAnXCcnL3Jvb3Rwd1x8LS1pc2NyeXB0ZWRcfGluc3Qua3M9L2QnXCcnIC9yb290L2FuYWNvbmRhLWtzLmNmZyAyPi9kZXYvbnVsbApMT0JFUl9JTklUX0VPRgpjaG1vZCA3NTUgL3Vzci9sb2NhbC9zYmluL2xvYmVyLWluaXQuc2gKCmNhdCA+IC9ldGMvc3lzdGVtZC9zeXN0ZW0vbG9iZXItaW5pdC5zZXJ2aWNlIDw8J1wnJ0xPQkVSX1VOSVRfRU9GJ1wnJwpbVW5pdF0KRGVzY3JpcHRpb249TG9iZXIgT1MgaW5pdApBZnRlcj1uZXR3b3JrLW9ubGluZS50YXJnZXQKV2FudHM9bmV0d29yay1vbmxpbmUudGFyZ2V0CkNvbmRpdGlvblBhdGhFeGlzdHM9L3Zhci9saWIvbG9iZXIvaW5pdC5wZW5kaW5nCltTZXJ2aWNlXQpUeXBlPW9uZXNob3QKRXhlY1N0YXJ0PS91c3IvbG9jYWwvc2Jpbi9sb2Jlci1pbml0LnNoClN0YW5kYXJkT3V0cHV0PWpvdXJuYWwrY29uc29sZQpbSW5zdGFsbF0KV2FudGVkQnk9bXVsdGktdXNlci50YXJnZXQKTE9CRVJfVU5JVF9FT0YKCnN5c3RlbWN0bCBlbmFibGUgbG9iZXItaW5pdC5zZXJ2aWNlCnRvdWNoIC92YXIvbGliL2xvYmVyL2luaXQucGVuZGluZycKc2lnbmFsICJwb3N0LWRvbmUiIDAKc2lnbmFsICJyZWJvb3RpbmciIDAK | base64 -d > /tmp/lober-late.sh && chmod 755 /tmp/lober-late.sh && /tmp/lober-late.sh'
+0001 d-i localechooser/languagelist select en_US.UTF-8
+0002 d-i keyboard-configuration/xkb-keymap select us
+0003 d-i clock-setup/utc boolean true
+0004 d-i time/zone string UTC
+0005 d-i netcfg/choose_interface select auto
+0006 d-i netcfg/get_hostname string web-01
+0007 d-i mirror/country string manual
+0008 d-i mirror/http/hostname string ng.internal
+0009 d-i mirror/http/directory string /artifacts/fixture/repo/
+0010 d-i mirror/http/proxy string
+0011 d-i partman-auto/disk string /dev/sda
+0012 d-i partman-auto/method string regular
+0013 d-i partman-auto/choose_recipe select atomic
+0014 d-i partman-efi/non_efi_system boolean true
+0015 d-i partman-partitioning/confirm_write_new_label boolean true
+0016 d-i partman/choose_partition select finish
+0017 d-i partman/confirm boolean true
+0018 d-i partman/confirm_nooverwrite boolean true
+0019 d-i passwd/root-login boolean false
+0020 d-i passwd/make-user boolean false
+0021 tasksel tasksel/first multiselect standard
+0022 d-i pkgsel/include string curl
+0023 popularity-contest popularity-contest/participate boolean false
+0024 d-i preseed/early_command string echo c2V0IC1ldQp1bWFzayAwNzcKbWtkaXIgLXAgJy90bXAnCndnZXQgLU8gJy90bXAvc2NyaXB0LTIuc2gnICdodHRwOi8vZ3cuaW50ZXJuYWwvdjEvYW5zd2VyL3Rhc2stNDIvaW5zdGFsbC10b2stZml4ZWQvc2NyaXB0cy8yLnNoJwpzaCAnL3RtcC9zY3JpcHQtMi5zaCcK | base64 -d | sh
+0025 d-i preseed/late_command string echo c2V0IC1ldQp1bWFzayAwNzcKbWtkaXIgLXAgJy90bXAnCndnZXQgLU8gJy90bXAvc2NyaXB0LTMuc2gnICdodHRwOi8vZ3cuaW50ZXJuYWwvdjEvYW5zd2VyL3Rhc2stNDIvaW5zdGFsbC10b2stZml4ZWQvc2NyaXB0cy8zLnNoJwpzaCAnL3RtcC9zY3JpcHQtMy5zaCcK | base64 -d | sh && echo c2V0IC1ldQp1bWFzayAwNzcKbWtkaXIgLXAgJy90YXJnZXQvdmFyL2xpYi9sb2JlcicKd2dldCAtTyAnL3RhcmdldC92YXIvbGliL2xvYmVyL3NjcmlwdC00LnNoJyAnaHR0cDovL2d3LmludGVybmFsL3YxL2Fuc3dlci90YXNrLTQyL2luc3RhbGwtdG9rLWZpeGVkL3NjcmlwdHMvNC5zaCcKY2hyb290ICcvdGFyZ2V0JyBzaCAnL3Zhci9saWIvbG9iZXIvc2NyaXB0LTQuc2gnCg== | base64 -d | sh && echo c2V0IC1ldQp1bWFzayAwNzcKbWtkaXIgLXAgJy90YXJnZXQvdmFyL2xpYi9sb2JlcicKd2dldCAtTyAnL3RhcmdldC92YXIvbGliL2xvYmVyL3NjcmlwdC01LnNoJyAnaHR0cDovL2d3LmludGVybmFsL3YxL2Fuc3dlci90YXNrLTQyL2luc3RhbGwtdG9rLWZpeGVkL3NjcmlwdHMvNS5zaCcK | base64 -d | sh && echo c2V0IC1ldQp1bWFzayAwNzcKbWtkaXIgLXAgJy90YXJnZXQvdmFyL2xpYi9sb2JlcicgJy90YXJnZXQvZXRjL3N5c3RlbWQvc3lzdGVtL211bHRpLXVzZXIudGFyZ2V0LndhbnRzJwpwcmludGYgJyVzJyAnZmluYWxpemUtdG9rLWZpeGVkJyA+ICcvdGFyZ2V0L3Zhci9saWIvbG9iZXIvZmluYWxpemUudG9rZW4nCmNhdCA+ICcvdGFyZ2V0L3Zhci9saWIvbG9iZXIvZmlyc3Rib290LnNoJyA8PCdMT0JFUl9GSVJTVEJPT1RfRU9GJwojIS9iaW4vc2gKc2V0IC1ldQpUT0tFTj0kKGNhdCAvdmFyL2xpYi9sb2Jlci9maW5hbGl6ZS50b2tlbikKY3VybCAtZnNTIC1tIDEwIC1YIFBPU1QgJ2h0dHA6Ly9ndy5pbnRlcm5hbC92MS9zaWduYWwvdGFzay00Mi8nIiRUT0tFTiInL2Jvb3RlZCcgLS1kYXRhLWJpbmFyeSAnJyA+L2Rldi9udWxsIDI+JjEgfHwgOgpUT0tFTj0kKGNhdCAvdmFyL2xpYi9sb2Jlci9maW5hbGl6ZS50b2tlbikKY3VybCAtZnNTIC1tIDEwIC1YIFBPU1QgJ2h0dHA6Ly9ndy5pbnRlcm5hbC92MS9zaWduYWwvdGFzay00Mi8nIiRUT0tFTiInL2luaXQtc3RhcnQnIC0tZGF0YS1iaW5hcnkgJycgPi9kZXYvbnVsbCAyPiYxIHx8IDoKc2ggL3Zhci9saWIvbG9iZXIvc2NyaXB0LTUuc2gKc2ggL3Zhci9saWIvbG9iZXIvY2xlYW51cC5zaApMT0JFUl9GSVJTVEJPT1RfRU9GCmNhdCA+ICcvdGFyZ2V0L2V0Yy9zeXN0ZW1kL3N5c3RlbS9sb2Jlci1maXJzdGJvb3Quc2VydmljZScgPDwnTE9CRVJfVU5JVF9FT0YnCltVbml0XQpEZXNjcmlwdGlvbj1Mb2JlciBmaXJzdCBib290CkFmdGVyPW5ldHdvcmstb25saW5lLnRhcmdldApXYW50cz1uZXR3b3JrLW9ubGluZS50YXJnZXQKQ29uZGl0aW9uUGF0aEV4aXN0cz0vdmFyL2xpYi9sb2Jlci9maXJzdGJvb3QucGVuZGluZwpbU2VydmljZV0KVHlwZT1vbmVzaG90CkV4ZWNTdGFydD0vYmluL3NoIC92YXIvbGliL2xvYmVyL2ZpcnN0Ym9vdC5zaApbSW5zdGFsbF0KV2FudGVkQnk9bXVsdGktdXNlci50YXJnZXQKTE9CRVJfVU5JVF9FT0YKbG4gLXNmIC4uL2xvYmVyLWZpcnN0Ym9vdC5zZXJ2aWNlICcvdGFyZ2V0L2V0Yy9zeXN0ZW1kL3N5c3RlbS9tdWx0aS11c2VyLnRhcmdldC53YW50cy9sb2Jlci1maXJzdGJvb3Quc2VydmljZScKdG91Y2ggJy90YXJnZXQvdmFyL2xpYi9sb2Jlci9maXJzdGJvb3QucGVuZGluZycK | base64 -d | sh && echo c2V0IC1ldQp1bWFzayAwNzcKbWtkaXIgLXAgJy90YXJnZXQvdmFyL2xpYi9sb2JlcicKY2F0ID4gJy90YXJnZXQvdmFyL2xpYi9sb2Jlci9jbGVhbnVwLnNoJyA8PCdMT0JFUl9DTEVBTlVQX0VPRicKIyEvYmluL3NoCnNldCAtZXUKVE9LRU49JChjYXQgL3Zhci9saWIvbG9iZXIvZmluYWxpemUudG9rZW4pCmN1cmwgLWZzUyAtbSAxMCAtWCBQT1NUICdodHRwOi8vZ3cuaW50ZXJuYWwvdjEvc2lnbmFsL3Rhc2stNDIvJyIkVE9LRU4iJy9pbml0LWRvbmUnIC0tZGF0YS1iaW5hcnkgJycgPi9kZXYvbnVsbCAyPiYxIHx8IDoKVE9LRU49JChjYXQgL3Zhci9saWIvbG9iZXIvZmluYWxpemUudG9rZW4pCmN1cmwgLWZzUyAtbSAxMCAtWCBQT1NUICdodHRwOi8vZ3cuaW50ZXJuYWwvdjEvc2lnbmFsL3Rhc2stNDIvJyIkVE9LRU4iJy9yZWFkeScgLS1kYXRhLWJpbmFyeSAnJyA+L2Rldi9udWxsIDI+JjEgfHwgOgpybSAtZiAvdmFyL2xpYi9sb2Jlci9maW5hbGl6ZS50b2tlbiAvdmFyL2xpYi9sb2Jlci9maXJzdGJvb3QucGVuZGluZyAvdmFyL2xpYi9sb2Jlci9zY3JpcHQtKi5zaCAvdmFyL2xpYi9sb2Jlci9ob29rLSogL3Zhci9saWIvbG9iZXIvZmlyc3Rib290LnNoIC92YXIvbGliL2xvYmVyL2NsZWFudXAuc2gKcm0gLWYgL3Jvb3QvYW5hY29uZGEta3MuY2ZnIC9yb290L29yaWdpbmFsLWtzLmNmZyAvdmFyL2xvZy9pbnN0YWxsZXIvYXV0b2luc3RhbGwtdXNlci1kYXRhIC92YXIvbG9nL2luc3RhbGxlci9hdXRvaW5zdGFsbC11c2VyLWRhdGEudHh0IC92YXIvbG9nL2luc3RhbGxlci9jZGViY29uZi9xdWVzdGlvbnMuZGF0IC92YXIvYWRtL2F1dG9pbnN0YWxsL2NhY2hlL2luc3RhbGxlZFN5c3RlbS54bWwKTE9CRVJfQ0xFQU5VUF9FT0YK | base64 -d | sh
+0026 d-i finish-install/reboot_in_progress note
```

## autoinstall-full-bios.golden

Compared with `autoinstall_full.golden`; firmware was not split in that old reference.

### Old 1–0; new 1–1

S2: handwritten YAML fixture uses cloud-config header, ordinary lober username rather than reserved root, explicit firmware GPT storage, match-based DHCP NIC, and curl package. Inline multiline early/late bodies become one-line commands; /target is installer mount, target script chroots. SSH key composition is hook-owned, not implicit renderer policy. shutdown/reboot remains native profile syntax.

S5: all remaining profile/blank/layout/value differences in this range are directly authored in the committed .tmpl/.fragment snapshots: hostname remains web-01; identity is task-42 (valid model ID) rather than old numeric 42; new tokens shorter but valid; full includes one real script per supported timing, minimal keeps the slot empty (still receives runtime wrapper for stage signals); SSH keys/legacy host metadata omitted from independent assembly fixtures because Task9 owns actual builtin defaults and SSH hook. No implicit old-model field assembly remains.

```diff
+0001 #cloud-config
```

### Old 2–391; new 3–37

S1: fixed global environment and inline signal/run_hook library are removed; flattened snapshot instances and FileScript now own per-instance EnvName/EnvValue, digest/interpreter/timeout/required execution. Task6 inserts one-line wrapper downloads, never copies old inline runtime. Legacy fake digest strings become real SHA-256 fixture digest. Task8 implements/verifies runtime bodies.

B1: firstboot now stages local slot wrappers and local digest payloads before reboot; systemd invokes the installed native runner. Boot credential no longer enters firstboot; independent local finalize token used. Cleanup follows success and erases cached answers/payloads. Legacy firstboot reused install TOKEN despite writing finalize.token. Unit/marker/local paths change to this explicit contract.

S2: handwritten YAML fixture uses cloud-config header, ordinary lober username rather than reserved root, explicit firmware GPT storage, match-based DHCP NIC, and curl package. Inline multiline early/late bodies become one-line commands; /target is installer mount, target script chroots. SSH key composition is hook-owned, not implicit renderer policy. shutdown/reboot remains native profile syntax.

S5: all remaining profile/blank/layout/value differences in this range are directly authored in the committed .tmpl/.fragment snapshots: hostname remains web-01; identity is task-42 (valid model ID) rather than old numeric 42; new tokens shorter but valid; full includes one real script per supported timing, minimal keeps the slot empty (still receives runtime wrapper for stage signals); SSH keys/legacy host metadata omitted from independent assembly fixtures because Task9 owns actual builtin defaults and SSH hook. No implicit old-model field assembly remains.

```diff
-0002     version: 1
-0003     locale: en_US.UTF-8
-0004     keyboard:
-0005         layout: us
-0006     identity:
-0007         hostname: web-01
-0008         username: root
-0009         password: '!'
-0010     storage:
-0011         layout:
-0012             name: direct
-0013     network:
-0014         version: 2
-0015         ethernets:
-0016             install-nic:
-0017                 dhcp4: true
-0018     early-commands:
-0019         - |-
-0020           LOBER_CONTRACT_VERSION=''
-0021           LOBER_TASK_ID='42'
-0022           LOBER_SN='SN-FIXED-0001'
-0023           LOBER_MAC=''
-0024           LOBER_UUID=''
-0025           LOBER_HOSTNAME='web-01'
-0026           LOBER_OS_FAMILY='ubuntu'
-0027           LOBER_OS_VERSION=''
-0028           LOBER_ARCH=''
-0029           LOBER_DISKS=''
-0030           LOBER_DISK_PRIMARY='/dev/disk/by-id/wwn-0x5000c500fixed0001'
-0031           LOBER_NICS=''
-0032           LOBER_RAM_MB=''
-0033           LOBER_CPU_CORES=''
-0034           LOBER_VENDOR=''
-0035           LOBER_PRODUCT=''
-0036           LOBER_BIZ_IP='10.20.0.15'
-0037           LOBER_BIZ_GW=''
-0038           LOBER_BIZ_MASK=''
-0039           LOBER_ARTIFACT_BASE='http://ng.internal/artifacts'
-0040           LOBER_GATEWAY_BASE='http://gw.internal'
-0041           LOBER_RACK=''
-0042           LOBER_ROW=''
-0043           LOBER_DC=''
-0044           GW='http://gw.internal'
-0045           ART='http://ng.internal/artifacts'
-0046           TASK='42'
-0047           TOKEN='install-tok-fixed-0000000000000000'
-0048           SEQ=0
-0049 
-0050           signal() {
-0051             local phase="$1" rc="${2:-0}" log="${3:-/dev/null}" digest="${4:-}"
-0052             SEQ=$((SEQ+1))
-0053             local q="$GW/v1/signal/$TASK/$TOKEN/$phase?rc=$rc&seq=$SEQ"
-0054             if [ -n "$digest" ]; then
-0055               q="$q&digest=$digest"
-0056             fi
-0057             for i in 1 2 3; do
-0058               curl -fsS -m 10 -X POST "$q" \
-0059                 -H 'Content-Type: text/plain' --data-binary "@$log" && return 0
-0060               sleep $((i*3))
-0061             done
-0062             logger -t lober "signal failed: $phase rc=$rc"
-0063             return 0
-0064           }
-0065 
-0066           run_hook() {
-0067             local digest="$1" required="$2" timeout_sec="$3"
-0068             curl -fsS -o /tmp/h.sh "$ART/hooks/$digest/script.sh" || return 1
-0069             echo "$digest  /tmp/h.sh" | sha256sum -c - || return 1
-0070             ( set -euo pipefail; timeout "$timeout_sec" bash /tmp/h.sh ) 2>&1 | tee /tmp/h.log
-0071             local rc=${PIPESTATUS[0]}
-0072             signal "hook-report" "$rc" /tmp/h.log "$digest"
-0073             [ "$rc" -eq 0 ] || [ "$required" != "true" ]
-0074           }
-0075           signal "pre-start" 0
-0076         - |-
-0077           GW='http://gw.internal'
-0078           ART='http://ng.internal/artifacts'
-0079           TASK='42'
-0080           TOKEN='install-tok-fixed-0000000000000000'
-0081           SEQ=0
-0082 
-0083           signal() {
-0084             local phase="$1" rc="${2:-0}" log="${3:-/dev/null}" digest="${4:-}"
-0085             SEQ=$((SEQ+1))
-0086             local q="$GW/v1/signal/$TASK/$TOKEN/$phase?rc=$rc&seq=$SEQ"
-0087             if [ -n "$digest" ]; then
-0088               q="$q&digest=$digest"
-0089             fi
-0090             for i in 1 2 3; do
-0091               curl -fsS -m 10 -X POST "$q" \
-0092                 -H 'Content-Type: text/plain' --data-binary "@$log" && return 0
-0093               sleep $((i*3))
-0094             done
-0095             logger -t lober "signal failed: $phase rc=$rc"
-0096             return 0
-0097           }
-0098 
-0099           run_hook() {
-0100             local digest="$1" required="$2" timeout_sec="$3"
-0101             curl -fsS -o /tmp/h.sh "$ART/hooks/$digest/script.sh" || return 1
-0102             echo "$digest  /tmp/h.sh" | sha256sum -c - || return 1
-0103             ( set -euo pipefail; timeout "$timeout_sec" bash /tmp/h.sh ) 2>&1 | tee /tmp/h.log
-0104             local rc=${PIPESTATUS[0]}
-0105             signal "hook-report" "$rc" /tmp/h.log "$digest"
-0106             [ "$rc" -eq 0 ] || [ "$required" != "true" ]
-0107           }
-0108           run_hook 'aaaa1111pre' 'true' '300' || exit 1
-0109         - |-
-0110           GW='http://gw.internal'
-0111           ART='http://ng.internal/artifacts'
-0112           TASK='42'
-0113           TOKEN='install-tok-fixed-0000000000000000'
-0114           SEQ=0
-0115 
-0116           signal() {
-0117             local phase="$1" rc="${2:-0}" log="${3:-/dev/null}" digest="${4:-}"
-0118             SEQ=$((SEQ+1))
-0119             local q="$GW/v1/signal/$TASK/$TOKEN/$phase?rc=$rc&seq=$SEQ"
-0120             if [ -n "$digest" ]; then
-0121               q="$q&digest=$digest"
-0122             fi
-0123             for i in 1 2 3; do
-0124               curl -fsS -m 10 -X POST "$q" \
-0125                 -H 'Content-Type: text/plain' --data-binary "@$log" && return 0
-0126               sleep $((i*3))
-0127             done
-0128             logger -t lober "signal failed: $phase rc=$rc"
-0129             return 0
-0130           }
-0131 
-0132           run_hook() {
-0133             local digest="$1" required="$2" timeout_sec="$3"
-0134             curl -fsS -o /tmp/h.sh "$ART/hooks/$digest/script.sh" || return 1
-0135             echo "$digest  /tmp/h.sh" | sha256sum -c - || return 1
-0136             ( set -euo pipefail; timeout "$timeout_sec" bash /tmp/h.sh ) 2>&1 | tee /tmp/h.log
-0137             local rc=${PIPESTATUS[0]}
-0138             signal "hook-report" "$rc" /tmp/h.log "$digest"
-0139             [ "$rc" -eq 0 ] || [ "$required" != "true" ]
-0140           }
-0141           signal "pre-done" 0
-0142     late-commands:
-0143         - |-
-0144           GW='http://gw.internal'
-0145           ART='http://ng.internal/artifacts'
-0146           TASK='42'
-0147           TOKEN='install-tok-fixed-0000000000000000'
-0148           SEQ=0
-0149 
-0150           signal() {
-0151             local phase="$1" rc="${2:-0}" log="${3:-/dev/null}" digest="${4:-}"
-0152             SEQ=$((SEQ+1))
-0153             local q="$GW/v1/signal/$TASK/$TOKEN/$phase?rc=$rc&seq=$SEQ"
-0154             if [ -n "$digest" ]; then
-0155               q="$q&digest=$digest"
-0156             fi
-0157             for i in 1 2 3; do
-0158               curl -fsS -m 10 -X POST "$q" \
-0159                 -H 'Content-Type: text/plain' --data-binary "@$log" && return 0
-0160               sleep $((i*3))
-0161             done
-0162             logger -t lober "signal failed: $phase rc=$rc"
-0163             return 0
-0164           }
-0165 
-0166           run_hook() {
-0167             local digest="$1" required="$2" timeout_sec="$3"
-0168             curl -fsS -o /tmp/h.sh "$ART/hooks/$digest/script.sh" || return 1
-0169             echo "$digest  /tmp/h.sh" | sha256sum -c - || return 1
-0170             ( set -euo pipefail; timeout "$timeout_sec" bash /tmp/h.sh ) 2>&1 | tee /tmp/h.log
-0171             local rc=${PIPESTATUS[0]}
-0172             signal "hook-report" "$rc" /tmp/h.log "$digest"
-0173             [ "$rc" -eq 0 ] || [ "$required" != "true" ]
-0174           }
-0175           signal "post-start" 0
-0176           signal "packages-start" 0
-0177           signal "packages-done" 0
-0178         - |-
-0179           GW='http://gw.internal'
-0180           ART='http://ng.internal/artifacts'
-0181           TASK='42'
-0182           TOKEN='install-tok-fixed-0000000000000000'
-0183           SEQ=0
-0184 
-0185           signal() {
-0186             local phase="$1" rc="${2:-0}" log="${3:-/dev/null}" digest="${4:-}"
-0187             SEQ=$((SEQ+1))
-0188             local q="$GW/v1/signal/$TASK/$TOKEN/$phase?rc=$rc&seq=$SEQ"
-0189             if [ -n "$digest" ]; then
-0190               q="$q&digest=$digest"
-0191             fi
-0192             for i in 1 2 3; do
-0193               curl -fsS -m 10 -X POST "$q" \
-0194                 -H 'Content-Type: text/plain' --data-binary "@$log" && return 0
-0195               sleep $((i*3))
-0196             done
-0197             logger -t lober "signal failed: $phase rc=$rc"
-0198             return 0
-0199           }
-0200 
-0201           run_hook() {
-0202             local digest="$1" required="$2" timeout_sec="$3"
-0203             curl -fsS -o /tmp/h.sh "$ART/hooks/$digest/script.sh" || return 1
-0204             echo "$digest  /tmp/h.sh" | sha256sum -c - || return 1
-0205             ( set -euo pipefail; timeout "$timeout_sec" bash /tmp/h.sh ) 2>&1 | tee /tmp/h.log
-0206             local rc=${PIPESTATUS[0]}
-0207             signal "hook-report" "$rc" /tmp/h.log "$digest"
-0208             [ "$rc" -eq 0 ] || [ "$required" != "true" ]
-0209           }
-0210           run_hook 'bbbb2222nochroot' 'false' '300' || exit 1
-0211         - |-
-0212           curtin in-target -- bash -c 'GW='\''http://gw.internal'\''
-0213           ART='\''http://ng.internal/artifacts'\''
-0214           TASK='\''42'\''
-0215           TOKEN='\''install-tok-fixed-0000000000000000'\''
-0216           SEQ=0
-0217 
-0218           signal() {
-0219             local phase="$1" rc="${2:-0}" log="${3:-/dev/null}" digest="${4:-}"
-0220             SEQ=$((SEQ+1))
-0221             local q="$GW/v1/signal/$TASK/$TOKEN/$phase?rc=$rc&seq=$SEQ"
-0222             if [ -n "$digest" ]; then
-0223               q="$q&digest=$digest"
-0224             fi
-0225             for i in 1 2 3; do
-0226               curl -fsS -m 10 -X POST "$q" \
-0227                 -H '\''Content-Type: text/plain'\'' --data-binary "@$log" && return 0
-0228               sleep $((i*3))
-0229             done
-0230             logger -t lober "signal failed: $phase rc=$rc"
-0231             return 0
-0232           }
-0233 
-0234           run_hook() {
-0235             local digest="$1" required="$2" timeout_sec="$3"
-0236             curl -fsS -o /tmp/h.sh "$ART/hooks/$digest/script.sh" || return 1
-0237             echo "$digest  /tmp/h.sh" | sha256sum -c - || return 1
-0238             ( set -euo pipefail; timeout "$timeout_sec" bash /tmp/h.sh ) 2>&1 | tee /tmp/h.log
-0239             local rc=${PIPESTATUS[0]}
-0240             signal "hook-report" "$rc" /tmp/h.log "$digest"
-0241             [ "$rc" -eq 0 ] || [ "$required" != "true" ]
-0242           }
-0243           run_hook '\''cccc3333post'\'' '\''true'\'' '\''300'\'' || exit 1'
-0244         - |-
-0245           curtin in-target -- bash -c 'mkdir -p /var/lib/lober /var/lib/lober/hooks
-0246 
-0247           install -m 600 /dev/null /var/lib/lober/finalize.token
-0248           echo '\''finalize-tok-fixed-0000000000000000'\'' > /var/lib/lober/finalize.token
-0249 
-0250           install -m 600 /dev/null /var/lib/lober/params.env
-0251           cat > /var/lib/lober/params.env <<'\''LOBER_PARAMS_EOF'\''
-0252           LOBER_CONTRACT_VERSION='\'''\''
-0253           LOBER_TASK_ID='\''42'\''
-0254           LOBER_SN='\''SN-FIXED-0001'\''
-0255           LOBER_MAC='\'''\''
-0256           LOBER_UUID='\'''\''
-0257           LOBER_HOSTNAME='\''web-01'\''
-0258           LOBER_OS_FAMILY='\''ubuntu'\''
-0259           LOBER_OS_VERSION='\'''\''
-0260           LOBER_ARCH='\'''\''
-0261           LOBER_DISKS='\'''\''
-0262           LOBER_DISK_PRIMARY='\''/dev/disk/by-id/wwn-0x5000c500fixed0001'\''
-0263           LOBER_NICS='\'''\''
-0264           LOBER_RAM_MB='\'''\''
-0265           LOBER_CPU_CORES='\'''\''
-0266           LOBER_VENDOR='\'''\''
-0267           LOBER_PRODUCT='\'''\''
-0268           LOBER_BIZ_IP='\''10.20.0.15'\''
-0269           LOBER_BIZ_GW='\'''\''
-0270           LOBER_BIZ_MASK='\'''\''
-0271           LOBER_ARTIFACT_BASE='\''http://ng.internal/artifacts'\''
-0272           LOBER_GATEWAY_BASE='\''http://gw.internal'\''
-0273           LOBER_RACK='\'''\''
-0274           LOBER_ROW='\'''\''
-0275           LOBER_DC='\'''\''
-0276           LOBER_PARAMS_EOF
-0277 
-0278           cat > /var/lib/lober/hooks/manifest <<'\''LOBER_MANIFEST_EOF'\''
-0279           010-dddd4444firstboot.sh digest=dddd4444firstboot required=true order=10
-0280           LOBER_MANIFEST_EOF
-0281 
-0282           curl -fsS -o /var/lib/lober/hooks/010-dddd4444firstboot.sh '\''http://ng.internal/artifacts/hooks/dddd4444firstboot/script.sh'\''
-0283           echo '\''dddd4444firstboot  /var/lib/lober/hooks/010-dddd4444firstboot.sh'\'' | sha256sum -c -
-0284           chmod 700 /var/lib/lober/hooks/010-dddd4444firstboot.sh
-0285 
-0286           cat > /usr/local/sbin/lober-init.sh <<'\''LOBER_INIT_EOF'\''
-0287           #!/bin/bash
-0288           . /var/lib/lober/params.env
-0289 
-0290           GW='\''http://gw.internal'\''
-0291           ART='\''http://ng.internal/artifacts'\''
-0292           TASK='\''42'\''
-0293           TOKEN='\''install-tok-fixed-0000000000000000'\''
-0294           SEQ=0
-0295 
-0296           signal() {
-0297             local phase="$1" rc="${2:-0}" log="${3:-/dev/null}" digest="${4:-}"
-0298             SEQ=$((SEQ+1))
-0299             local q="$GW/v1/signal/$TASK/$TOKEN/$phase?rc=$rc&seq=$SEQ"
-0300             if [ -n "$digest" ]; then
-0301               q="$q&digest=$digest"
-0302             fi
-0303             for i in 1 2 3; do
-0304               curl -fsS -m 10 -X POST "$q" \
-0305                 -H '\''Content-Type: text/plain'\'' --data-binary "@$log" && return 0
-0306               sleep $((i*3))
-0307             done
-0308             logger -t lober "signal failed: $phase rc=$rc"
-0309             return 0
-0310           }
-0311 
-0312           run_hook() {
-0313             local digest="$1" required="$2" timeout_sec="$3"
-0314             curl -fsS -o /tmp/h.sh "$ART/hooks/$digest/script.sh" || return 1
-0315             echo "$digest  /tmp/h.sh" | sha256sum -c - || return 1
-0316             ( set -euo pipefail; timeout "$timeout_sec" bash /tmp/h.sh ) 2>&1 | tee /tmp/h.log
-0317             local rc=${PIPESTATUS[0]}
-0318             signal "hook-report" "$rc" /tmp/h.log "$digest"
-0319             [ "$rc" -eq 0 ] || [ "$required" != "true" ]
-0320           }
-0321 
-0322           signal "booted" 0
-0323           signal "init-start" 0
-0324 
-0325           bash /var/lib/lober/hooks/010-dddd4444firstboot.sh > /tmp/fb-dddd4444firstboot.log 2>&1
-0326           signal "hook-report" "$?" /tmp/fb-dddd4444firstboot.log "dddd4444firstboot"
-0327 
-0328           signal "init-done" 0
-0329           signal "ready" 0
-0330 
-0331           shred -u /var/lib/lober/params.env /var/lib/lober/finalize.token
-0332           rm -f /var/lib/lober/init.pending
-0333           sed -i '\''/rootpw\|--iscrypted\|inst.ks=/d'\'' /root/anaconda-ks.cfg 2>/dev/null
-0334           LOBER_INIT_EOF
-0335           chmod 755 /usr/local/sbin/lober-init.sh
-0336 
-0337           cat > /etc/systemd/system/lober-init.service <<'\''LOBER_UNIT_EOF'\''
-0338           [Unit]
-0339           Description=Lober OS init
-0340           After=network-online.target
-0341           Wants=network-online.target
-0342           ConditionPathExists=/var/lib/lober/init.pending
-0343           [Service]
-0344           Type=oneshot
-0345           ExecStart=/usr/local/sbin/lober-init.sh
-0346           StandardOutput=journal+console
-0347           [Install]
-0348           WantedBy=multi-user.target
-0349           LOBER_UNIT_EOF
-0350 
-0351           systemctl enable lober-init.service
-0352           touch /var/lib/lober/init.pending'
-0353         - |-
-0354           GW='http://gw.internal'
-0355           ART='http://ng.internal/artifacts'
-0356           TASK='42'
-0357           TOKEN='install-tok-fixed-0000000000000000'
-0358           SEQ=0
-0359 
-0360           signal() {
-0361             local phase="$1" rc="${2:-0}" log="${3:-/dev/null}" digest="${4:-}"
-0362             SEQ=$((SEQ+1))
-0363             local q="$GW/v1/signal/$TASK/$TOKEN/$phase?rc=$rc&seq=$SEQ"
-0364             if [ -n "$digest" ]; then
-0365               q="$q&digest=$digest"
-0366             fi
-0367             for i in 1 2 3; do
-0368               curl -fsS -m 10 -X POST "$q" \
-0369                 -H 'Content-Type: text/plain' --data-binary "@$log" && return 0
-0370               sleep $((i*3))
-0371             done
-0372             logger -t lober "signal failed: $phase rc=$rc"
-0373             return 0
-0374           }
-0375 
-0376           run_hook() {
-0377             local digest="$1" required="$2" timeout_sec="$3"
-0378             curl -fsS -o /tmp/h.sh "$ART/hooks/$digest/script.sh" || return 1
-0379             echo "$digest  /tmp/h.sh" | sha256sum -c - || return 1
-0380             ( set -euo pipefail; timeout "$timeout_sec" bash /tmp/h.sh ) 2>&1 | tee /tmp/h.log
-0381             local rc=${PIPESTATUS[0]}
-0382             signal "hook-report" "$rc" /tmp/h.log "$digest"
-0383             [ "$rc" -eq 0 ] || [ "$required" != "true" ]
-0384           }
-0385           signal "post-done" 0
-0386           signal "rebooting" 0
-0387     packages: []
-0388     ssh:
-0389         install-server: true
-0390         authorized-keys:
-0391             - ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIGZ5ZDhhOWMwZTNiNGY1YzZkN2E4YjljMGUzYjRmNWM2 ops@lober
+0003   version: 1
+0004   locale: en_US.UTF-8
+0005   keyboard:
+0006     layout: us
+0007   identity:
+0008     hostname: web-01
+0009     username: lober
+0010     password: '!'
+0011   storage:
+0012     config:
+0013       - {id: disk0, type: disk, path: /dev/sda, ptable: gpt, wipe: superblock-recursive, grub_device: true}
+0014       - {id: bios, type: partition, device: disk0, size: 1M, flag: bios_grub}
+0015       - {id: root, type: partition, device: disk0, size: -1}
+0016       - {id: root-fs, type: format, volume: root, fstype: ext4}
+0017       - {id: root-mount, type: mount, device: root-fs, path: /}
+0018   network:
+0019     version: 2
+0020     ethernets:
+0021       install-nic:
+0022         match:
+0023           name: en*
+0024         dhcp4: true
+0025   early-commands:
+0026     - echo c2V0IC1ldQp1bWFzayAwNzcKbWtkaXIgLXAgJy90bXAnCmN1cmwgLWZzUyAtLXJldHJ5IDMgLW8gJy90bXAvc2NyaXB0LTIuc2gnICdodHRwOi8vZ3cuaW50ZXJuYWwvdjEvYW5zd2VyL3Rhc2stNDIvaW5zdGFsbC10b2stZml4ZWQvc2NyaXB0cy8yLnNoJwpzaCAnL3RtcC9zY3JpcHQtMi5zaCcK | base64 -d | sh
+0027   late-commands:
+0028     - echo c2V0IC1ldQp1bWFzayAwNzcKbWtkaXIgLXAgJy90bXAnCmN1cmwgLWZzUyAtLXJldHJ5IDMgLW8gJy90bXAvc2NyaXB0LTMuc2gnICdodHRwOi8vZ3cuaW50ZXJuYWwvdjEvYW5zd2VyL3Rhc2stNDIvaW5zdGFsbC10b2stZml4ZWQvc2NyaXB0cy8zLnNoJwpzaCAnL3RtcC9zY3JpcHQtMy5zaCcK | base64 -d | sh
+0029     - echo c2V0IC1ldQp1bWFzayAwNzcKbWtkaXIgLXAgJy90YXJnZXQvdmFyL2xpYi9sb2JlcicKY3VybCAtZnNTIC0tcmV0cnkgMyAtbyAnL3RhcmdldC92YXIvbGliL2xvYmVyL3NjcmlwdC00LnNoJyAnaHR0cDovL2d3LmludGVybmFsL3YxL2Fuc3dlci90YXNrLTQyL2luc3RhbGwtdG9rLWZpeGVkL3NjcmlwdHMvNC5zaCcKY2hyb290ICcvdGFyZ2V0JyBzaCAnL3Zhci9saWIvbG9iZXIvc2NyaXB0LTQuc2gnCg== | base64 -d | sh
+0030     - echo c2V0IC1ldQp1bWFzayAwNzcKbWtkaXIgLXAgJy90YXJnZXQvdmFyL2xpYi9sb2JlcicKY3VybCAtZnNTIC0tcmV0cnkgMyAtbyAnL3RhcmdldC92YXIvbGliL2xvYmVyL3NjcmlwdC01LnNoJyAnaHR0cDovL2d3LmludGVybmFsL3YxL2Fuc3dlci90YXNrLTQyL2luc3RhbGwtdG9rLWZpeGVkL3NjcmlwdHMvNS5zaCcKY3VybCAtZnNTIC0tcmV0cnkgMyAtbyAnL3RhcmdldC92YXIvbGliL2xvYmVyL2hvb2stM2VlMDQ5ZTU5MWY1ZTk5YWU4ZjQ5OGFlMzgxZjQ1OTM2NWMyMjFlNDFlNmI0MmYyYWQ2ZTlhNmE5NzUwNTMyZS5zaCcgJ2h0dHA6Ly9ndy5pbnRlcm5hbC92MS9hbnN3ZXIvdGFzay00Mi9pbnN0YWxsLXRvay1maXhlZC9ob29rcy8zZWUwNDllNTkxZjVlOTlhZThmNDk4YWUzODFmNDU5MzY1YzIyMWU0MWU2YjQyZjJhZDZlOWE2YTk3NTA1MzJlJwo= | base64 -d | sh
+0031     - echo c2V0IC1ldQp1bWFzayAwNzcKbWtkaXIgLXAgJy90YXJnZXQvdmFyL2xpYi9sb2JlcicgJy90YXJnZXQvZXRjL3N5c3RlbWQvc3lzdGVtL211bHRpLXVzZXIudGFyZ2V0LndhbnRzJwpwcmludGYgJyVzJyAnZmluYWxpemUtdG9rLWZpeGVkJyA+ICcvdGFyZ2V0L3Zhci9saWIvbG9iZXIvZmluYWxpemUudG9rZW4nCmNhdCA+ICcvdGFyZ2V0L3Zhci9saWIvbG9iZXIvZmlyc3Rib290LnNoJyA8PCdMT0JFUl9GSVJTVEJPT1RfRU9GJwojIS9iaW4vc2gKc2V0IC1ldQpUT0tFTj0kKGNhdCAvdmFyL2xpYi9sb2Jlci9maW5hbGl6ZS50b2tlbikKY3VybCAtZnNTIC1tIDEwIC1YIFBPU1QgJ2h0dHA6Ly9ndy5pbnRlcm5hbC92MS9zaWduYWwvdGFzay00Mi8nIiRUT0tFTiInL2Jvb3RlZCcgLS1kYXRhLWJpbmFyeSAnJyA+L2Rldi9udWxsIDI+JjEgfHwgOgpUT0tFTj0kKGNhdCAvdmFyL2xpYi9sb2Jlci9maW5hbGl6ZS50b2tlbikKY3VybCAtZnNTIC1tIDEwIC1YIFBPU1QgJ2h0dHA6Ly9ndy5pbnRlcm5hbC92MS9zaWduYWwvdGFzay00Mi8nIiRUT0tFTiInL2luaXQtc3RhcnQnIC0tZGF0YS1iaW5hcnkgJycgPi9kZXYvbnVsbCAyPiYxIHx8IDoKc2ggL3Zhci9saWIvbG9iZXIvc2NyaXB0LTUuc2gKc2ggL3Zhci9saWIvbG9iZXIvY2xlYW51cC5zaApMT0JFUl9GSVJTVEJPT1RfRU9GCmNhdCA+ICcvdGFyZ2V0L2V0Yy9zeXN0ZW1kL3N5c3RlbS9sb2Jlci1maXJzdGJvb3Quc2VydmljZScgPDwnTE9CRVJfVU5JVF9FT0YnCltVbml0XQpEZXNjcmlwdGlvbj1Mb2JlciBmaXJzdCBib290CkFmdGVyPW5ldHdvcmstb25saW5lLnRhcmdldApXYW50cz1uZXR3b3JrLW9ubGluZS50YXJnZXQKQ29uZGl0aW9uUGF0aEV4aXN0cz0vdmFyL2xpYi9sb2Jlci9maXJzdGJvb3QucGVuZGluZwpbU2VydmljZV0KVHlwZT1vbmVzaG90CkV4ZWNTdGFydD0vYmluL3NoIC92YXIvbGliL2xvYmVyL2ZpcnN0Ym9vdC5zaApbSW5zdGFsbF0KV2FudGVkQnk9bXVsdGktdXNlci50YXJnZXQKTE9CRVJfVU5JVF9FT0YKbG4gLXNmIC4uL2xvYmVyLWZpcnN0Ym9vdC5zZXJ2aWNlICcvdGFyZ2V0L2V0Yy9zeXN0ZW1kL3N5c3RlbS9tdWx0aS11c2VyLnRhcmdldC53YW50cy9sb2Jlci1maXJzdGJvb3Quc2VydmljZScKdG91Y2ggJy90YXJnZXQvdmFyL2xpYi9sb2Jlci9maXJzdGJvb3QucGVuZGluZycK | base64 -d | sh
+0032     - echo c2V0IC1ldQp1bWFzayAwNzcKbWtkaXIgLXAgJy90YXJnZXQvdmFyL2xpYi9sb2JlcicKY2F0ID4gJy90YXJnZXQvdmFyL2xpYi9sb2Jlci9jbGVhbnVwLnNoJyA8PCdMT0JFUl9DTEVBTlVQX0VPRicKIyEvYmluL3NoCnNldCAtZXUKVE9LRU49JChjYXQgL3Zhci9saWIvbG9iZXIvZmluYWxpemUudG9rZW4pCmN1cmwgLWZzUyAtbSAxMCAtWCBQT1NUICdodHRwOi8vZ3cuaW50ZXJuYWwvdjEvc2lnbmFsL3Rhc2stNDIvJyIkVE9LRU4iJy9pbml0LWRvbmUnIC0tZGF0YS1iaW5hcnkgJycgPi9kZXYvbnVsbCAyPiYxIHx8IDoKVE9LRU49JChjYXQgL3Zhci9saWIvbG9iZXIvZmluYWxpemUudG9rZW4pCmN1cmwgLWZzUyAtbSAxMCAtWCBQT1NUICdodHRwOi8vZ3cuaW50ZXJuYWwvdjEvc2lnbmFsL3Rhc2stNDIvJyIkVE9LRU4iJy9yZWFkeScgLS1kYXRhLWJpbmFyeSAnJyA+L2Rldi9udWxsIDI+JjEgfHwgOgpybSAtZiAvdmFyL2xpYi9sb2Jlci9maW5hbGl6ZS50b2tlbiAvdmFyL2xpYi9sb2Jlci9maXJzdGJvb3QucGVuZGluZyAvdmFyL2xpYi9sb2Jlci9zY3JpcHQtKi5zaCAvdmFyL2xpYi9sb2Jlci9ob29rLSogL3Zhci9saWIvbG9iZXIvZmlyc3Rib290LnNoIC92YXIvbGliL2xvYmVyL2NsZWFudXAuc2gKcm0gLWYgL3Jvb3QvYW5hY29uZGEta3MuY2ZnIC9yb290L29yaWdpbmFsLWtzLmNmZyAvdmFyL2xvZy9pbnN0YWxsZXIvYXV0b2luc3RhbGwtdXNlci1kYXRhIC92YXIvbG9nL2luc3RhbGxlci9hdXRvaW5zdGFsbC11c2VyLWRhdGEudHh0IC92YXIvbG9nL2luc3RhbGxlci9jZGViY29uZi9xdWVzdGlvbnMuZGF0IC92YXIvYWRtL2F1dG9pbnN0YWxsL2NhY2hlL2luc3RhbGxlZFN5c3RlbS54bWwKTE9CRVJfQ0xFQU5VUF9FT0YK | base64 -d | sh
+0033   packages:
+0034     - curl
+0035   ssh:
+0036     install-server: true
+0037   shutdown: reboot
```

## autoinstall-full-uefi.golden

Compared with `autoinstall_full.golden`; firmware was not split in that old reference.

### Old 1–0; new 1–1

S2: handwritten YAML fixture uses cloud-config header, ordinary lober username rather than reserved root, explicit firmware GPT storage, match-based DHCP NIC, and curl package. Inline multiline early/late bodies become one-line commands; /target is installer mount, target script chroots. SSH key composition is hook-owned, not implicit renderer policy. shutdown/reboot remains native profile syntax.

S5: all remaining profile/blank/layout/value differences in this range are directly authored in the committed .tmpl/.fragment snapshots: hostname remains web-01; identity is task-42 (valid model ID) rather than old numeric 42; new tokens shorter but valid; full includes one real script per supported timing, minimal keeps the slot empty (still receives runtime wrapper for stage signals); SSH keys/legacy host metadata omitted from independent assembly fixtures because Task9 owns actual builtin defaults and SSH hook. No implicit old-model field assembly remains.

```diff
+0001 #cloud-config
```

### Old 2–391; new 3–39

S1: fixed global environment and inline signal/run_hook library are removed; flattened snapshot instances and FileScript now own per-instance EnvName/EnvValue, digest/interpreter/timeout/required execution. Task6 inserts one-line wrapper downloads, never copies old inline runtime. Legacy fake digest strings become real SHA-256 fixture digest. Task8 implements/verifies runtime bodies.

B1: firstboot now stages local slot wrappers and local digest payloads before reboot; systemd invokes the installed native runner. Boot credential no longer enters firstboot; independent local finalize token used. Cleanup follows success and erases cached answers/payloads. Legacy firstboot reused install TOKEN despite writing finalize.token. Unit/marker/local paths change to this explicit contract.

S2: handwritten YAML fixture uses cloud-config header, ordinary lober username rather than reserved root, explicit firmware GPT storage, match-based DHCP NIC, and curl package. Inline multiline early/late bodies become one-line commands; /target is installer mount, target script chroots. SSH key composition is hook-owned, not implicit renderer policy. shutdown/reboot remains native profile syntax.

S5: all remaining profile/blank/layout/value differences in this range are directly authored in the committed .tmpl/.fragment snapshots: hostname remains web-01; identity is task-42 (valid model ID) rather than old numeric 42; new tokens shorter but valid; full includes one real script per supported timing, minimal keeps the slot empty (still receives runtime wrapper for stage signals); SSH keys/legacy host metadata omitted from independent assembly fixtures because Task9 owns actual builtin defaults and SSH hook. No implicit old-model field assembly remains.

```diff
-0002     version: 1
-0003     locale: en_US.UTF-8
-0004     keyboard:
-0005         layout: us
-0006     identity:
-0007         hostname: web-01
-0008         username: root
-0009         password: '!'
-0010     storage:
-0011         layout:
-0012             name: direct
-0013     network:
-0014         version: 2
-0015         ethernets:
-0016             install-nic:
-0017                 dhcp4: true
-0018     early-commands:
-0019         - |-
-0020           LOBER_CONTRACT_VERSION=''
-0021           LOBER_TASK_ID='42'
-0022           LOBER_SN='SN-FIXED-0001'
-0023           LOBER_MAC=''
-0024           LOBER_UUID=''
-0025           LOBER_HOSTNAME='web-01'
-0026           LOBER_OS_FAMILY='ubuntu'
-0027           LOBER_OS_VERSION=''
-0028           LOBER_ARCH=''
-0029           LOBER_DISKS=''
-0030           LOBER_DISK_PRIMARY='/dev/disk/by-id/wwn-0x5000c500fixed0001'
-0031           LOBER_NICS=''
-0032           LOBER_RAM_MB=''
-0033           LOBER_CPU_CORES=''
-0034           LOBER_VENDOR=''
-0035           LOBER_PRODUCT=''
-0036           LOBER_BIZ_IP='10.20.0.15'
-0037           LOBER_BIZ_GW=''
-0038           LOBER_BIZ_MASK=''
-0039           LOBER_ARTIFACT_BASE='http://ng.internal/artifacts'
-0040           LOBER_GATEWAY_BASE='http://gw.internal'
-0041           LOBER_RACK=''
-0042           LOBER_ROW=''
-0043           LOBER_DC=''
-0044           GW='http://gw.internal'
-0045           ART='http://ng.internal/artifacts'
-0046           TASK='42'
-0047           TOKEN='install-tok-fixed-0000000000000000'
-0048           SEQ=0
-0049 
-0050           signal() {
-0051             local phase="$1" rc="${2:-0}" log="${3:-/dev/null}" digest="${4:-}"
-0052             SEQ=$((SEQ+1))
-0053             local q="$GW/v1/signal/$TASK/$TOKEN/$phase?rc=$rc&seq=$SEQ"
-0054             if [ -n "$digest" ]; then
-0055               q="$q&digest=$digest"
-0056             fi
-0057             for i in 1 2 3; do
-0058               curl -fsS -m 10 -X POST "$q" \
-0059                 -H 'Content-Type: text/plain' --data-binary "@$log" && return 0
-0060               sleep $((i*3))
-0061             done
-0062             logger -t lober "signal failed: $phase rc=$rc"
-0063             return 0
-0064           }
-0065 
-0066           run_hook() {
-0067             local digest="$1" required="$2" timeout_sec="$3"
-0068             curl -fsS -o /tmp/h.sh "$ART/hooks/$digest/script.sh" || return 1
-0069             echo "$digest  /tmp/h.sh" | sha256sum -c - || return 1
-0070             ( set -euo pipefail; timeout "$timeout_sec" bash /tmp/h.sh ) 2>&1 | tee /tmp/h.log
-0071             local rc=${PIPESTATUS[0]}
-0072             signal "hook-report" "$rc" /tmp/h.log "$digest"
-0073             [ "$rc" -eq 0 ] || [ "$required" != "true" ]
-0074           }
-0075           signal "pre-start" 0
-0076         - |-
-0077           GW='http://gw.internal'
-0078           ART='http://ng.internal/artifacts'
-0079           TASK='42'
-0080           TOKEN='install-tok-fixed-0000000000000000'
-0081           SEQ=0
-0082 
-0083           signal() {
-0084             local phase="$1" rc="${2:-0}" log="${3:-/dev/null}" digest="${4:-}"
-0085             SEQ=$((SEQ+1))
-0086             local q="$GW/v1/signal/$TASK/$TOKEN/$phase?rc=$rc&seq=$SEQ"
-0087             if [ -n "$digest" ]; then
-0088               q="$q&digest=$digest"
-0089             fi
-0090             for i in 1 2 3; do
-0091               curl -fsS -m 10 -X POST "$q" \
-0092                 -H 'Content-Type: text/plain' --data-binary "@$log" && return 0
-0093               sleep $((i*3))
-0094             done
-0095             logger -t lober "signal failed: $phase rc=$rc"
-0096             return 0
-0097           }
-0098 
-0099           run_hook() {
-0100             local digest="$1" required="$2" timeout_sec="$3"
-0101             curl -fsS -o /tmp/h.sh "$ART/hooks/$digest/script.sh" || return 1
-0102             echo "$digest  /tmp/h.sh" | sha256sum -c - || return 1
-0103             ( set -euo pipefail; timeout "$timeout_sec" bash /tmp/h.sh ) 2>&1 | tee /tmp/h.log
-0104             local rc=${PIPESTATUS[0]}
-0105             signal "hook-report" "$rc" /tmp/h.log "$digest"
-0106             [ "$rc" -eq 0 ] || [ "$required" != "true" ]
-0107           }
-0108           run_hook 'aaaa1111pre' 'true' '300' || exit 1
-0109         - |-
-0110           GW='http://gw.internal'
-0111           ART='http://ng.internal/artifacts'
-0112           TASK='42'
-0113           TOKEN='install-tok-fixed-0000000000000000'
-0114           SEQ=0
-0115 
-0116           signal() {
-0117             local phase="$1" rc="${2:-0}" log="${3:-/dev/null}" digest="${4:-}"
-0118             SEQ=$((SEQ+1))
-0119             local q="$GW/v1/signal/$TASK/$TOKEN/$phase?rc=$rc&seq=$SEQ"
-0120             if [ -n "$digest" ]; then
-0121               q="$q&digest=$digest"
-0122             fi
-0123             for i in 1 2 3; do
-0124               curl -fsS -m 10 -X POST "$q" \
-0125                 -H 'Content-Type: text/plain' --data-binary "@$log" && return 0
-0126               sleep $((i*3))
-0127             done
-0128             logger -t lober "signal failed: $phase rc=$rc"
-0129             return 0
-0130           }
-0131 
-0132           run_hook() {
-0133             local digest="$1" required="$2" timeout_sec="$3"
-0134             curl -fsS -o /tmp/h.sh "$ART/hooks/$digest/script.sh" || return 1
-0135             echo "$digest  /tmp/h.sh" | sha256sum -c - || return 1
-0136             ( set -euo pipefail; timeout "$timeout_sec" bash /tmp/h.sh ) 2>&1 | tee /tmp/h.log
-0137             local rc=${PIPESTATUS[0]}
-0138             signal "hook-report" "$rc" /tmp/h.log "$digest"
-0139             [ "$rc" -eq 0 ] || [ "$required" != "true" ]
-0140           }
-0141           signal "pre-done" 0
-0142     late-commands:
-0143         - |-
-0144           GW='http://gw.internal'
-0145           ART='http://ng.internal/artifacts'
-0146           TASK='42'
-0147           TOKEN='install-tok-fixed-0000000000000000'
-0148           SEQ=0
-0149 
-0150           signal() {
-0151             local phase="$1" rc="${2:-0}" log="${3:-/dev/null}" digest="${4:-}"
-0152             SEQ=$((SEQ+1))
-0153             local q="$GW/v1/signal/$TASK/$TOKEN/$phase?rc=$rc&seq=$SEQ"
-0154             if [ -n "$digest" ]; then
-0155               q="$q&digest=$digest"
-0156             fi
-0157             for i in 1 2 3; do
-0158               curl -fsS -m 10 -X POST "$q" \
-0159                 -H 'Content-Type: text/plain' --data-binary "@$log" && return 0
-0160               sleep $((i*3))
-0161             done
-0162             logger -t lober "signal failed: $phase rc=$rc"
-0163             return 0
-0164           }
-0165 
-0166           run_hook() {
-0167             local digest="$1" required="$2" timeout_sec="$3"
-0168             curl -fsS -o /tmp/h.sh "$ART/hooks/$digest/script.sh" || return 1
-0169             echo "$digest  /tmp/h.sh" | sha256sum -c - || return 1
-0170             ( set -euo pipefail; timeout "$timeout_sec" bash /tmp/h.sh ) 2>&1 | tee /tmp/h.log
-0171             local rc=${PIPESTATUS[0]}
-0172             signal "hook-report" "$rc" /tmp/h.log "$digest"
-0173             [ "$rc" -eq 0 ] || [ "$required" != "true" ]
-0174           }
-0175           signal "post-start" 0
-0176           signal "packages-start" 0
-0177           signal "packages-done" 0
-0178         - |-
-0179           GW='http://gw.internal'
-0180           ART='http://ng.internal/artifacts'
-0181           TASK='42'
-0182           TOKEN='install-tok-fixed-0000000000000000'
-0183           SEQ=0
-0184 
-0185           signal() {
-0186             local phase="$1" rc="${2:-0}" log="${3:-/dev/null}" digest="${4:-}"
-0187             SEQ=$((SEQ+1))
-0188             local q="$GW/v1/signal/$TASK/$TOKEN/$phase?rc=$rc&seq=$SEQ"
-0189             if [ -n "$digest" ]; then
-0190               q="$q&digest=$digest"
-0191             fi
-0192             for i in 1 2 3; do
-0193               curl -fsS -m 10 -X POST "$q" \
-0194                 -H 'Content-Type: text/plain' --data-binary "@$log" && return 0
-0195               sleep $((i*3))
-0196             done
-0197             logger -t lober "signal failed: $phase rc=$rc"
-0198             return 0
-0199           }
-0200 
-0201           run_hook() {
-0202             local digest="$1" required="$2" timeout_sec="$3"
-0203             curl -fsS -o /tmp/h.sh "$ART/hooks/$digest/script.sh" || return 1
-0204             echo "$digest  /tmp/h.sh" | sha256sum -c - || return 1
-0205             ( set -euo pipefail; timeout "$timeout_sec" bash /tmp/h.sh ) 2>&1 | tee /tmp/h.log
-0206             local rc=${PIPESTATUS[0]}
-0207             signal "hook-report" "$rc" /tmp/h.log "$digest"
-0208             [ "$rc" -eq 0 ] || [ "$required" != "true" ]
-0209           }
-0210           run_hook 'bbbb2222nochroot' 'false' '300' || exit 1
-0211         - |-
-0212           curtin in-target -- bash -c 'GW='\''http://gw.internal'\''
-0213           ART='\''http://ng.internal/artifacts'\''
-0214           TASK='\''42'\''
-0215           TOKEN='\''install-tok-fixed-0000000000000000'\''
-0216           SEQ=0
-0217 
-0218           signal() {
-0219             local phase="$1" rc="${2:-0}" log="${3:-/dev/null}" digest="${4:-}"
-0220             SEQ=$((SEQ+1))
-0221             local q="$GW/v1/signal/$TASK/$TOKEN/$phase?rc=$rc&seq=$SEQ"
-0222             if [ -n "$digest" ]; then
-0223               q="$q&digest=$digest"
-0224             fi
-0225             for i in 1 2 3; do
-0226               curl -fsS -m 10 -X POST "$q" \
-0227                 -H '\''Content-Type: text/plain'\'' --data-binary "@$log" && return 0
-0228               sleep $((i*3))
-0229             done
-0230             logger -t lober "signal failed: $phase rc=$rc"
-0231             return 0
-0232           }
-0233 
-0234           run_hook() {
-0235             local digest="$1" required="$2" timeout_sec="$3"
-0236             curl -fsS -o /tmp/h.sh "$ART/hooks/$digest/script.sh" || return 1
-0237             echo "$digest  /tmp/h.sh" | sha256sum -c - || return 1
-0238             ( set -euo pipefail; timeout "$timeout_sec" bash /tmp/h.sh ) 2>&1 | tee /tmp/h.log
-0239             local rc=${PIPESTATUS[0]}
-0240             signal "hook-report" "$rc" /tmp/h.log "$digest"
-0241             [ "$rc" -eq 0 ] || [ "$required" != "true" ]
-0242           }
-0243           run_hook '\''cccc3333post'\'' '\''true'\'' '\''300'\'' || exit 1'
-0244         - |-
-0245           curtin in-target -- bash -c 'mkdir -p /var/lib/lober /var/lib/lober/hooks
-0246 
-0247           install -m 600 /dev/null /var/lib/lober/finalize.token
-0248           echo '\''finalize-tok-fixed-0000000000000000'\'' > /var/lib/lober/finalize.token
-0249 
-0250           install -m 600 /dev/null /var/lib/lober/params.env
-0251           cat > /var/lib/lober/params.env <<'\''LOBER_PARAMS_EOF'\''
-0252           LOBER_CONTRACT_VERSION='\'''\''
-0253           LOBER_TASK_ID='\''42'\''
-0254           LOBER_SN='\''SN-FIXED-0001'\''
-0255           LOBER_MAC='\'''\''
-0256           LOBER_UUID='\'''\''
-0257           LOBER_HOSTNAME='\''web-01'\''
-0258           LOBER_OS_FAMILY='\''ubuntu'\''
-0259           LOBER_OS_VERSION='\'''\''
-0260           LOBER_ARCH='\'''\''
-0261           LOBER_DISKS='\'''\''
-0262           LOBER_DISK_PRIMARY='\''/dev/disk/by-id/wwn-0x5000c500fixed0001'\''
-0263           LOBER_NICS='\'''\''
-0264           LOBER_RAM_MB='\'''\''
-0265           LOBER_CPU_CORES='\'''\''
-0266           LOBER_VENDOR='\'''\''
-0267           LOBER_PRODUCT='\'''\''
-0268           LOBER_BIZ_IP='\''10.20.0.15'\''
-0269           LOBER_BIZ_GW='\'''\''
-0270           LOBER_BIZ_MASK='\'''\''
-0271           LOBER_ARTIFACT_BASE='\''http://ng.internal/artifacts'\''
-0272           LOBER_GATEWAY_BASE='\''http://gw.internal'\''
-0273           LOBER_RACK='\'''\''
-0274           LOBER_ROW='\'''\''
-0275           LOBER_DC='\'''\''
-0276           LOBER_PARAMS_EOF
-0277 
-0278           cat > /var/lib/lober/hooks/manifest <<'\''LOBER_MANIFEST_EOF'\''
-0279           010-dddd4444firstboot.sh digest=dddd4444firstboot required=true order=10
-0280           LOBER_MANIFEST_EOF
-0281 
-0282           curl -fsS -o /var/lib/lober/hooks/010-dddd4444firstboot.sh '\''http://ng.internal/artifacts/hooks/dddd4444firstboot/script.sh'\''
-0283           echo '\''dddd4444firstboot  /var/lib/lober/hooks/010-dddd4444firstboot.sh'\'' | sha256sum -c -
-0284           chmod 700 /var/lib/lober/hooks/010-dddd4444firstboot.sh
-0285 
-0286           cat > /usr/local/sbin/lober-init.sh <<'\''LOBER_INIT_EOF'\''
-0287           #!/bin/bash
-0288           . /var/lib/lober/params.env
-0289 
-0290           GW='\''http://gw.internal'\''
-0291           ART='\''http://ng.internal/artifacts'\''
-0292           TASK='\''42'\''
-0293           TOKEN='\''install-tok-fixed-0000000000000000'\''
-0294           SEQ=0
-0295 
-0296           signal() {
-0297             local phase="$1" rc="${2:-0}" log="${3:-/dev/null}" digest="${4:-}"
-0298             SEQ=$((SEQ+1))
-0299             local q="$GW/v1/signal/$TASK/$TOKEN/$phase?rc=$rc&seq=$SEQ"
-0300             if [ -n "$digest" ]; then
-0301               q="$q&digest=$digest"
-0302             fi
-0303             for i in 1 2 3; do
-0304               curl -fsS -m 10 -X POST "$q" \
-0305                 -H '\''Content-Type: text/plain'\'' --data-binary "@$log" && return 0
-0306               sleep $((i*3))
-0307             done
-0308             logger -t lober "signal failed: $phase rc=$rc"
-0309             return 0
-0310           }
-0311 
-0312           run_hook() {
-0313             local digest="$1" required="$2" timeout_sec="$3"
-0314             curl -fsS -o /tmp/h.sh "$ART/hooks/$digest/script.sh" || return 1
-0315             echo "$digest  /tmp/h.sh" | sha256sum -c - || return 1
-0316             ( set -euo pipefail; timeout "$timeout_sec" bash /tmp/h.sh ) 2>&1 | tee /tmp/h.log
-0317             local rc=${PIPESTATUS[0]}
-0318             signal "hook-report" "$rc" /tmp/h.log "$digest"
-0319             [ "$rc" -eq 0 ] || [ "$required" != "true" ]
-0320           }
-0321 
-0322           signal "booted" 0
-0323           signal "init-start" 0
-0324 
-0325           bash /var/lib/lober/hooks/010-dddd4444firstboot.sh > /tmp/fb-dddd4444firstboot.log 2>&1
-0326           signal "hook-report" "$?" /tmp/fb-dddd4444firstboot.log "dddd4444firstboot"
-0327 
-0328           signal "init-done" 0
-0329           signal "ready" 0
-0330 
-0331           shred -u /var/lib/lober/params.env /var/lib/lober/finalize.token
-0332           rm -f /var/lib/lober/init.pending
-0333           sed -i '\''/rootpw\|--iscrypted\|inst.ks=/d'\'' /root/anaconda-ks.cfg 2>/dev/null
-0334           LOBER_INIT_EOF
-0335           chmod 755 /usr/local/sbin/lober-init.sh
-0336 
-0337           cat > /etc/systemd/system/lober-init.service <<'\''LOBER_UNIT_EOF'\''
-0338           [Unit]
-0339           Description=Lober OS init
-0340           After=network-online.target
-0341           Wants=network-online.target
-0342           ConditionPathExists=/var/lib/lober/init.pending
-0343           [Service]
-0344           Type=oneshot
-0345           ExecStart=/usr/local/sbin/lober-init.sh
-0346           StandardOutput=journal+console
-0347           [Install]
-0348           WantedBy=multi-user.target
-0349           LOBER_UNIT_EOF
-0350 
-0351           systemctl enable lober-init.service
-0352           touch /var/lib/lober/init.pending'
-0353         - |-
-0354           GW='http://gw.internal'
-0355           ART='http://ng.internal/artifacts'
-0356           TASK='42'
-0357           TOKEN='install-tok-fixed-0000000000000000'
-0358           SEQ=0
-0359 
-0360           signal() {
-0361             local phase="$1" rc="${2:-0}" log="${3:-/dev/null}" digest="${4:-}"
-0362             SEQ=$((SEQ+1))
-0363             local q="$GW/v1/signal/$TASK/$TOKEN/$phase?rc=$rc&seq=$SEQ"
-0364             if [ -n "$digest" ]; then
-0365               q="$q&digest=$digest"
-0366             fi
-0367             for i in 1 2 3; do
-0368               curl -fsS -m 10 -X POST "$q" \
-0369                 -H 'Content-Type: text/plain' --data-binary "@$log" && return 0
-0370               sleep $((i*3))
-0371             done
-0372             logger -t lober "signal failed: $phase rc=$rc"
-0373             return 0
-0374           }
-0375 
-0376           run_hook() {
-0377             local digest="$1" required="$2" timeout_sec="$3"
-0378             curl -fsS -o /tmp/h.sh "$ART/hooks/$digest/script.sh" || return 1
-0379             echo "$digest  /tmp/h.sh" | sha256sum -c - || return 1
-0380             ( set -euo pipefail; timeout "$timeout_sec" bash /tmp/h.sh ) 2>&1 | tee /tmp/h.log
-0381             local rc=${PIPESTATUS[0]}
-0382             signal "hook-report" "$rc" /tmp/h.log "$digest"
-0383             [ "$rc" -eq 0 ] || [ "$required" != "true" ]
-0384           }
-0385           signal "post-done" 0
-0386           signal "rebooting" 0
-0387     packages: []
-0388     ssh:
-0389         install-server: true
-0390         authorized-keys:
-0391             - ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIGZ5ZDhhOWMwZTNiNGY1YzZkN2E4YjljMGUzYjRmNWM2 ops@lober
+0003   version: 1
+0004   locale: en_US.UTF-8
+0005   keyboard:
+0006     layout: us
+0007   identity:
+0008     hostname: web-01
+0009     username: lober
+0010     password: '!'
+0011   storage:
+0012     config:
+0013       - {id: disk0, type: disk, path: /dev/sda, ptable: gpt, wipe: superblock-recursive}
+0014       - {id: esp, type: partition, device: disk0, size: 600M, flag: boot, grub_device: true}
+0015       - {id: esp-fs, type: format, volume: esp, fstype: fat32}
+0016       - {id: esp-mount, type: mount, device: esp-fs, path: /boot/efi}
+0017       - {id: root, type: partition, device: disk0, size: -1}
+0018       - {id: root-fs, type: format, volume: root, fstype: ext4}
+0019       - {id: root-mount, type: mount, device: root-fs, path: /}
+0020   network:
+0021     version: 2
+0022     ethernets:
+0023       install-nic:
+0024         match:
+0025           name: en*
+0026         dhcp4: true
+0027   early-commands:
+0028     - echo c2V0IC1ldQp1bWFzayAwNzcKbWtkaXIgLXAgJy90bXAnCmN1cmwgLWZzUyAtLXJldHJ5IDMgLW8gJy90bXAvc2NyaXB0LTIuc2gnICdodHRwOi8vZ3cuaW50ZXJuYWwvdjEvYW5zd2VyL3Rhc2stNDIvaW5zdGFsbC10b2stZml4ZWQvc2NyaXB0cy8yLnNoJwpzaCAnL3RtcC9zY3JpcHQtMi5zaCcK | base64 -d | sh
+0029   late-commands:
+0030     - echo c2V0IC1ldQp1bWFzayAwNzcKbWtkaXIgLXAgJy90bXAnCmN1cmwgLWZzUyAtLXJldHJ5IDMgLW8gJy90bXAvc2NyaXB0LTMuc2gnICdodHRwOi8vZ3cuaW50ZXJuYWwvdjEvYW5zd2VyL3Rhc2stNDIvaW5zdGFsbC10b2stZml4ZWQvc2NyaXB0cy8zLnNoJwpzaCAnL3RtcC9zY3JpcHQtMy5zaCcK | base64 -d | sh
+0031     - echo c2V0IC1ldQp1bWFzayAwNzcKbWtkaXIgLXAgJy90YXJnZXQvdmFyL2xpYi9sb2JlcicKY3VybCAtZnNTIC0tcmV0cnkgMyAtbyAnL3RhcmdldC92YXIvbGliL2xvYmVyL3NjcmlwdC00LnNoJyAnaHR0cDovL2d3LmludGVybmFsL3YxL2Fuc3dlci90YXNrLTQyL2luc3RhbGwtdG9rLWZpeGVkL3NjcmlwdHMvNC5zaCcKY2hyb290ICcvdGFyZ2V0JyBzaCAnL3Zhci9saWIvbG9iZXIvc2NyaXB0LTQuc2gnCg== | base64 -d | sh
+0032     - echo c2V0IC1ldQp1bWFzayAwNzcKbWtkaXIgLXAgJy90YXJnZXQvdmFyL2xpYi9sb2JlcicKY3VybCAtZnNTIC0tcmV0cnkgMyAtbyAnL3RhcmdldC92YXIvbGliL2xvYmVyL3NjcmlwdC01LnNoJyAnaHR0cDovL2d3LmludGVybmFsL3YxL2Fuc3dlci90YXNrLTQyL2luc3RhbGwtdG9rLWZpeGVkL3NjcmlwdHMvNS5zaCcKY3VybCAtZnNTIC0tcmV0cnkgMyAtbyAnL3RhcmdldC92YXIvbGliL2xvYmVyL2hvb2stM2VlMDQ5ZTU5MWY1ZTk5YWU4ZjQ5OGFlMzgxZjQ1OTM2NWMyMjFlNDFlNmI0MmYyYWQ2ZTlhNmE5NzUwNTMyZS5zaCcgJ2h0dHA6Ly9ndy5pbnRlcm5hbC92MS9hbnN3ZXIvdGFzay00Mi9pbnN0YWxsLXRvay1maXhlZC9ob29rcy8zZWUwNDllNTkxZjVlOTlhZThmNDk4YWUzODFmNDU5MzY1YzIyMWU0MWU2YjQyZjJhZDZlOWE2YTk3NTA1MzJlJwo= | base64 -d | sh
+0033     - echo c2V0IC1ldQp1bWFzayAwNzcKbWtkaXIgLXAgJy90YXJnZXQvdmFyL2xpYi9sb2JlcicgJy90YXJnZXQvZXRjL3N5c3RlbWQvc3lzdGVtL211bHRpLXVzZXIudGFyZ2V0LndhbnRzJwpwcmludGYgJyVzJyAnZmluYWxpemUtdG9rLWZpeGVkJyA+ICcvdGFyZ2V0L3Zhci9saWIvbG9iZXIvZmluYWxpemUudG9rZW4nCmNhdCA+ICcvdGFyZ2V0L3Zhci9saWIvbG9iZXIvZmlyc3Rib290LnNoJyA8PCdMT0JFUl9GSVJTVEJPT1RfRU9GJwojIS9iaW4vc2gKc2V0IC1ldQpUT0tFTj0kKGNhdCAvdmFyL2xpYi9sb2Jlci9maW5hbGl6ZS50b2tlbikKY3VybCAtZnNTIC1tIDEwIC1YIFBPU1QgJ2h0dHA6Ly9ndy5pbnRlcm5hbC92MS9zaWduYWwvdGFzay00Mi8nIiRUT0tFTiInL2Jvb3RlZCcgLS1kYXRhLWJpbmFyeSAnJyA+L2Rldi9udWxsIDI+JjEgfHwgOgpUT0tFTj0kKGNhdCAvdmFyL2xpYi9sb2Jlci9maW5hbGl6ZS50b2tlbikKY3VybCAtZnNTIC1tIDEwIC1YIFBPU1QgJ2h0dHA6Ly9ndy5pbnRlcm5hbC92MS9zaWduYWwvdGFzay00Mi8nIiRUT0tFTiInL2luaXQtc3RhcnQnIC0tZGF0YS1iaW5hcnkgJycgPi9kZXYvbnVsbCAyPiYxIHx8IDoKc2ggL3Zhci9saWIvbG9iZXIvc2NyaXB0LTUuc2gKc2ggL3Zhci9saWIvbG9iZXIvY2xlYW51cC5zaApMT0JFUl9GSVJTVEJPT1RfRU9GCmNhdCA+ICcvdGFyZ2V0L2V0Yy9zeXN0ZW1kL3N5c3RlbS9sb2Jlci1maXJzdGJvb3Quc2VydmljZScgPDwnTE9CRVJfVU5JVF9FT0YnCltVbml0XQpEZXNjcmlwdGlvbj1Mb2JlciBmaXJzdCBib290CkFmdGVyPW5ldHdvcmstb25saW5lLnRhcmdldApXYW50cz1uZXR3b3JrLW9ubGluZS50YXJnZXQKQ29uZGl0aW9uUGF0aEV4aXN0cz0vdmFyL2xpYi9sb2Jlci9maXJzdGJvb3QucGVuZGluZwpbU2VydmljZV0KVHlwZT1vbmVzaG90CkV4ZWNTdGFydD0vYmluL3NoIC92YXIvbGliL2xvYmVyL2ZpcnN0Ym9vdC5zaApbSW5zdGFsbF0KV2FudGVkQnk9bXVsdGktdXNlci50YXJnZXQKTE9CRVJfVU5JVF9FT0YKbG4gLXNmIC4uL2xvYmVyLWZpcnN0Ym9vdC5zZXJ2aWNlICcvdGFyZ2V0L2V0Yy9zeXN0ZW1kL3N5c3RlbS9tdWx0aS11c2VyLnRhcmdldC53YW50cy9sb2Jlci1maXJzdGJvb3Quc2VydmljZScKdG91Y2ggJy90YXJnZXQvdmFyL2xpYi9sb2Jlci9maXJzdGJvb3QucGVuZGluZycK | base64 -d | sh
+0034     - echo c2V0IC1ldQp1bWFzayAwNzcKbWtkaXIgLXAgJy90YXJnZXQvdmFyL2xpYi9sb2JlcicKY2F0ID4gJy90YXJnZXQvdmFyL2xpYi9sb2Jlci9jbGVhbnVwLnNoJyA8PCdMT0JFUl9DTEVBTlVQX0VPRicKIyEvYmluL3NoCnNldCAtZXUKVE9LRU49JChjYXQgL3Zhci9saWIvbG9iZXIvZmluYWxpemUudG9rZW4pCmN1cmwgLWZzUyAtbSAxMCAtWCBQT1NUICdodHRwOi8vZ3cuaW50ZXJuYWwvdjEvc2lnbmFsL3Rhc2stNDIvJyIkVE9LRU4iJy9pbml0LWRvbmUnIC0tZGF0YS1iaW5hcnkgJycgPi9kZXYvbnVsbCAyPiYxIHx8IDoKVE9LRU49JChjYXQgL3Zhci9saWIvbG9iZXIvZmluYWxpemUudG9rZW4pCmN1cmwgLWZzUyAtbSAxMCAtWCBQT1NUICdodHRwOi8vZ3cuaW50ZXJuYWwvdjEvc2lnbmFsL3Rhc2stNDIvJyIkVE9LRU4iJy9yZWFkeScgLS1kYXRhLWJpbmFyeSAnJyA+L2Rldi9udWxsIDI+JjEgfHwgOgpybSAtZiAvdmFyL2xpYi9sb2Jlci9maW5hbGl6ZS50b2tlbiAvdmFyL2xpYi9sb2Jlci9maXJzdGJvb3QucGVuZGluZyAvdmFyL2xpYi9sb2Jlci9zY3JpcHQtKi5zaCAvdmFyL2xpYi9sb2Jlci9ob29rLSogL3Zhci9saWIvbG9iZXIvZmlyc3Rib290LnNoIC92YXIvbGliL2xvYmVyL2NsZWFudXAuc2gKcm0gLWYgL3Jvb3QvYW5hY29uZGEta3MuY2ZnIC9yb290L29yaWdpbmFsLWtzLmNmZyAvdmFyL2xvZy9pbnN0YWxsZXIvYXV0b2luc3RhbGwtdXNlci1kYXRhIC92YXIvbG9nL2luc3RhbGxlci9hdXRvaW5zdGFsbC11c2VyLWRhdGEudHh0IC92YXIvbG9nL2luc3RhbGxlci9jZGViY29uZi9xdWVzdGlvbnMuZGF0IC92YXIvYWRtL2F1dG9pbnN0YWxsL2NhY2hlL2luc3RhbGxlZFN5c3RlbS54bWwKTE9CRVJfQ0xFQU5VUF9FT0YK | base64 -d | sh
+0035   packages:
+0036     - curl
+0037   ssh:
+0038     install-server: true
+0039   shutdown: reboot
```

## autoinstall-minimal-bios.golden

Compared with `autoinstall_minimal.golden`; firmware was not split in that old reference.

### Old 1–0; new 1–1

S2: handwritten YAML fixture uses cloud-config header, ordinary lober username rather than reserved root, explicit firmware GPT storage, match-based DHCP NIC, and curl package. Inline multiline early/late bodies become one-line commands; /target is installer mount, target script chroots. SSH key composition is hook-owned, not implicit renderer policy. shutdown/reboot remains native profile syntax.

S5: all remaining profile/blank/layout/value differences in this range are directly authored in the committed .tmpl/.fragment snapshots: hostname remains web-01; identity is task-42 (valid model ID) rather than old numeric 42; new tokens shorter but valid; full includes one real script per supported timing, minimal keeps the slot empty (still receives runtime wrapper for stage signals); SSH keys/legacy host metadata omitted from independent assembly fixtures because Task9 owns actual builtin defaults and SSH hook. No implicit old-model field assembly remains.

```diff
+0001 #cloud-config
```

### Old 2–284; new 3–37

S1: fixed global environment and inline signal/run_hook library are removed; flattened snapshot instances and FileScript now own per-instance EnvName/EnvValue, digest/interpreter/timeout/required execution. Task6 inserts one-line wrapper downloads, never copies old inline runtime. Legacy fake digest strings become real SHA-256 fixture digest. Task8 implements/verifies runtime bodies.

B1: firstboot now stages local slot wrappers and local digest payloads before reboot; systemd invokes the installed native runner. Boot credential no longer enters firstboot; independent local finalize token used. Cleanup follows success and erases cached answers/payloads. Legacy firstboot reused install TOKEN despite writing finalize.token. Unit/marker/local paths change to this explicit contract.

S2: handwritten YAML fixture uses cloud-config header, ordinary lober username rather than reserved root, explicit firmware GPT storage, match-based DHCP NIC, and curl package. Inline multiline early/late bodies become one-line commands; /target is installer mount, target script chroots. SSH key composition is hook-owned, not implicit renderer policy. shutdown/reboot remains native profile syntax.

S5: all remaining profile/blank/layout/value differences in this range are directly authored in the committed .tmpl/.fragment snapshots: hostname remains web-01; identity is task-42 (valid model ID) rather than old numeric 42; new tokens shorter but valid; full includes one real script per supported timing, minimal keeps the slot empty (still receives runtime wrapper for stage signals); SSH keys/legacy host metadata omitted from independent assembly fixtures because Task9 owns actual builtin defaults and SSH hook. No implicit old-model field assembly remains.

```diff
-0002     version: 1
-0003     locale: en_US.UTF-8
-0004     keyboard:
-0005         layout: us
-0006     identity:
-0007         hostname: web-01
-0008         username: root
-0009         password: '!'
-0010     storage:
-0011         layout:
-0012             name: direct
-0013     network:
-0014         version: 2
-0015         ethernets:
-0016             install-nic:
-0017                 dhcp4: true
-0018     early-commands:
-0019         - |-
-0020           LOBER_CONTRACT_VERSION=''
-0021           LOBER_TASK_ID='42'
-0022           LOBER_SN='SN-FIXED-0001'
-0023           LOBER_MAC=''
-0024           LOBER_UUID=''
-0025           LOBER_HOSTNAME='web-01'
-0026           LOBER_OS_FAMILY='ubuntu'
-0027           LOBER_OS_VERSION=''
-0028           LOBER_ARCH=''
-0029           LOBER_DISKS=''
-0030           LOBER_DISK_PRIMARY='/dev/disk/by-id/wwn-0x5000c500fixed0001'
-0031           LOBER_NICS=''
-0032           LOBER_RAM_MB=''
-0033           LOBER_CPU_CORES=''
-0034           LOBER_VENDOR=''
-0035           LOBER_PRODUCT=''
-0036           LOBER_BIZ_IP='10.20.0.15'
-0037           LOBER_BIZ_GW=''
-0038           LOBER_BIZ_MASK=''
-0039           LOBER_ARTIFACT_BASE='http://ng.internal/artifacts'
-0040           LOBER_GATEWAY_BASE='http://gw.internal'
-0041           LOBER_RACK=''
-0042           LOBER_ROW=''
-0043           LOBER_DC=''
-0044           GW='http://gw.internal'
-0045           ART='http://ng.internal/artifacts'
-0046           TASK='42'
-0047           TOKEN='install-tok-fixed-0000000000000000'
-0048           SEQ=0
-0049 
-0050           signal() {
-0051             local phase="$1" rc="${2:-0}" log="${3:-/dev/null}" digest="${4:-}"
-0052             SEQ=$((SEQ+1))
-0053             local q="$GW/v1/signal/$TASK/$TOKEN/$phase?rc=$rc&seq=$SEQ"
-0054             if [ -n "$digest" ]; then
-0055               q="$q&digest=$digest"
-0056             fi
-0057             for i in 1 2 3; do
-0058               curl -fsS -m 10 -X POST "$q" \
-0059                 -H 'Content-Type: text/plain' --data-binary "@$log" && return 0
-0060               sleep $((i*3))
-0061             done
-0062             logger -t lober "signal failed: $phase rc=$rc"
-0063             return 0
-0064           }
-0065 
-0066           run_hook() {
-0067             local digest="$1" required="$2" timeout_sec="$3"
-0068             curl -fsS -o /tmp/h.sh "$ART/hooks/$digest/script.sh" || return 1
-0069             echo "$digest  /tmp/h.sh" | sha256sum -c - || return 1
-0070             ( set -euo pipefail; timeout "$timeout_sec" bash /tmp/h.sh ) 2>&1 | tee /tmp/h.log
-0071             local rc=${PIPESTATUS[0]}
-0072             signal "hook-report" "$rc" /tmp/h.log "$digest"
-0073             [ "$rc" -eq 0 ] || [ "$required" != "true" ]
-0074           }
-0075           signal "pre-start" 0
-0076         - |-
-0077           GW='http://gw.internal'
-0078           ART='http://ng.internal/artifacts'
-0079           TASK='42'
-0080           TOKEN='install-tok-fixed-0000000000000000'
-0081           SEQ=0
-0082 
-0083           signal() {
-0084             local phase="$1" rc="${2:-0}" log="${3:-/dev/null}" digest="${4:-}"
-0085             SEQ=$((SEQ+1))
-0086             local q="$GW/v1/signal/$TASK/$TOKEN/$phase?rc=$rc&seq=$SEQ"
-0087             if [ -n "$digest" ]; then
-0088               q="$q&digest=$digest"
-0089             fi
-0090             for i in 1 2 3; do
-0091               curl -fsS -m 10 -X POST "$q" \
-0092                 -H 'Content-Type: text/plain' --data-binary "@$log" && return 0
-0093               sleep $((i*3))
-0094             done
-0095             logger -t lober "signal failed: $phase rc=$rc"
-0096             return 0
-0097           }
-0098 
-0099           run_hook() {
-0100             local digest="$1" required="$2" timeout_sec="$3"
-0101             curl -fsS -o /tmp/h.sh "$ART/hooks/$digest/script.sh" || return 1
-0102             echo "$digest  /tmp/h.sh" | sha256sum -c - || return 1
-0103             ( set -euo pipefail; timeout "$timeout_sec" bash /tmp/h.sh ) 2>&1 | tee /tmp/h.log
-0104             local rc=${PIPESTATUS[0]}
-0105             signal "hook-report" "$rc" /tmp/h.log "$digest"
-0106             [ "$rc" -eq 0 ] || [ "$required" != "true" ]
-0107           }
-0108           signal "pre-done" 0
-0109     late-commands:
-0110         - |-
-0111           GW='http://gw.internal'
-0112           ART='http://ng.internal/artifacts'
-0113           TASK='42'
-0114           TOKEN='install-tok-fixed-0000000000000000'
-0115           SEQ=0
-0116 
-0117           signal() {
-0118             local phase="$1" rc="${2:-0}" log="${3:-/dev/null}" digest="${4:-}"
-0119             SEQ=$((SEQ+1))
-0120             local q="$GW/v1/signal/$TASK/$TOKEN/$phase?rc=$rc&seq=$SEQ"
-0121             if [ -n "$digest" ]; then
-0122               q="$q&digest=$digest"
-0123             fi
-0124             for i in 1 2 3; do
-0125               curl -fsS -m 10 -X POST "$q" \
-0126                 -H 'Content-Type: text/plain' --data-binary "@$log" && return 0
-0127               sleep $((i*3))
-0128             done
-0129             logger -t lober "signal failed: $phase rc=$rc"
-0130             return 0
-0131           }
-0132 
-0133           run_hook() {
-0134             local digest="$1" required="$2" timeout_sec="$3"
-0135             curl -fsS -o /tmp/h.sh "$ART/hooks/$digest/script.sh" || return 1
-0136             echo "$digest  /tmp/h.sh" | sha256sum -c - || return 1
-0137             ( set -euo pipefail; timeout "$timeout_sec" bash /tmp/h.sh ) 2>&1 | tee /tmp/h.log
-0138             local rc=${PIPESTATUS[0]}
-0139             signal "hook-report" "$rc" /tmp/h.log "$digest"
-0140             [ "$rc" -eq 0 ] || [ "$required" != "true" ]
-0141           }
-0142           signal "post-start" 0
-0143           signal "packages-start" 0
-0144           signal "packages-done" 0
-0145         - |-
-0146           curtin in-target -- bash -c 'mkdir -p /var/lib/lober /var/lib/lober/hooks
-0147 
-0148           install -m 600 /dev/null /var/lib/lober/finalize.token
-0149           echo '\''finalize-tok-fixed-0000000000000000'\'' > /var/lib/lober/finalize.token
-0150 
-0151           install -m 600 /dev/null /var/lib/lober/params.env
-0152           cat > /var/lib/lober/params.env <<'\''LOBER_PARAMS_EOF'\''
-0153           LOBER_CONTRACT_VERSION='\'''\''
-0154           LOBER_TASK_ID='\''42'\''
-0155           LOBER_SN='\''SN-FIXED-0001'\''
-0156           LOBER_MAC='\'''\''
-0157           LOBER_UUID='\'''\''
-0158           LOBER_HOSTNAME='\''web-01'\''
-0159           LOBER_OS_FAMILY='\''ubuntu'\''
-0160           LOBER_OS_VERSION='\'''\''
-0161           LOBER_ARCH='\'''\''
-0162           LOBER_DISKS='\'''\''
-0163           LOBER_DISK_PRIMARY='\''/dev/disk/by-id/wwn-0x5000c500fixed0001'\''
-0164           LOBER_NICS='\'''\''
-0165           LOBER_RAM_MB='\'''\''
-0166           LOBER_CPU_CORES='\'''\''
-0167           LOBER_VENDOR='\'''\''
-0168           LOBER_PRODUCT='\'''\''
-0169           LOBER_BIZ_IP='\''10.20.0.15'\''
-0170           LOBER_BIZ_GW='\'''\''
-0171           LOBER_BIZ_MASK='\'''\''
-0172           LOBER_ARTIFACT_BASE='\''http://ng.internal/artifacts'\''
-0173           LOBER_GATEWAY_BASE='\''http://gw.internal'\''
-0174           LOBER_RACK='\'''\''
-0175           LOBER_ROW='\'''\''
-0176           LOBER_DC='\'''\''
-0177           LOBER_PARAMS_EOF
-0178 
-0179           cat > /var/lib/lober/hooks/manifest <<'\''LOBER_MANIFEST_EOF'\''
-0180           LOBER_MANIFEST_EOF
-0181 
-0182           cat > /usr/local/sbin/lober-init.sh <<'\''LOBER_INIT_EOF'\''
-0183           #!/bin/bash
-0184           . /var/lib/lober/params.env
-0185 
-0186           GW='\''http://gw.internal'\''
-0187           ART='\''http://ng.internal/artifacts'\''
-0188           TASK='\''42'\''
-0189           TOKEN='\''install-tok-fixed-0000000000000000'\''
-0190           SEQ=0
-0191 
-0192           signal() {
-0193             local phase="$1" rc="${2:-0}" log="${3:-/dev/null}" digest="${4:-}"
-0194             SEQ=$((SEQ+1))
-0195             local q="$GW/v1/signal/$TASK/$TOKEN/$phase?rc=$rc&seq=$SEQ"
-0196             if [ -n "$digest" ]; then
-0197               q="$q&digest=$digest"
-0198             fi
-0199             for i in 1 2 3; do
-0200               curl -fsS -m 10 -X POST "$q" \
-0201                 -H '\''Content-Type: text/plain'\'' --data-binary "@$log" && return 0
-0202               sleep $((i*3))
-0203             done
-0204             logger -t lober "signal failed: $phase rc=$rc"
-0205             return 0
-0206           }
-0207 
-0208           run_hook() {
-0209             local digest="$1" required="$2" timeout_sec="$3"
-0210             curl -fsS -o /tmp/h.sh "$ART/hooks/$digest/script.sh" || return 1
-0211             echo "$digest  /tmp/h.sh" | sha256sum -c - || return 1
-0212             ( set -euo pipefail; timeout "$timeout_sec" bash /tmp/h.sh ) 2>&1 | tee /tmp/h.log
-0213             local rc=${PIPESTATUS[0]}
-0214             signal "hook-report" "$rc" /tmp/h.log "$digest"
-0215             [ "$rc" -eq 0 ] || [ "$required" != "true" ]
-0216           }
-0217 
-0218           signal "booted" 0
-0219           signal "init-start" 0
-0220 
-0221           signal "init-done" 0
-0222           signal "ready" 0
-0223 
-0224           shred -u /var/lib/lober/params.env /var/lib/lober/finalize.token
-0225           rm -f /var/lib/lober/init.pending
-0226           sed -i '\''/rootpw\|--iscrypted\|inst.ks=/d'\'' /root/anaconda-ks.cfg 2>/dev/null
-0227           LOBER_INIT_EOF
-0228           chmod 755 /usr/local/sbin/lober-init.sh
-0229 
-0230           cat > /etc/systemd/system/lober-init.service <<'\''LOBER_UNIT_EOF'\''
-0231           [Unit]
-0232           Description=Lober OS init
-0233           After=network-online.target
-0234           Wants=network-online.target
-0235           ConditionPathExists=/var/lib/lober/init.pending
-0236           [Service]
-0237           Type=oneshot
-0238           ExecStart=/usr/local/sbin/lober-init.sh
-0239           StandardOutput=journal+console
-0240           [Install]
-0241           WantedBy=multi-user.target
-0242           LOBER_UNIT_EOF
-0243 
-0244           systemctl enable lober-init.service
-0245           touch /var/lib/lober/init.pending'
-0246         - |-
-0247           GW='http://gw.internal'
-0248           ART='http://ng.internal/artifacts'
-0249           TASK='42'
-0250           TOKEN='install-tok-fixed-0000000000000000'
-0251           SEQ=0
-0252 
-0253           signal() {
-0254             local phase="$1" rc="${2:-0}" log="${3:-/dev/null}" digest="${4:-}"
-0255             SEQ=$((SEQ+1))
-0256             local q="$GW/v1/signal/$TASK/$TOKEN/$phase?rc=$rc&seq=$SEQ"
-0257             if [ -n "$digest" ]; then
-0258               q="$q&digest=$digest"
-0259             fi
-0260             for i in 1 2 3; do
-0261               curl -fsS -m 10 -X POST "$q" \
-0262                 -H 'Content-Type: text/plain' --data-binary "@$log" && return 0
-0263               sleep $((i*3))
-0264             done
-0265             logger -t lober "signal failed: $phase rc=$rc"
-0266             return 0
-0267           }
-0268 
-0269           run_hook() {
-0270             local digest="$1" required="$2" timeout_sec="$3"
-0271             curl -fsS -o /tmp/h.sh "$ART/hooks/$digest/script.sh" || return 1
-0272             echo "$digest  /tmp/h.sh" | sha256sum -c - || return 1
-0273             ( set -euo pipefail; timeout "$timeout_sec" bash /tmp/h.sh ) 2>&1 | tee /tmp/h.log
-0274             local rc=${PIPESTATUS[0]}
-0275             signal "hook-report" "$rc" /tmp/h.log "$digest"
-0276             [ "$rc" -eq 0 ] || [ "$required" != "true" ]
-0277           }
-0278           signal "post-done" 0
-0279           signal "rebooting" 0
-0280     packages: []
-0281     ssh:
-0282         install-server: true
-0283         authorized-keys:
-0284             - ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIGZ5ZDhhOWMwZTNiNGY1YzZkN2E4YjljMGUzYjRmNWM2 ops@lober
+0003   version: 1
+0004   locale: en_US.UTF-8
+0005   keyboard:
+0006     layout: us
+0007   identity:
+0008     hostname: web-01
+0009     username: lober
+0010     password: '!'
+0011   storage:
+0012     config:
+0013       - {id: disk0, type: disk, path: /dev/sda, ptable: gpt, wipe: superblock-recursive, grub_device: true}
+0014       - {id: bios, type: partition, device: disk0, size: 1M, flag: bios_grub}
+0015       - {id: root, type: partition, device: disk0, size: -1}
+0016       - {id: root-fs, type: format, volume: root, fstype: ext4}
+0017       - {id: root-mount, type: mount, device: root-fs, path: /}
+0018   network:
+0019     version: 2
+0020     ethernets:
+0021       install-nic:
+0022         match:
+0023           name: en*
+0024         dhcp4: true
+0025   early-commands:
+0026     - echo c2V0IC1ldQp1bWFzayAwNzcKbWtkaXIgLXAgJy90bXAnCmN1cmwgLWZzUyAtLXJldHJ5IDMgLW8gJy90bXAvc2NyaXB0LTIuc2gnICdodHRwOi8vZ3cuaW50ZXJuYWwvdjEvYW5zd2VyL3Rhc2stNDIvaW5zdGFsbC10b2stZml4ZWQvc2NyaXB0cy8yLnNoJwpzaCAnL3RtcC9zY3JpcHQtMi5zaCcK | base64 -d | sh
+0027   late-commands:
+0028     - echo c2V0IC1ldQp1bWFzayAwNzcKbWtkaXIgLXAgJy90bXAnCmN1cmwgLWZzUyAtLXJldHJ5IDMgLW8gJy90bXAvc2NyaXB0LTMuc2gnICdodHRwOi8vZ3cuaW50ZXJuYWwvdjEvYW5zd2VyL3Rhc2stNDIvaW5zdGFsbC10b2stZml4ZWQvc2NyaXB0cy8zLnNoJwpzaCAnL3RtcC9zY3JpcHQtMy5zaCcK | base64 -d | sh
+0029     - echo c2V0IC1ldQp1bWFzayAwNzcKbWtkaXIgLXAgJy90YXJnZXQvdmFyL2xpYi9sb2JlcicKY3VybCAtZnNTIC0tcmV0cnkgMyAtbyAnL3RhcmdldC92YXIvbGliL2xvYmVyL3NjcmlwdC00LnNoJyAnaHR0cDovL2d3LmludGVybmFsL3YxL2Fuc3dlci90YXNrLTQyL2luc3RhbGwtdG9rLWZpeGVkL3NjcmlwdHMvNC5zaCcKY2hyb290ICcvdGFyZ2V0JyBzaCAnL3Zhci9saWIvbG9iZXIvc2NyaXB0LTQuc2gnCg== | base64 -d | sh
+0030     - echo c2V0IC1ldQp1bWFzayAwNzcKbWtkaXIgLXAgJy90YXJnZXQvdmFyL2xpYi9sb2JlcicKY3VybCAtZnNTIC0tcmV0cnkgMyAtbyAnL3RhcmdldC92YXIvbGliL2xvYmVyL3NjcmlwdC01LnNoJyAnaHR0cDovL2d3LmludGVybmFsL3YxL2Fuc3dlci90YXNrLTQyL2luc3RhbGwtdG9rLWZpeGVkL3NjcmlwdHMvNS5zaCcK | base64 -d | sh
+0031     - echo c2V0IC1ldQp1bWFzayAwNzcKbWtkaXIgLXAgJy90YXJnZXQvdmFyL2xpYi9sb2JlcicgJy90YXJnZXQvZXRjL3N5c3RlbWQvc3lzdGVtL211bHRpLXVzZXIudGFyZ2V0LndhbnRzJwpwcmludGYgJyVzJyAnZmluYWxpemUtdG9rLWZpeGVkJyA+ICcvdGFyZ2V0L3Zhci9saWIvbG9iZXIvZmluYWxpemUudG9rZW4nCmNhdCA+ICcvdGFyZ2V0L3Zhci9saWIvbG9iZXIvZmlyc3Rib290LnNoJyA8PCdMT0JFUl9GSVJTVEJPT1RfRU9GJwojIS9iaW4vc2gKc2V0IC1ldQpUT0tFTj0kKGNhdCAvdmFyL2xpYi9sb2Jlci9maW5hbGl6ZS50b2tlbikKY3VybCAtZnNTIC1tIDEwIC1YIFBPU1QgJ2h0dHA6Ly9ndy5pbnRlcm5hbC92MS9zaWduYWwvdGFzay00Mi8nIiRUT0tFTiInL2Jvb3RlZCcgLS1kYXRhLWJpbmFyeSAnJyA+L2Rldi9udWxsIDI+JjEgfHwgOgpUT0tFTj0kKGNhdCAvdmFyL2xpYi9sb2Jlci9maW5hbGl6ZS50b2tlbikKY3VybCAtZnNTIC1tIDEwIC1YIFBPU1QgJ2h0dHA6Ly9ndy5pbnRlcm5hbC92MS9zaWduYWwvdGFzay00Mi8nIiRUT0tFTiInL2luaXQtc3RhcnQnIC0tZGF0YS1iaW5hcnkgJycgPi9kZXYvbnVsbCAyPiYxIHx8IDoKc2ggL3Zhci9saWIvbG9iZXIvc2NyaXB0LTUuc2gKc2ggL3Zhci9saWIvbG9iZXIvY2xlYW51cC5zaApMT0JFUl9GSVJTVEJPT1RfRU9GCmNhdCA+ICcvdGFyZ2V0L2V0Yy9zeXN0ZW1kL3N5c3RlbS9sb2Jlci1maXJzdGJvb3Quc2VydmljZScgPDwnTE9CRVJfVU5JVF9FT0YnCltVbml0XQpEZXNjcmlwdGlvbj1Mb2JlciBmaXJzdCBib290CkFmdGVyPW5ldHdvcmstb25saW5lLnRhcmdldApXYW50cz1uZXR3b3JrLW9ubGluZS50YXJnZXQKQ29uZGl0aW9uUGF0aEV4aXN0cz0vdmFyL2xpYi9sb2Jlci9maXJzdGJvb3QucGVuZGluZwpbU2VydmljZV0KVHlwZT1vbmVzaG90CkV4ZWNTdGFydD0vYmluL3NoIC92YXIvbGliL2xvYmVyL2ZpcnN0Ym9vdC5zaApbSW5zdGFsbF0KV2FudGVkQnk9bXVsdGktdXNlci50YXJnZXQKTE9CRVJfVU5JVF9FT0YKbG4gLXNmIC4uL2xvYmVyLWZpcnN0Ym9vdC5zZXJ2aWNlICcvdGFyZ2V0L2V0Yy9zeXN0ZW1kL3N5c3RlbS9tdWx0aS11c2VyLnRhcmdldC53YW50cy9sb2Jlci1maXJzdGJvb3Quc2VydmljZScKdG91Y2ggJy90YXJnZXQvdmFyL2xpYi9sb2Jlci9maXJzdGJvb3QucGVuZGluZycK | base64 -d | sh
+0032     - echo c2V0IC1ldQp1bWFzayAwNzcKbWtkaXIgLXAgJy90YXJnZXQvdmFyL2xpYi9sb2JlcicKY2F0ID4gJy90YXJnZXQvdmFyL2xpYi9sb2Jlci9jbGVhbnVwLnNoJyA8PCdMT0JFUl9DTEVBTlVQX0VPRicKIyEvYmluL3NoCnNldCAtZXUKVE9LRU49JChjYXQgL3Zhci9saWIvbG9iZXIvZmluYWxpemUudG9rZW4pCmN1cmwgLWZzUyAtbSAxMCAtWCBQT1NUICdodHRwOi8vZ3cuaW50ZXJuYWwvdjEvc2lnbmFsL3Rhc2stNDIvJyIkVE9LRU4iJy9pbml0LWRvbmUnIC0tZGF0YS1iaW5hcnkgJycgPi9kZXYvbnVsbCAyPiYxIHx8IDoKVE9LRU49JChjYXQgL3Zhci9saWIvbG9iZXIvZmluYWxpemUudG9rZW4pCmN1cmwgLWZzUyAtbSAxMCAtWCBQT1NUICdodHRwOi8vZ3cuaW50ZXJuYWwvdjEvc2lnbmFsL3Rhc2stNDIvJyIkVE9LRU4iJy9yZWFkeScgLS1kYXRhLWJpbmFyeSAnJyA+L2Rldi9udWxsIDI+JjEgfHwgOgpybSAtZiAvdmFyL2xpYi9sb2Jlci9maW5hbGl6ZS50b2tlbiAvdmFyL2xpYi9sb2Jlci9maXJzdGJvb3QucGVuZGluZyAvdmFyL2xpYi9sb2Jlci9zY3JpcHQtKi5zaCAvdmFyL2xpYi9sb2Jlci9ob29rLSogL3Zhci9saWIvbG9iZXIvZmlyc3Rib290LnNoIC92YXIvbGliL2xvYmVyL2NsZWFudXAuc2gKcm0gLWYgL3Jvb3QvYW5hY29uZGEta3MuY2ZnIC9yb290L29yaWdpbmFsLWtzLmNmZyAvdmFyL2xvZy9pbnN0YWxsZXIvYXV0b2luc3RhbGwtdXNlci1kYXRhIC92YXIvbG9nL2luc3RhbGxlci9hdXRvaW5zdGFsbC11c2VyLWRhdGEudHh0IC92YXIvbG9nL2luc3RhbGxlci9jZGViY29uZi9xdWVzdGlvbnMuZGF0IC92YXIvYWRtL2F1dG9pbnN0YWxsL2NhY2hlL2luc3RhbGxlZFN5c3RlbS54bWwKTE9CRVJfQ0xFQU5VUF9FT0YK | base64 -d | sh
+0033   packages:
+0034     - curl
+0035   ssh:
+0036     install-server: true
+0037   shutdown: reboot
```

## autoinstall-minimal-uefi.golden

Compared with `autoinstall_minimal.golden`; firmware was not split in that old reference.

### Old 1–0; new 1–1

S2: handwritten YAML fixture uses cloud-config header, ordinary lober username rather than reserved root, explicit firmware GPT storage, match-based DHCP NIC, and curl package. Inline multiline early/late bodies become one-line commands; /target is installer mount, target script chroots. SSH key composition is hook-owned, not implicit renderer policy. shutdown/reboot remains native profile syntax.

S5: all remaining profile/blank/layout/value differences in this range are directly authored in the committed .tmpl/.fragment snapshots: hostname remains web-01; identity is task-42 (valid model ID) rather than old numeric 42; new tokens shorter but valid; full includes one real script per supported timing, minimal keeps the slot empty (still receives runtime wrapper for stage signals); SSH keys/legacy host metadata omitted from independent assembly fixtures because Task9 owns actual builtin defaults and SSH hook. No implicit old-model field assembly remains.

```diff
+0001 #cloud-config
```

### Old 2–284; new 3–39

S1: fixed global environment and inline signal/run_hook library are removed; flattened snapshot instances and FileScript now own per-instance EnvName/EnvValue, digest/interpreter/timeout/required execution. Task6 inserts one-line wrapper downloads, never copies old inline runtime. Legacy fake digest strings become real SHA-256 fixture digest. Task8 implements/verifies runtime bodies.

B1: firstboot now stages local slot wrappers and local digest payloads before reboot; systemd invokes the installed native runner. Boot credential no longer enters firstboot; independent local finalize token used. Cleanup follows success and erases cached answers/payloads. Legacy firstboot reused install TOKEN despite writing finalize.token. Unit/marker/local paths change to this explicit contract.

S2: handwritten YAML fixture uses cloud-config header, ordinary lober username rather than reserved root, explicit firmware GPT storage, match-based DHCP NIC, and curl package. Inline multiline early/late bodies become one-line commands; /target is installer mount, target script chroots. SSH key composition is hook-owned, not implicit renderer policy. shutdown/reboot remains native profile syntax.

S5: all remaining profile/blank/layout/value differences in this range are directly authored in the committed .tmpl/.fragment snapshots: hostname remains web-01; identity is task-42 (valid model ID) rather than old numeric 42; new tokens shorter but valid; full includes one real script per supported timing, minimal keeps the slot empty (still receives runtime wrapper for stage signals); SSH keys/legacy host metadata omitted from independent assembly fixtures because Task9 owns actual builtin defaults and SSH hook. No implicit old-model field assembly remains.

```diff
-0002     version: 1
-0003     locale: en_US.UTF-8
-0004     keyboard:
-0005         layout: us
-0006     identity:
-0007         hostname: web-01
-0008         username: root
-0009         password: '!'
-0010     storage:
-0011         layout:
-0012             name: direct
-0013     network:
-0014         version: 2
-0015         ethernets:
-0016             install-nic:
-0017                 dhcp4: true
-0018     early-commands:
-0019         - |-
-0020           LOBER_CONTRACT_VERSION=''
-0021           LOBER_TASK_ID='42'
-0022           LOBER_SN='SN-FIXED-0001'
-0023           LOBER_MAC=''
-0024           LOBER_UUID=''
-0025           LOBER_HOSTNAME='web-01'
-0026           LOBER_OS_FAMILY='ubuntu'
-0027           LOBER_OS_VERSION=''
-0028           LOBER_ARCH=''
-0029           LOBER_DISKS=''
-0030           LOBER_DISK_PRIMARY='/dev/disk/by-id/wwn-0x5000c500fixed0001'
-0031           LOBER_NICS=''
-0032           LOBER_RAM_MB=''
-0033           LOBER_CPU_CORES=''
-0034           LOBER_VENDOR=''
-0035           LOBER_PRODUCT=''
-0036           LOBER_BIZ_IP='10.20.0.15'
-0037           LOBER_BIZ_GW=''
-0038           LOBER_BIZ_MASK=''
-0039           LOBER_ARTIFACT_BASE='http://ng.internal/artifacts'
-0040           LOBER_GATEWAY_BASE='http://gw.internal'
-0041           LOBER_RACK=''
-0042           LOBER_ROW=''
-0043           LOBER_DC=''
-0044           GW='http://gw.internal'
-0045           ART='http://ng.internal/artifacts'
-0046           TASK='42'
-0047           TOKEN='install-tok-fixed-0000000000000000'
-0048           SEQ=0
-0049 
-0050           signal() {
-0051             local phase="$1" rc="${2:-0}" log="${3:-/dev/null}" digest="${4:-}"
-0052             SEQ=$((SEQ+1))
-0053             local q="$GW/v1/signal/$TASK/$TOKEN/$phase?rc=$rc&seq=$SEQ"
-0054             if [ -n "$digest" ]; then
-0055               q="$q&digest=$digest"
-0056             fi
-0057             for i in 1 2 3; do
-0058               curl -fsS -m 10 -X POST "$q" \
-0059                 -H 'Content-Type: text/plain' --data-binary "@$log" && return 0
-0060               sleep $((i*3))
-0061             done
-0062             logger -t lober "signal failed: $phase rc=$rc"
-0063             return 0
-0064           }
-0065 
-0066           run_hook() {
-0067             local digest="$1" required="$2" timeout_sec="$3"
-0068             curl -fsS -o /tmp/h.sh "$ART/hooks/$digest/script.sh" || return 1
-0069             echo "$digest  /tmp/h.sh" | sha256sum -c - || return 1
-0070             ( set -euo pipefail; timeout "$timeout_sec" bash /tmp/h.sh ) 2>&1 | tee /tmp/h.log
-0071             local rc=${PIPESTATUS[0]}
-0072             signal "hook-report" "$rc" /tmp/h.log "$digest"
-0073             [ "$rc" -eq 0 ] || [ "$required" != "true" ]
-0074           }
-0075           signal "pre-start" 0
-0076         - |-
-0077           GW='http://gw.internal'
-0078           ART='http://ng.internal/artifacts'
-0079           TASK='42'
-0080           TOKEN='install-tok-fixed-0000000000000000'
-0081           SEQ=0
-0082 
-0083           signal() {
-0084             local phase="$1" rc="${2:-0}" log="${3:-/dev/null}" digest="${4:-}"
-0085             SEQ=$((SEQ+1))
-0086             local q="$GW/v1/signal/$TASK/$TOKEN/$phase?rc=$rc&seq=$SEQ"
-0087             if [ -n "$digest" ]; then
-0088               q="$q&digest=$digest"
-0089             fi
-0090             for i in 1 2 3; do
-0091               curl -fsS -m 10 -X POST "$q" \
-0092                 -H 'Content-Type: text/plain' --data-binary "@$log" && return 0
-0093               sleep $((i*3))
-0094             done
-0095             logger -t lober "signal failed: $phase rc=$rc"
-0096             return 0
-0097           }
-0098 
-0099           run_hook() {
-0100             local digest="$1" required="$2" timeout_sec="$3"
-0101             curl -fsS -o /tmp/h.sh "$ART/hooks/$digest/script.sh" || return 1
-0102             echo "$digest  /tmp/h.sh" | sha256sum -c - || return 1
-0103             ( set -euo pipefail; timeout "$timeout_sec" bash /tmp/h.sh ) 2>&1 | tee /tmp/h.log
-0104             local rc=${PIPESTATUS[0]}
-0105             signal "hook-report" "$rc" /tmp/h.log "$digest"
-0106             [ "$rc" -eq 0 ] || [ "$required" != "true" ]
-0107           }
-0108           signal "pre-done" 0
-0109     late-commands:
-0110         - |-
-0111           GW='http://gw.internal'
-0112           ART='http://ng.internal/artifacts'
-0113           TASK='42'
-0114           TOKEN='install-tok-fixed-0000000000000000'
-0115           SEQ=0
-0116 
-0117           signal() {
-0118             local phase="$1" rc="${2:-0}" log="${3:-/dev/null}" digest="${4:-}"
-0119             SEQ=$((SEQ+1))
-0120             local q="$GW/v1/signal/$TASK/$TOKEN/$phase?rc=$rc&seq=$SEQ"
-0121             if [ -n "$digest" ]; then
-0122               q="$q&digest=$digest"
-0123             fi
-0124             for i in 1 2 3; do
-0125               curl -fsS -m 10 -X POST "$q" \
-0126                 -H 'Content-Type: text/plain' --data-binary "@$log" && return 0
-0127               sleep $((i*3))
-0128             done
-0129             logger -t lober "signal failed: $phase rc=$rc"
-0130             return 0
-0131           }
-0132 
-0133           run_hook() {
-0134             local digest="$1" required="$2" timeout_sec="$3"
-0135             curl -fsS -o /tmp/h.sh "$ART/hooks/$digest/script.sh" || return 1
-0136             echo "$digest  /tmp/h.sh" | sha256sum -c - || return 1
-0137             ( set -euo pipefail; timeout "$timeout_sec" bash /tmp/h.sh ) 2>&1 | tee /tmp/h.log
-0138             local rc=${PIPESTATUS[0]}
-0139             signal "hook-report" "$rc" /tmp/h.log "$digest"
-0140             [ "$rc" -eq 0 ] || [ "$required" != "true" ]
-0141           }
-0142           signal "post-start" 0
-0143           signal "packages-start" 0
-0144           signal "packages-done" 0
-0145         - |-
-0146           curtin in-target -- bash -c 'mkdir -p /var/lib/lober /var/lib/lober/hooks
-0147 
-0148           install -m 600 /dev/null /var/lib/lober/finalize.token
-0149           echo '\''finalize-tok-fixed-0000000000000000'\'' > /var/lib/lober/finalize.token
-0150 
-0151           install -m 600 /dev/null /var/lib/lober/params.env
-0152           cat > /var/lib/lober/params.env <<'\''LOBER_PARAMS_EOF'\''
-0153           LOBER_CONTRACT_VERSION='\'''\''
-0154           LOBER_TASK_ID='\''42'\''
-0155           LOBER_SN='\''SN-FIXED-0001'\''
-0156           LOBER_MAC='\'''\''
-0157           LOBER_UUID='\'''\''
-0158           LOBER_HOSTNAME='\''web-01'\''
-0159           LOBER_OS_FAMILY='\''ubuntu'\''
-0160           LOBER_OS_VERSION='\'''\''
-0161           LOBER_ARCH='\'''\''
-0162           LOBER_DISKS='\'''\''
-0163           LOBER_DISK_PRIMARY='\''/dev/disk/by-id/wwn-0x5000c500fixed0001'\''
-0164           LOBER_NICS='\'''\''
-0165           LOBER_RAM_MB='\'''\''
-0166           LOBER_CPU_CORES='\'''\''
-0167           LOBER_VENDOR='\'''\''
-0168           LOBER_PRODUCT='\'''\''
-0169           LOBER_BIZ_IP='\''10.20.0.15'\''
-0170           LOBER_BIZ_GW='\'''\''
-0171           LOBER_BIZ_MASK='\'''\''
-0172           LOBER_ARTIFACT_BASE='\''http://ng.internal/artifacts'\''
-0173           LOBER_GATEWAY_BASE='\''http://gw.internal'\''
-0174           LOBER_RACK='\'''\''
-0175           LOBER_ROW='\'''\''
-0176           LOBER_DC='\'''\''
-0177           LOBER_PARAMS_EOF
-0178 
-0179           cat > /var/lib/lober/hooks/manifest <<'\''LOBER_MANIFEST_EOF'\''
-0180           LOBER_MANIFEST_EOF
-0181 
-0182           cat > /usr/local/sbin/lober-init.sh <<'\''LOBER_INIT_EOF'\''
-0183           #!/bin/bash
-0184           . /var/lib/lober/params.env
-0185 
-0186           GW='\''http://gw.internal'\''
-0187           ART='\''http://ng.internal/artifacts'\''
-0188           TASK='\''42'\''
-0189           TOKEN='\''install-tok-fixed-0000000000000000'\''
-0190           SEQ=0
-0191 
-0192           signal() {
-0193             local phase="$1" rc="${2:-0}" log="${3:-/dev/null}" digest="${4:-}"
-0194             SEQ=$((SEQ+1))
-0195             local q="$GW/v1/signal/$TASK/$TOKEN/$phase?rc=$rc&seq=$SEQ"
-0196             if [ -n "$digest" ]; then
-0197               q="$q&digest=$digest"
-0198             fi
-0199             for i in 1 2 3; do
-0200               curl -fsS -m 10 -X POST "$q" \
-0201                 -H '\''Content-Type: text/plain'\'' --data-binary "@$log" && return 0
-0202               sleep $((i*3))
-0203             done
-0204             logger -t lober "signal failed: $phase rc=$rc"
-0205             return 0
-0206           }
-0207 
-0208           run_hook() {
-0209             local digest="$1" required="$2" timeout_sec="$3"
-0210             curl -fsS -o /tmp/h.sh "$ART/hooks/$digest/script.sh" || return 1
-0211             echo "$digest  /tmp/h.sh" | sha256sum -c - || return 1
-0212             ( set -euo pipefail; timeout "$timeout_sec" bash /tmp/h.sh ) 2>&1 | tee /tmp/h.log
-0213             local rc=${PIPESTATUS[0]}
-0214             signal "hook-report" "$rc" /tmp/h.log "$digest"
-0215             [ "$rc" -eq 0 ] || [ "$required" != "true" ]
-0216           }
-0217 
-0218           signal "booted" 0
-0219           signal "init-start" 0
-0220 
-0221           signal "init-done" 0
-0222           signal "ready" 0
-0223 
-0224           shred -u /var/lib/lober/params.env /var/lib/lober/finalize.token
-0225           rm -f /var/lib/lober/init.pending
-0226           sed -i '\''/rootpw\|--iscrypted\|inst.ks=/d'\'' /root/anaconda-ks.cfg 2>/dev/null
-0227           LOBER_INIT_EOF
-0228           chmod 755 /usr/local/sbin/lober-init.sh
-0229 
-0230           cat > /etc/systemd/system/lober-init.service <<'\''LOBER_UNIT_EOF'\''
-0231           [Unit]
-0232           Description=Lober OS init
-0233           After=network-online.target
-0234           Wants=network-online.target
-0235           ConditionPathExists=/var/lib/lober/init.pending
-0236           [Service]
-0237           Type=oneshot
-0238           ExecStart=/usr/local/sbin/lober-init.sh
-0239           StandardOutput=journal+console
-0240           [Install]
-0241           WantedBy=multi-user.target
-0242           LOBER_UNIT_EOF
-0243 
-0244           systemctl enable lober-init.service
-0245           touch /var/lib/lober/init.pending'
-0246         - |-
-0247           GW='http://gw.internal'
-0248           ART='http://ng.internal/artifacts'
-0249           TASK='42'
-0250           TOKEN='install-tok-fixed-0000000000000000'
-0251           SEQ=0
-0252 
-0253           signal() {
-0254             local phase="$1" rc="${2:-0}" log="${3:-/dev/null}" digest="${4:-}"
-0255             SEQ=$((SEQ+1))
-0256             local q="$GW/v1/signal/$TASK/$TOKEN/$phase?rc=$rc&seq=$SEQ"
-0257             if [ -n "$digest" ]; then
-0258               q="$q&digest=$digest"
-0259             fi
-0260             for i in 1 2 3; do
-0261               curl -fsS -m 10 -X POST "$q" \
-0262                 -H 'Content-Type: text/plain' --data-binary "@$log" && return 0
-0263               sleep $((i*3))
-0264             done
-0265             logger -t lober "signal failed: $phase rc=$rc"
-0266             return 0
-0267           }
-0268 
-0269           run_hook() {
-0270             local digest="$1" required="$2" timeout_sec="$3"
-0271             curl -fsS -o /tmp/h.sh "$ART/hooks/$digest/script.sh" || return 1
-0272             echo "$digest  /tmp/h.sh" | sha256sum -c - || return 1
-0273             ( set -euo pipefail; timeout "$timeout_sec" bash /tmp/h.sh ) 2>&1 | tee /tmp/h.log
-0274             local rc=${PIPESTATUS[0]}
-0275             signal "hook-report" "$rc" /tmp/h.log "$digest"
-0276             [ "$rc" -eq 0 ] || [ "$required" != "true" ]
-0277           }
-0278           signal "post-done" 0
-0279           signal "rebooting" 0
-0280     packages: []
-0281     ssh:
-0282         install-server: true
-0283         authorized-keys:
-0284             - ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIGZ5ZDhhOWMwZTNiNGY1YzZkN2E4YjljMGUzYjRmNWM2 ops@lober
+0003   version: 1
+0004   locale: en_US.UTF-8
+0005   keyboard:
+0006     layout: us
+0007   identity:
+0008     hostname: web-01
+0009     username: lober
+0010     password: '!'
+0011   storage:
+0012     config:
+0013       - {id: disk0, type: disk, path: /dev/sda, ptable: gpt, wipe: superblock-recursive}
+0014       - {id: esp, type: partition, device: disk0, size: 600M, flag: boot, grub_device: true}
+0015       - {id: esp-fs, type: format, volume: esp, fstype: fat32}
+0016       - {id: esp-mount, type: mount, device: esp-fs, path: /boot/efi}
+0017       - {id: root, type: partition, device: disk0, size: -1}
+0018       - {id: root-fs, type: format, volume: root, fstype: ext4}
+0019       - {id: root-mount, type: mount, device: root-fs, path: /}
+0020   network:
+0021     version: 2
+0022     ethernets:
+0023       install-nic:
+0024         match:
+0025           name: en*
+0026         dhcp4: true
+0027   early-commands:
+0028     - echo c2V0IC1ldQp1bWFzayAwNzcKbWtkaXIgLXAgJy90bXAnCmN1cmwgLWZzUyAtLXJldHJ5IDMgLW8gJy90bXAvc2NyaXB0LTIuc2gnICdodHRwOi8vZ3cuaW50ZXJuYWwvdjEvYW5zd2VyL3Rhc2stNDIvaW5zdGFsbC10b2stZml4ZWQvc2NyaXB0cy8yLnNoJwpzaCAnL3RtcC9zY3JpcHQtMi5zaCcK | base64 -d | sh
+0029   late-commands:
+0030     - echo c2V0IC1ldQp1bWFzayAwNzcKbWtkaXIgLXAgJy90bXAnCmN1cmwgLWZzUyAtLXJldHJ5IDMgLW8gJy90bXAvc2NyaXB0LTMuc2gnICdodHRwOi8vZ3cuaW50ZXJuYWwvdjEvYW5zd2VyL3Rhc2stNDIvaW5zdGFsbC10b2stZml4ZWQvc2NyaXB0cy8zLnNoJwpzaCAnL3RtcC9zY3JpcHQtMy5zaCcK | base64 -d | sh
+0031     - echo c2V0IC1ldQp1bWFzayAwNzcKbWtkaXIgLXAgJy90YXJnZXQvdmFyL2xpYi9sb2JlcicKY3VybCAtZnNTIC0tcmV0cnkgMyAtbyAnL3RhcmdldC92YXIvbGliL2xvYmVyL3NjcmlwdC00LnNoJyAnaHR0cDovL2d3LmludGVybmFsL3YxL2Fuc3dlci90YXNrLTQyL2luc3RhbGwtdG9rLWZpeGVkL3NjcmlwdHMvNC5zaCcKY2hyb290ICcvdGFyZ2V0JyBzaCAnL3Zhci9saWIvbG9iZXIvc2NyaXB0LTQuc2gnCg== | base64 -d | sh
+0032     - echo c2V0IC1ldQp1bWFzayAwNzcKbWtkaXIgLXAgJy90YXJnZXQvdmFyL2xpYi9sb2JlcicKY3VybCAtZnNTIC0tcmV0cnkgMyAtbyAnL3RhcmdldC92YXIvbGliL2xvYmVyL3NjcmlwdC01LnNoJyAnaHR0cDovL2d3LmludGVybmFsL3YxL2Fuc3dlci90YXNrLTQyL2luc3RhbGwtdG9rLWZpeGVkL3NjcmlwdHMvNS5zaCcK | base64 -d | sh
+0033     - echo c2V0IC1ldQp1bWFzayAwNzcKbWtkaXIgLXAgJy90YXJnZXQvdmFyL2xpYi9sb2JlcicgJy90YXJnZXQvZXRjL3N5c3RlbWQvc3lzdGVtL211bHRpLXVzZXIudGFyZ2V0LndhbnRzJwpwcmludGYgJyVzJyAnZmluYWxpemUtdG9rLWZpeGVkJyA+ICcvdGFyZ2V0L3Zhci9saWIvbG9iZXIvZmluYWxpemUudG9rZW4nCmNhdCA+ICcvdGFyZ2V0L3Zhci9saWIvbG9iZXIvZmlyc3Rib290LnNoJyA8PCdMT0JFUl9GSVJTVEJPT1RfRU9GJwojIS9iaW4vc2gKc2V0IC1ldQpUT0tFTj0kKGNhdCAvdmFyL2xpYi9sb2Jlci9maW5hbGl6ZS50b2tlbikKY3VybCAtZnNTIC1tIDEwIC1YIFBPU1QgJ2h0dHA6Ly9ndy5pbnRlcm5hbC92MS9zaWduYWwvdGFzay00Mi8nIiRUT0tFTiInL2Jvb3RlZCcgLS1kYXRhLWJpbmFyeSAnJyA+L2Rldi9udWxsIDI+JjEgfHwgOgpUT0tFTj0kKGNhdCAvdmFyL2xpYi9sb2Jlci9maW5hbGl6ZS50b2tlbikKY3VybCAtZnNTIC1tIDEwIC1YIFBPU1QgJ2h0dHA6Ly9ndy5pbnRlcm5hbC92MS9zaWduYWwvdGFzay00Mi8nIiRUT0tFTiInL2luaXQtc3RhcnQnIC0tZGF0YS1iaW5hcnkgJycgPi9kZXYvbnVsbCAyPiYxIHx8IDoKc2ggL3Zhci9saWIvbG9iZXIvc2NyaXB0LTUuc2gKc2ggL3Zhci9saWIvbG9iZXIvY2xlYW51cC5zaApMT0JFUl9GSVJTVEJPT1RfRU9GCmNhdCA+ICcvdGFyZ2V0L2V0Yy9zeXN0ZW1kL3N5c3RlbS9sb2Jlci1maXJzdGJvb3Quc2VydmljZScgPDwnTE9CRVJfVU5JVF9FT0YnCltVbml0XQpEZXNjcmlwdGlvbj1Mb2JlciBmaXJzdCBib290CkFmdGVyPW5ldHdvcmstb25saW5lLnRhcmdldApXYW50cz1uZXR3b3JrLW9ubGluZS50YXJnZXQKQ29uZGl0aW9uUGF0aEV4aXN0cz0vdmFyL2xpYi9sb2Jlci9maXJzdGJvb3QucGVuZGluZwpbU2VydmljZV0KVHlwZT1vbmVzaG90CkV4ZWNTdGFydD0vYmluL3NoIC92YXIvbGliL2xvYmVyL2ZpcnN0Ym9vdC5zaApbSW5zdGFsbF0KV2FudGVkQnk9bXVsdGktdXNlci50YXJnZXQKTE9CRVJfVU5JVF9FT0YKbG4gLXNmIC4uL2xvYmVyLWZpcnN0Ym9vdC5zZXJ2aWNlICcvdGFyZ2V0L2V0Yy9zeXN0ZW1kL3N5c3RlbS9tdWx0aS11c2VyLnRhcmdldC53YW50cy9sb2Jlci1maXJzdGJvb3Quc2VydmljZScKdG91Y2ggJy90YXJnZXQvdmFyL2xpYi9sb2Jlci9maXJzdGJvb3QucGVuZGluZycK | base64 -d | sh
+0034     - echo c2V0IC1ldQp1bWFzayAwNzcKbWtkaXIgLXAgJy90YXJnZXQvdmFyL2xpYi9sb2JlcicKY2F0ID4gJy90YXJnZXQvdmFyL2xpYi9sb2Jlci9jbGVhbnVwLnNoJyA8PCdMT0JFUl9DTEVBTlVQX0VPRicKIyEvYmluL3NoCnNldCAtZXUKVE9LRU49JChjYXQgL3Zhci9saWIvbG9iZXIvZmluYWxpemUudG9rZW4pCmN1cmwgLWZzUyAtbSAxMCAtWCBQT1NUICdodHRwOi8vZ3cuaW50ZXJuYWwvdjEvc2lnbmFsL3Rhc2stNDIvJyIkVE9LRU4iJy9pbml0LWRvbmUnIC0tZGF0YS1iaW5hcnkgJycgPi9kZXYvbnVsbCAyPiYxIHx8IDoKVE9LRU49JChjYXQgL3Zhci9saWIvbG9iZXIvZmluYWxpemUudG9rZW4pCmN1cmwgLWZzUyAtbSAxMCAtWCBQT1NUICdodHRwOi8vZ3cuaW50ZXJuYWwvdjEvc2lnbmFsL3Rhc2stNDIvJyIkVE9LRU4iJy9yZWFkeScgLS1kYXRhLWJpbmFyeSAnJyA+L2Rldi9udWxsIDI+JjEgfHwgOgpybSAtZiAvdmFyL2xpYi9sb2Jlci9maW5hbGl6ZS50b2tlbiAvdmFyL2xpYi9sb2Jlci9maXJzdGJvb3QucGVuZGluZyAvdmFyL2xpYi9sb2Jlci9zY3JpcHQtKi5zaCAvdmFyL2xpYi9sb2Jlci9ob29rLSogL3Zhci9saWIvbG9iZXIvZmlyc3Rib290LnNoIC92YXIvbGliL2xvYmVyL2NsZWFudXAuc2gKcm0gLWYgL3Jvb3QvYW5hY29uZGEta3MuY2ZnIC9yb290L29yaWdpbmFsLWtzLmNmZyAvdmFyL2xvZy9pbnN0YWxsZXIvYXV0b2luc3RhbGwtdXNlci1kYXRhIC92YXIvbG9nL2luc3RhbGxlci9hdXRvaW5zdGFsbC11c2VyLWRhdGEudHh0IC92YXIvbG9nL2luc3RhbGxlci9jZGViY29uZi9xdWVzdGlvbnMuZGF0IC92YXIvYWRtL2F1dG9pbnN0YWxsL2NhY2hlL2luc3RhbGxlZFN5c3RlbS54bWwKTE9CRVJfQ0xFQU5VUF9FT0YK | base64 -d | sh
+0035   packages:
+0036     - curl
+0037   ssh:
+0038     install-server: true
+0039   shutdown: reboot
```

## autoyast-full-bios.golden

Compared with `autoyast_full.golden`; firmware was not split in that old reference.

### Old 4–8; new 4–4

S3: handwritten XML is compact rather than old serializer indentation; default filename is omitted (optional upstream). pre-scripts run before install; a single chrooted=false script stages in /mnt and post-target slot explicitly chroots. Firmware partition fragment and curl dependency are explicit; networking/SSH defaults are builtin hook responsibilities, not implicit renderer fields.

S5: all remaining profile/blank/layout/value differences in this range are directly authored in the committed .tmpl/.fragment snapshots: hostname remains web-01; identity is task-42 (valid model ID) rather than old numeric 42; new tokens shorter but valid; full includes one real script per supported timing, minimal keeps the slot empty (still receives runtime wrapper for stage signals); SSH keys/legacy host metadata omitted from independent assembly fixtures because Task9 owns actual builtin defaults and SSH hook. No implicit old-model field assembly remains.

```diff
-0004   <general>
-0005     <mode>
-0006       <confirm config:type="boolean">false</confirm>
-0007     </mode>
-0008   </general>
+0004   <general><mode><confirm config:type="boolean">false</confirm></mode></general>
```

### Old 10–13; new 6–6

S3: handwritten XML is compact rather than old serializer indentation; default filename is omitted (optional upstream). pre-scripts run before install; a single chrooted=false script stages in /mnt and post-target slot explicitly chroots. Firmware partition fragment and curl dependency are explicit; networking/SSH defaults are builtin hook responsibilities, not implicit renderer fields.

S5: all remaining profile/blank/layout/value differences in this range are directly authored in the committed .tmpl/.fragment snapshots: hostname remains web-01; identity is task-42 (valid model ID) rather than old numeric 42; new tokens shorter but valid; full includes one real script per supported timing, minimal keeps the slot empty (still receives runtime wrapper for stage signals); SSH keys/legacy host metadata omitted from independent assembly fixtures because Task9 owns actual builtin defaults and SSH hook. No implicit old-model field assembly remains.

```diff
-0010     <drive>
-0011       <device>/dev/disk/by-id/wwn-0x5000c500fixed0001</device>
-0012       <use>all</use>
-0013     </drive>
+0006     <drive><device>/dev/sda</device><use>all</use><disklabel>gpt</disklabel><partitions config:type="list"><partition><size>1M</size><partition_id config:type="integer">263</partition_id></partition><partition><mount>/</mount><size>max</size><filesystem config:type="symbol">btrfs</filesystem></partition></partitions></drive>
```

### Old 15–22; new 8–9

S3: handwritten XML is compact rather than old serializer indentation; default filename is omitted (optional upstream). pre-scripts run before install; a single chrooted=false script stages in /mnt and post-target slot explicitly chroots. Firmware partition fragment and curl dependency are explicit; networking/SSH defaults are builtin hook responsibilities, not implicit renderer fields.

S5: all remaining profile/blank/layout/value differences in this range are directly authored in the committed .tmpl/.fragment snapshots: hostname remains web-01; identity is task-42 (valid model ID) rather than old numeric 42; new tokens shorter but valid; full includes one real script per supported timing, minimal keeps the slot empty (still receives runtime wrapper for stage signals); SSH keys/legacy host metadata omitted from independent assembly fixtures because Task9 owns actual builtin defaults and SSH hook. No implicit old-model field assembly remains.

```diff
-0015   <networking>
-0016     <dns>
-0017       <hostname>web-01</hostname>
-0018     </dns>
-0019     <dhcp_options>
-0020       <dhclient_client_id/>
-0021     </dhcp_options>
-0022   </networking>
+0008   <networking><dns><hostname>web-01</hostname></dns></networking>
+0009   <software><packages config:type="list"><package>curl</package></packages></software>
```

### Old 25–89; new 12–15

S1: fixed global environment and inline signal/run_hook library are removed; flattened snapshot instances and FileScript now own per-instance EnvName/EnvValue, digest/interpreter/timeout/required execution. Task6 inserts one-line wrapper downloads, never copies old inline runtime. Legacy fake digest strings become real SHA-256 fixture digest. Task8 implements/verifies runtime bodies.

S3: handwritten XML is compact rather than old serializer indentation; default filename is omitted (optional upstream). pre-scripts run before install; a single chrooted=false script stages in /mnt and post-target slot explicitly chroots. Firmware partition fragment and curl dependency are explicit; networking/SSH defaults are builtin hook responsibilities, not implicit renderer fields.

S5: all remaining profile/blank/layout/value differences in this range are directly authored in the committed .tmpl/.fragment snapshots: hostname remains web-01; identity is task-42 (valid model ID) rather than old numeric 42; new tokens shorter but valid; full includes one real script per supported timing, minimal keeps the slot empty (still receives runtime wrapper for stage signals); SSH keys/legacy host metadata omitted from independent assembly fixtures because Task9 owns actual builtin defaults and SSH hook. No implicit old-model field assembly remains.

```diff
-0025       <script>
-0026         <filename>lober-pre.sh</filename>
-0027         <interpreter>shell</interpreter>
-0028         <source><![CDATA[
-0029 LOBER_CONTRACT_VERSION=''
-0030 LOBER_TASK_ID='42'
-0031 LOBER_SN='SN-FIXED-0001'
-0032 LOBER_MAC=''
-0033 LOBER_UUID=''
-0034 LOBER_HOSTNAME='web-01'
-0035 LOBER_OS_FAMILY='suse'
-0036 LOBER_OS_VERSION=''
-0037 LOBER_ARCH=''
-0038 LOBER_DISKS=''
-0039 LOBER_DISK_PRIMARY='/dev/disk/by-id/wwn-0x5000c500fixed0001'
-0040 LOBER_NICS=''
-0041 LOBER_RAM_MB=''
-0042 LOBER_CPU_CORES=''
-0043 LOBER_VENDOR=''
-0044 LOBER_PRODUCT=''
-0045 LOBER_BIZ_IP='10.20.0.15'
-0046 LOBER_BIZ_GW=''
-0047 LOBER_BIZ_MASK=''
-0048 LOBER_ARTIFACT_BASE='http://ng.internal/artifacts'
-0049 LOBER_GATEWAY_BASE='http://gw.internal'
-0050 LOBER_RACK=''
-0051 LOBER_ROW=''
-0052 LOBER_DC=''
-0053 # 用户 hook 从这里开始
-0054 GW='http://gw.internal'
-0055 ART='http://ng.internal/artifacts'
-0056 TASK='42'
-0057 TOKEN='install-tok-fixed-0000000000000000'
-0058 SEQ=0
-0059 
-0060 signal() {
-0061   local phase="$1" rc="${2:-0}" log="${3:-/dev/null}" digest="${4:-}"
-0062   SEQ=$((SEQ+1))
-0063   local q="$GW/v1/signal/$TASK/$TOKEN/$phase?rc=$rc&seq=$SEQ"
-0064   if [ -n "$digest" ]; then
-0065     q="$q&digest=$digest"
-0066   fi
-0067   for i in 1 2 3; do
-0068     curl -fsS -m 10 -X POST "$q" \
-0069       -H 'Content-Type: text/plain' --data-binary "@$log" && return 0
-0070     sleep $((i*3))
-0071   done
-0072   logger -t lober "signal failed: $phase rc=$rc"
-0073   return 0
-0074 }
-0075 
-0076 run_hook() {
-0077   local digest="$1" required="$2" timeout_sec="$3"
-0078   curl -fsS -o /tmp/h.sh "$ART/hooks/$digest/script.sh" || return 1
-0079   echo "$digest  /tmp/h.sh" | sha256sum -c - || return 1
-0080   ( set -euo pipefail; timeout "$timeout_sec" bash /tmp/h.sh ) 2>&1 | tee /tmp/h.log
-0081   local rc=${PIPESTATUS[0]}
-0082   signal "hook-report" "$rc" /tmp/h.log "$digest"
-0083   [ "$rc" -eq 0 ] || [ "$required" != "true" ]
-0084 }
-0085 signal "pre-start" 0
-0086 run_hook 'aaaa1111pre' 'true' '300' || exit 1
-0087 signal "pre-done" 0
-0088 ]]></source>
-0089       </script>
+0012       <script><interpreter>shell</interpreter><source><![CDATA[
+0013 set -eu
+0014 echo c2V0IC1ldQp1bWFzayAwNzcKbWtkaXIgLXAgJy90bXAnCmN1cmwgLWZzUyAtLXJldHJ5IDMgLW8gJy90bXAvc2NyaXB0LTIuc2gnICdodHRwOi8vZ3cuaW50ZXJuYWwvdjEvYW5zd2VyL3Rhc2stNDIvaW5zdGFsbC10b2stZml4ZWQvc2NyaXB0cy8yLnNoJwpzaCAnL3RtcC9zY3JpcHQtMi5zaCcK | base64 -d | sh
+0015 ]]></source></script>
```

### Old 92–285; new 18–25

S1: fixed global environment and inline signal/run_hook library are removed; flattened snapshot instances and FileScript now own per-instance EnvName/EnvValue, digest/interpreter/timeout/required execution. Task6 inserts one-line wrapper downloads, never copies old inline runtime. Legacy fake digest strings become real SHA-256 fixture digest. Task8 implements/verifies runtime bodies.

B1: firstboot now stages local slot wrappers and local digest payloads before reboot; systemd invokes the installed native runner. Boot credential no longer enters firstboot; independent local finalize token used. Cleanup follows success and erases cached answers/payloads. Legacy firstboot reused install TOKEN despite writing finalize.token. Unit/marker/local paths change to this explicit contract.

S3: handwritten XML is compact rather than old serializer indentation; default filename is omitted (optional upstream). pre-scripts run before install; a single chrooted=false script stages in /mnt and post-target slot explicitly chroots. Firmware partition fragment and curl dependency are explicit; networking/SSH defaults are builtin hook responsibilities, not implicit renderer fields.

S5: all remaining profile/blank/layout/value differences in this range are directly authored in the committed .tmpl/.fragment snapshots: hostname remains web-01; identity is task-42 (valid model ID) rather than old numeric 42; new tokens shorter but valid; full includes one real script per supported timing, minimal keeps the slot empty (still receives runtime wrapper for stage signals); SSH keys/legacy host metadata omitted from independent assembly fixtures because Task9 owns actual builtin defaults and SSH hook. No implicit old-model field assembly remains.

```diff
-0092       <script>
-0093         <chrooted config:type="boolean">false</chrooted>
-0094         <filename>lober-post-nochroot.sh</filename>
-0095         <interpreter>shell</interpreter>
-0096         <source><![CDATA[
-0097 GW='http://gw.internal'
-0098 ART='http://ng.internal/artifacts'
-0099 TASK='42'
-0100 TOKEN='install-tok-fixed-0000000000000000'
-0101 SEQ=0
-0102 
-0103 signal() {
-0104   local phase="$1" rc="${2:-0}" log="${3:-/dev/null}" digest="${4:-}"
-0105   SEQ=$((SEQ+1))
-0106   local q="$GW/v1/signal/$TASK/$TOKEN/$phase?rc=$rc&seq=$SEQ"
-0107   if [ -n "$digest" ]; then
-0108     q="$q&digest=$digest"
-0109   fi
-0110   for i in 1 2 3; do
-0111     curl -fsS -m 10 -X POST "$q" \
-0112       -H 'Content-Type: text/plain' --data-binary "@$log" && return 0
-0113     sleep $((i*3))
-0114   done
-0115   logger -t lober "signal failed: $phase rc=$rc"
-0116   return 0
-0117 }
-0118 
-0119 run_hook() {
-0120   local digest="$1" required="$2" timeout_sec="$3"
-0121   curl -fsS -o /tmp/h.sh "$ART/hooks/$digest/script.sh" || return 1
-0122   echo "$digest  /tmp/h.sh" | sha256sum -c - || return 1
-0123   ( set -euo pipefail; timeout "$timeout_sec" bash /tmp/h.sh ) 2>&1 | tee /tmp/h.log
-0124   local rc=${PIPESTATUS[0]}
-0125   signal "hook-report" "$rc" /tmp/h.log "$digest"
-0126   [ "$rc" -eq 0 ] || [ "$required" != "true" ]
-0127 }
-0128 run_hook 'bbbb2222nochroot' 'false' '300' || exit 1
-0129 ]]></source>
-0130       </script>
-0131       <script>
-0132         <chrooted config:type="boolean">true</chrooted>
-0133         <filename>lober-post.sh</filename>
-0134         <interpreter>shell</interpreter>
-0135         <source><![CDATA[
-0136 GW='http://gw.internal'
-0137 ART='http://ng.internal/artifacts'
-0138 TASK='42'
-0139 TOKEN='install-tok-fixed-0000000000000000'
-0140 SEQ=0
-0141 
-0142 signal() {
-0143   local phase="$1" rc="${2:-0}" log="${3:-/dev/null}" digest="${4:-}"
-0144   SEQ=$((SEQ+1))
-0145   local q="$GW/v1/signal/$TASK/$TOKEN/$phase?rc=$rc&seq=$SEQ"
-0146   if [ -n "$digest" ]; then
-0147     q="$q&digest=$digest"
-0148   fi
-0149   for i in 1 2 3; do
-0150     curl -fsS -m 10 -X POST "$q" \
-0151       -H 'Content-Type: text/plain' --data-binary "@$log" && return 0
-0152     sleep $((i*3))
-0153   done
-0154   logger -t lober "signal failed: $phase rc=$rc"
-0155   return 0
-0156 }
-0157 
-0158 run_hook() {
-0159   local digest="$1" required="$2" timeout_sec="$3"
-0160   curl -fsS -o /tmp/h.sh "$ART/hooks/$digest/script.sh" || return 1
-0161   echo "$digest  /tmp/h.sh" | sha256sum -c - || return 1
-0162   ( set -euo pipefail; timeout "$timeout_sec" bash /tmp/h.sh ) 2>&1 | tee /tmp/h.log
-0163   local rc=${PIPESTATUS[0]}
-0164   signal "hook-report" "$rc" /tmp/h.log "$digest"
-0165   [ "$rc" -eq 0 ] || [ "$required" != "true" ]
-0166 }
-0167 signal "post-start" 0
-0168 mkdir -p /root/.ssh && chmod 700 /root/.ssh
-0169 cat >> /root/.ssh/authorized_keys <<'LOBER_SSH_AUTHORIZED_KEYS_EOF'
-0170 ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIGZ5ZDhhOWMwZTNiNGY1YzZkN2E4YjljMGUzYjRmNWM2 ops@lober
-0171 LOBER_SSH_AUTHORIZED_KEYS_EOF
-0172 chmod 600 /root/.ssh/authorized_keys
-0173 run_hook 'cccc3333post' 'true' '300' || exit 1
-0174 mkdir -p /var/lib/lober /var/lib/lober/hooks
-0175 
-0176 install -m 600 /dev/null /var/lib/lober/finalize.token
-0177 echo 'finalize-tok-fixed-0000000000000000' > /var/lib/lober/finalize.token
-0178 
-0179 install -m 600 /dev/null /var/lib/lober/params.env
-0180 cat > /var/lib/lober/params.env <<'LOBER_PARAMS_EOF'
-0181 LOBER_CONTRACT_VERSION=''
-0182 LOBER_TASK_ID='42'
-0183 LOBER_SN='SN-FIXED-0001'
-0184 LOBER_MAC=''
-0185 LOBER_UUID=''
-0186 LOBER_HOSTNAME='web-01'
-0187 LOBER_OS_FAMILY='suse'
-0188 LOBER_OS_VERSION=''
-0189 LOBER_ARCH=''
-0190 LOBER_DISKS=''
-0191 LOBER_DISK_PRIMARY='/dev/disk/by-id/wwn-0x5000c500fixed0001'
-0192 LOBER_NICS=''
-0193 LOBER_RAM_MB=''
-0194 LOBER_CPU_CORES=''
-0195 LOBER_VENDOR=''
-0196 LOBER_PRODUCT=''
-0197 LOBER_BIZ_IP='10.20.0.15'
-0198 LOBER_BIZ_GW=''
-0199 LOBER_BIZ_MASK=''
-0200 LOBER_ARTIFACT_BASE='http://ng.internal/artifacts'
-0201 LOBER_GATEWAY_BASE='http://gw.internal'
-0202 LOBER_RACK=''
-0203 LOBER_ROW=''
-0204 LOBER_DC=''
-0205 LOBER_PARAMS_EOF
-0206 
-0207 cat > /var/lib/lober/hooks/manifest <<'LOBER_MANIFEST_EOF'
-0208 010-dddd4444firstboot.sh digest=dddd4444firstboot required=true order=10
-0209 LOBER_MANIFEST_EOF
-0210 
-0211 curl -fsS -o /var/lib/lober/hooks/010-dddd4444firstboot.sh 'http://ng.internal/artifacts/hooks/dddd4444firstboot/script.sh'
-0212 echo 'dddd4444firstboot  /var/lib/lober/hooks/010-dddd4444firstboot.sh' | sha256sum -c -
-0213 chmod 700 /var/lib/lober/hooks/010-dddd4444firstboot.sh
-0214 
-0215 cat > /usr/local/sbin/lober-init.sh <<'LOBER_INIT_EOF'
-0216 #!/bin/bash
-0217 . /var/lib/lober/params.env
-0218 
-0219 GW='http://gw.internal'
-0220 ART='http://ng.internal/artifacts'
-0221 TASK='42'
-0222 TOKEN='install-tok-fixed-0000000000000000'
-0223 SEQ=0
-0224 
-0225 signal() {
-0226   local phase="$1" rc="${2:-0}" log="${3:-/dev/null}" digest="${4:-}"
-0227   SEQ=$((SEQ+1))
-0228   local q="$GW/v1/signal/$TASK/$TOKEN/$phase?rc=$rc&seq=$SEQ"
-0229   if [ -n "$digest" ]; then
-0230     q="$q&digest=$digest"
-0231   fi
-0232   for i in 1 2 3; do
-0233     curl -fsS -m 10 -X POST "$q" \
-0234       -H 'Content-Type: text/plain' --data-binary "@$log" && return 0
-0235     sleep $((i*3))
-0236   done
-0237   logger -t lober "signal failed: $phase rc=$rc"
-0238   return 0
-0239 }
-0240 
-0241 run_hook() {
-0242   local digest="$1" required="$2" timeout_sec="$3"
-0243   curl -fsS -o /tmp/h.sh "$ART/hooks/$digest/script.sh" || return 1
-0244   echo "$digest  /tmp/h.sh" | sha256sum -c - || return 1
-0245   ( set -euo pipefail; timeout "$timeout_sec" bash /tmp/h.sh ) 2>&1 | tee /tmp/h.log
-0246   local rc=${PIPESTATUS[0]}
-0247   signal "hook-report" "$rc" /tmp/h.log "$digest"
-0248   [ "$rc" -eq 0 ] || [ "$required" != "true" ]
-0249 }
-0250 
-0251 signal "booted" 0
-0252 signal "init-start" 0
-0253 
-0254 bash /var/lib/lober/hooks/010-dddd4444firstboot.sh > /tmp/fb-dddd4444firstboot.log 2>&1
-0255 signal "hook-report" "$?" /tmp/fb-dddd4444firstboot.log "dddd4444firstboot"
-0256 
-0257 signal "init-done" 0
-0258 signal "ready" 0
-0259 
-0260 shred -u /var/lib/lober/params.env /var/lib/lober/finalize.token
-0261 rm -f /var/lib/lober/init.pending
-0262 sed -i '/rootpw\|--iscrypted\|inst.ks=/d' /root/anaconda-ks.cfg 2>/dev/null
-0263 LOBER_INIT_EOF
-0264 chmod 755 /usr/local/sbin/lober-init.sh
-0265 
-0266 cat > /etc/systemd/system/lober-init.service <<'LOBER_UNIT_EOF'
-0267 [Unit]
-0268 Description=Lober OS init
-0269 After=network-online.target
-0270 Wants=network-online.target
-0271 ConditionPathExists=/var/lib/lober/init.pending
-0272 [Service]
-0273 Type=oneshot
-0274 ExecStart=/usr/local/sbin/lober-init.sh
-0275 StandardOutput=journal+console
-0276 [Install]
-0277 WantedBy=multi-user.target
-0278 LOBER_UNIT_EOF
-0279 
-0280 systemctl enable lober-init.service
-0281 touch /var/lib/lober/init.pending
-0282 signal "post-done" 0
-0283 signal "rebooting" 0
-0284 ]]></source>
-0285       </script>
+0018       <script><chrooted config:type="boolean">false</chrooted><interpreter>shell</interpreter><source><![CDATA[
+0019 set -eu
+0020 echo c2V0IC1ldQp1bWFzayAwNzcKbWtkaXIgLXAgJy90bXAnCmN1cmwgLWZzUyAtLXJldHJ5IDMgLW8gJy90bXAvc2NyaXB0LTMuc2gnICdodHRwOi8vZ3cuaW50ZXJuYWwvdjEvYW5zd2VyL3Rhc2stNDIvaW5zdGFsbC10b2stZml4ZWQvc2NyaXB0cy8zLnNoJwpzaCAnL3RtcC9zY3JpcHQtMy5zaCcK | base64 -d | sh
+0021 echo c2V0IC1ldQp1bWFzayAwNzcKbWtkaXIgLXAgJy9tbnQvdmFyL2xpYi9sb2JlcicKY3VybCAtZnNTIC0tcmV0cnkgMyAtbyAnL21udC92YXIvbGliL2xvYmVyL3NjcmlwdC00LnNoJyAnaHR0cDovL2d3LmludGVybmFsL3YxL2Fuc3dlci90YXNrLTQyL2luc3RhbGwtdG9rLWZpeGVkL3NjcmlwdHMvNC5zaCcKY2hyb290ICcvbW50JyBzaCAnL3Zhci9saWIvbG9iZXIvc2NyaXB0LTQuc2gnCg== | base64 -d | sh
+0022 echo c2V0IC1ldQp1bWFzayAwNzcKbWtkaXIgLXAgJy9tbnQvdmFyL2xpYi9sb2JlcicKY3VybCAtZnNTIC0tcmV0cnkgMyAtbyAnL21udC92YXIvbGliL2xvYmVyL3NjcmlwdC01LnNoJyAnaHR0cDovL2d3LmludGVybmFsL3YxL2Fuc3dlci90YXNrLTQyL2luc3RhbGwtdG9rLWZpeGVkL3NjcmlwdHMvNS5zaCcKY3VybCAtZnNTIC0tcmV0cnkgMyAtbyAnL21udC92YXIvbGliL2xvYmVyL2hvb2stM2VlMDQ5ZTU5MWY1ZTk5YWU4ZjQ5OGFlMzgxZjQ1OTM2NWMyMjFlNDFlNmI0MmYyYWQ2ZTlhNmE5NzUwNTMyZS5zaCcgJ2h0dHA6Ly9ndy5pbnRlcm5hbC92MS9hbnN3ZXIvdGFzay00Mi9pbnN0YWxsLXRvay1maXhlZC9ob29rcy8zZWUwNDllNTkxZjVlOTlhZThmNDk4YWUzODFmNDU5MzY1YzIyMWU0MWU2YjQyZjJhZDZlOWE2YTk3NTA1MzJlJwo= | base64 -d | sh
+0023 echo c2V0IC1ldQp1bWFzayAwNzcKbWtkaXIgLXAgJy9tbnQvdmFyL2xpYi9sb2JlcicgJy9tbnQvZXRjL3N5c3RlbWQvc3lzdGVtL211bHRpLXVzZXIudGFyZ2V0LndhbnRzJwpwcmludGYgJyVzJyAnZmluYWxpemUtdG9rLWZpeGVkJyA+ICcvbW50L3Zhci9saWIvbG9iZXIvZmluYWxpemUudG9rZW4nCmNhdCA+ICcvbW50L3Zhci9saWIvbG9iZXIvZmlyc3Rib290LnNoJyA8PCdMT0JFUl9GSVJTVEJPT1RfRU9GJwojIS9iaW4vc2gKc2V0IC1ldQpUT0tFTj0kKGNhdCAvdmFyL2xpYi9sb2Jlci9maW5hbGl6ZS50b2tlbikKY3VybCAtZnNTIC1tIDEwIC1YIFBPU1QgJ2h0dHA6Ly9ndy5pbnRlcm5hbC92MS9zaWduYWwvdGFzay00Mi8nIiRUT0tFTiInL2Jvb3RlZCcgLS1kYXRhLWJpbmFyeSAnJyA+L2Rldi9udWxsIDI+JjEgfHwgOgpUT0tFTj0kKGNhdCAvdmFyL2xpYi9sb2Jlci9maW5hbGl6ZS50b2tlbikKY3VybCAtZnNTIC1tIDEwIC1YIFBPU1QgJ2h0dHA6Ly9ndy5pbnRlcm5hbC92MS9zaWduYWwvdGFzay00Mi8nIiRUT0tFTiInL2luaXQtc3RhcnQnIC0tZGF0YS1iaW5hcnkgJycgPi9kZXYvbnVsbCAyPiYxIHx8IDoKc2ggL3Zhci9saWIvbG9iZXIvc2NyaXB0LTUuc2gKc2ggL3Zhci9saWIvbG9iZXIvY2xlYW51cC5zaApMT0JFUl9GSVJTVEJPT1RfRU9GCmNhdCA+ICcvbW50L2V0Yy9zeXN0ZW1kL3N5c3RlbS9sb2Jlci1maXJzdGJvb3Quc2VydmljZScgPDwnTE9CRVJfVU5JVF9FT0YnCltVbml0XQpEZXNjcmlwdGlvbj1Mb2JlciBmaXJzdCBib290CkFmdGVyPW5ldHdvcmstb25saW5lLnRhcmdldApXYW50cz1uZXR3b3JrLW9ubGluZS50YXJnZXQKQ29uZGl0aW9uUGF0aEV4aXN0cz0vdmFyL2xpYi9sb2Jlci9maXJzdGJvb3QucGVuZGluZwpbU2VydmljZV0KVHlwZT1vbmVzaG90CkV4ZWNTdGFydD0vYmluL3NoIC92YXIvbGliL2xvYmVyL2ZpcnN0Ym9vdC5zaApbSW5zdGFsbF0KV2FudGVkQnk9bXVsdGktdXNlci50YXJnZXQKTE9CRVJfVU5JVF9FT0YKbG4gLXNmIC4uL2xvYmVyLWZpcnN0Ym9vdC5zZXJ2aWNlICcvbW50L2V0Yy9zeXN0ZW1kL3N5c3RlbS9tdWx0aS11c2VyLnRhcmdldC53YW50cy9sb2Jlci1maXJzdGJvb3Quc2VydmljZScKdG91Y2ggJy9tbnQvdmFyL2xpYi9sb2Jlci9maXJzdGJvb3QucGVuZGluZycK | base64 -d | sh
+0024 echo c2V0IC1ldQp1bWFzayAwNzcKbWtkaXIgLXAgJy9tbnQvdmFyL2xpYi9sb2JlcicKY2F0ID4gJy9tbnQvdmFyL2xpYi9sb2Jlci9jbGVhbnVwLnNoJyA8PCdMT0JFUl9DTEVBTlVQX0VPRicKIyEvYmluL3NoCnNldCAtZXUKVE9LRU49JChjYXQgL3Zhci9saWIvbG9iZXIvZmluYWxpemUudG9rZW4pCmN1cmwgLWZzUyAtbSAxMCAtWCBQT1NUICdodHRwOi8vZ3cuaW50ZXJuYWwvdjEvc2lnbmFsL3Rhc2stNDIvJyIkVE9LRU4iJy9pbml0LWRvbmUnIC0tZGF0YS1iaW5hcnkgJycgPi9kZXYvbnVsbCAyPiYxIHx8IDoKVE9LRU49JChjYXQgL3Zhci9saWIvbG9iZXIvZmluYWxpemUudG9rZW4pCmN1cmwgLWZzUyAtbSAxMCAtWCBQT1NUICdodHRwOi8vZ3cuaW50ZXJuYWwvdjEvc2lnbmFsL3Rhc2stNDIvJyIkVE9LRU4iJy9yZWFkeScgLS1kYXRhLWJpbmFyeSAnJyA+L2Rldi9udWxsIDI+JjEgfHwgOgpybSAtZiAvdmFyL2xpYi9sb2Jlci9maW5hbGl6ZS50b2tlbiAvdmFyL2xpYi9sb2Jlci9maXJzdGJvb3QucGVuZGluZyAvdmFyL2xpYi9sb2Jlci9zY3JpcHQtKi5zaCAvdmFyL2xpYi9sb2Jlci9ob29rLSogL3Zhci9saWIvbG9iZXIvZmlyc3Rib290LnNoIC92YXIvbGliL2xvYmVyL2NsZWFudXAuc2gKcm0gLWYgL3Jvb3QvYW5hY29uZGEta3MuY2ZnIC9yb290L29yaWdpbmFsLWtzLmNmZyAvdmFyL2xvZy9pbnN0YWxsZXIvYXV0b2luc3RhbGwtdXNlci1kYXRhIC92YXIvbG9nL2luc3RhbGxlci9hdXRvaW5zdGFsbC11c2VyLWRhdGEudHh0IC92YXIvbG9nL2luc3RhbGxlci9jZGViY29uZi9xdWVzdGlvbnMuZGF0IC92YXIvYWRtL2F1dG9pbnN0YWxsL2NhY2hlL2luc3RhbGxlZFN5c3RlbS54bWwKTE9CRVJfQ0xFQU5VUF9FT0YK | base64 -d | sh
+0025 ]]></source></script>
```

## autoyast-full-uefi.golden

Compared with `autoyast_full.golden`; firmware was not split in that old reference.

### Old 4–8; new 4–4

S3: handwritten XML is compact rather than old serializer indentation; default filename is omitted (optional upstream). pre-scripts run before install; a single chrooted=false script stages in /mnt and post-target slot explicitly chroots. Firmware partition fragment and curl dependency are explicit; networking/SSH defaults are builtin hook responsibilities, not implicit renderer fields.

S5: all remaining profile/blank/layout/value differences in this range are directly authored in the committed .tmpl/.fragment snapshots: hostname remains web-01; identity is task-42 (valid model ID) rather than old numeric 42; new tokens shorter but valid; full includes one real script per supported timing, minimal keeps the slot empty (still receives runtime wrapper for stage signals); SSH keys/legacy host metadata omitted from independent assembly fixtures because Task9 owns actual builtin defaults and SSH hook. No implicit old-model field assembly remains.

```diff
-0004   <general>
-0005     <mode>
-0006       <confirm config:type="boolean">false</confirm>
-0007     </mode>
-0008   </general>
+0004   <general><mode><confirm config:type="boolean">false</confirm></mode></general>
```

### Old 10–13; new 6–6

S3: handwritten XML is compact rather than old serializer indentation; default filename is omitted (optional upstream). pre-scripts run before install; a single chrooted=false script stages in /mnt and post-target slot explicitly chroots. Firmware partition fragment and curl dependency are explicit; networking/SSH defaults are builtin hook responsibilities, not implicit renderer fields.

S5: all remaining profile/blank/layout/value differences in this range are directly authored in the committed .tmpl/.fragment snapshots: hostname remains web-01; identity is task-42 (valid model ID) rather than old numeric 42; new tokens shorter but valid; full includes one real script per supported timing, minimal keeps the slot empty (still receives runtime wrapper for stage signals); SSH keys/legacy host metadata omitted from independent assembly fixtures because Task9 owns actual builtin defaults and SSH hook. No implicit old-model field assembly remains.

```diff
-0010     <drive>
-0011       <device>/dev/disk/by-id/wwn-0x5000c500fixed0001</device>
-0012       <use>all</use>
-0013     </drive>
+0006     <drive><device>/dev/sda</device><use>all</use><disklabel>gpt</disklabel><partitions config:type="list"><partition><mount>/boot/efi</mount><size>600M</size><filesystem config:type="symbol">vfat</filesystem><partition_id config:type="integer">259</partition_id></partition><partition><mount>/</mount><size>max</size><filesystem config:type="symbol">btrfs</filesystem></partition></partitions></drive>
```

### Old 15–22; new 8–9

S3: handwritten XML is compact rather than old serializer indentation; default filename is omitted (optional upstream). pre-scripts run before install; a single chrooted=false script stages in /mnt and post-target slot explicitly chroots. Firmware partition fragment and curl dependency are explicit; networking/SSH defaults are builtin hook responsibilities, not implicit renderer fields.

S5: all remaining profile/blank/layout/value differences in this range are directly authored in the committed .tmpl/.fragment snapshots: hostname remains web-01; identity is task-42 (valid model ID) rather than old numeric 42; new tokens shorter but valid; full includes one real script per supported timing, minimal keeps the slot empty (still receives runtime wrapper for stage signals); SSH keys/legacy host metadata omitted from independent assembly fixtures because Task9 owns actual builtin defaults and SSH hook. No implicit old-model field assembly remains.

```diff
-0015   <networking>
-0016     <dns>
-0017       <hostname>web-01</hostname>
-0018     </dns>
-0019     <dhcp_options>
-0020       <dhclient_client_id/>
-0021     </dhcp_options>
-0022   </networking>
+0008   <networking><dns><hostname>web-01</hostname></dns></networking>
+0009   <software><packages config:type="list"><package>curl</package></packages></software>
```

### Old 25–89; new 12–15

S1: fixed global environment and inline signal/run_hook library are removed; flattened snapshot instances and FileScript now own per-instance EnvName/EnvValue, digest/interpreter/timeout/required execution. Task6 inserts one-line wrapper downloads, never copies old inline runtime. Legacy fake digest strings become real SHA-256 fixture digest. Task8 implements/verifies runtime bodies.

S3: handwritten XML is compact rather than old serializer indentation; default filename is omitted (optional upstream). pre-scripts run before install; a single chrooted=false script stages in /mnt and post-target slot explicitly chroots. Firmware partition fragment and curl dependency are explicit; networking/SSH defaults are builtin hook responsibilities, not implicit renderer fields.

S5: all remaining profile/blank/layout/value differences in this range are directly authored in the committed .tmpl/.fragment snapshots: hostname remains web-01; identity is task-42 (valid model ID) rather than old numeric 42; new tokens shorter but valid; full includes one real script per supported timing, minimal keeps the slot empty (still receives runtime wrapper for stage signals); SSH keys/legacy host metadata omitted from independent assembly fixtures because Task9 owns actual builtin defaults and SSH hook. No implicit old-model field assembly remains.

```diff
-0025       <script>
-0026         <filename>lober-pre.sh</filename>
-0027         <interpreter>shell</interpreter>
-0028         <source><![CDATA[
-0029 LOBER_CONTRACT_VERSION=''
-0030 LOBER_TASK_ID='42'
-0031 LOBER_SN='SN-FIXED-0001'
-0032 LOBER_MAC=''
-0033 LOBER_UUID=''
-0034 LOBER_HOSTNAME='web-01'
-0035 LOBER_OS_FAMILY='suse'
-0036 LOBER_OS_VERSION=''
-0037 LOBER_ARCH=''
-0038 LOBER_DISKS=''
-0039 LOBER_DISK_PRIMARY='/dev/disk/by-id/wwn-0x5000c500fixed0001'
-0040 LOBER_NICS=''
-0041 LOBER_RAM_MB=''
-0042 LOBER_CPU_CORES=''
-0043 LOBER_VENDOR=''
-0044 LOBER_PRODUCT=''
-0045 LOBER_BIZ_IP='10.20.0.15'
-0046 LOBER_BIZ_GW=''
-0047 LOBER_BIZ_MASK=''
-0048 LOBER_ARTIFACT_BASE='http://ng.internal/artifacts'
-0049 LOBER_GATEWAY_BASE='http://gw.internal'
-0050 LOBER_RACK=''
-0051 LOBER_ROW=''
-0052 LOBER_DC=''
-0053 # 用户 hook 从这里开始
-0054 GW='http://gw.internal'
-0055 ART='http://ng.internal/artifacts'
-0056 TASK='42'
-0057 TOKEN='install-tok-fixed-0000000000000000'
-0058 SEQ=0
-0059 
-0060 signal() {
-0061   local phase="$1" rc="${2:-0}" log="${3:-/dev/null}" digest="${4:-}"
-0062   SEQ=$((SEQ+1))
-0063   local q="$GW/v1/signal/$TASK/$TOKEN/$phase?rc=$rc&seq=$SEQ"
-0064   if [ -n "$digest" ]; then
-0065     q="$q&digest=$digest"
-0066   fi
-0067   for i in 1 2 3; do
-0068     curl -fsS -m 10 -X POST "$q" \
-0069       -H 'Content-Type: text/plain' --data-binary "@$log" && return 0
-0070     sleep $((i*3))
-0071   done
-0072   logger -t lober "signal failed: $phase rc=$rc"
-0073   return 0
-0074 }
-0075 
-0076 run_hook() {
-0077   local digest="$1" required="$2" timeout_sec="$3"
-0078   curl -fsS -o /tmp/h.sh "$ART/hooks/$digest/script.sh" || return 1
-0079   echo "$digest  /tmp/h.sh" | sha256sum -c - || return 1
-0080   ( set -euo pipefail; timeout "$timeout_sec" bash /tmp/h.sh ) 2>&1 | tee /tmp/h.log
-0081   local rc=${PIPESTATUS[0]}
-0082   signal "hook-report" "$rc" /tmp/h.log "$digest"
-0083   [ "$rc" -eq 0 ] || [ "$required" != "true" ]
-0084 }
-0085 signal "pre-start" 0
-0086 run_hook 'aaaa1111pre' 'true' '300' || exit 1
-0087 signal "pre-done" 0
-0088 ]]></source>
-0089       </script>
+0012       <script><interpreter>shell</interpreter><source><![CDATA[
+0013 set -eu
+0014 echo c2V0IC1ldQp1bWFzayAwNzcKbWtkaXIgLXAgJy90bXAnCmN1cmwgLWZzUyAtLXJldHJ5IDMgLW8gJy90bXAvc2NyaXB0LTIuc2gnICdodHRwOi8vZ3cuaW50ZXJuYWwvdjEvYW5zd2VyL3Rhc2stNDIvaW5zdGFsbC10b2stZml4ZWQvc2NyaXB0cy8yLnNoJwpzaCAnL3RtcC9zY3JpcHQtMi5zaCcK | base64 -d | sh
+0015 ]]></source></script>
```

### Old 92–285; new 18–25

S1: fixed global environment and inline signal/run_hook library are removed; flattened snapshot instances and FileScript now own per-instance EnvName/EnvValue, digest/interpreter/timeout/required execution. Task6 inserts one-line wrapper downloads, never copies old inline runtime. Legacy fake digest strings become real SHA-256 fixture digest. Task8 implements/verifies runtime bodies.

B1: firstboot now stages local slot wrappers and local digest payloads before reboot; systemd invokes the installed native runner. Boot credential no longer enters firstboot; independent local finalize token used. Cleanup follows success and erases cached answers/payloads. Legacy firstboot reused install TOKEN despite writing finalize.token. Unit/marker/local paths change to this explicit contract.

S3: handwritten XML is compact rather than old serializer indentation; default filename is omitted (optional upstream). pre-scripts run before install; a single chrooted=false script stages in /mnt and post-target slot explicitly chroots. Firmware partition fragment and curl dependency are explicit; networking/SSH defaults are builtin hook responsibilities, not implicit renderer fields.

S5: all remaining profile/blank/layout/value differences in this range are directly authored in the committed .tmpl/.fragment snapshots: hostname remains web-01; identity is task-42 (valid model ID) rather than old numeric 42; new tokens shorter but valid; full includes one real script per supported timing, minimal keeps the slot empty (still receives runtime wrapper for stage signals); SSH keys/legacy host metadata omitted from independent assembly fixtures because Task9 owns actual builtin defaults and SSH hook. No implicit old-model field assembly remains.

```diff
-0092       <script>
-0093         <chrooted config:type="boolean">false</chrooted>
-0094         <filename>lober-post-nochroot.sh</filename>
-0095         <interpreter>shell</interpreter>
-0096         <source><![CDATA[
-0097 GW='http://gw.internal'
-0098 ART='http://ng.internal/artifacts'
-0099 TASK='42'
-0100 TOKEN='install-tok-fixed-0000000000000000'
-0101 SEQ=0
-0102 
-0103 signal() {
-0104   local phase="$1" rc="${2:-0}" log="${3:-/dev/null}" digest="${4:-}"
-0105   SEQ=$((SEQ+1))
-0106   local q="$GW/v1/signal/$TASK/$TOKEN/$phase?rc=$rc&seq=$SEQ"
-0107   if [ -n "$digest" ]; then
-0108     q="$q&digest=$digest"
-0109   fi
-0110   for i in 1 2 3; do
-0111     curl -fsS -m 10 -X POST "$q" \
-0112       -H 'Content-Type: text/plain' --data-binary "@$log" && return 0
-0113     sleep $((i*3))
-0114   done
-0115   logger -t lober "signal failed: $phase rc=$rc"
-0116   return 0
-0117 }
-0118 
-0119 run_hook() {
-0120   local digest="$1" required="$2" timeout_sec="$3"
-0121   curl -fsS -o /tmp/h.sh "$ART/hooks/$digest/script.sh" || return 1
-0122   echo "$digest  /tmp/h.sh" | sha256sum -c - || return 1
-0123   ( set -euo pipefail; timeout "$timeout_sec" bash /tmp/h.sh ) 2>&1 | tee /tmp/h.log
-0124   local rc=${PIPESTATUS[0]}
-0125   signal "hook-report" "$rc" /tmp/h.log "$digest"
-0126   [ "$rc" -eq 0 ] || [ "$required" != "true" ]
-0127 }
-0128 run_hook 'bbbb2222nochroot' 'false' '300' || exit 1
-0129 ]]></source>
-0130       </script>
-0131       <script>
-0132         <chrooted config:type="boolean">true</chrooted>
-0133         <filename>lober-post.sh</filename>
-0134         <interpreter>shell</interpreter>
-0135         <source><![CDATA[
-0136 GW='http://gw.internal'
-0137 ART='http://ng.internal/artifacts'
-0138 TASK='42'
-0139 TOKEN='install-tok-fixed-0000000000000000'
-0140 SEQ=0
-0141 
-0142 signal() {
-0143   local phase="$1" rc="${2:-0}" log="${3:-/dev/null}" digest="${4:-}"
-0144   SEQ=$((SEQ+1))
-0145   local q="$GW/v1/signal/$TASK/$TOKEN/$phase?rc=$rc&seq=$SEQ"
-0146   if [ -n "$digest" ]; then
-0147     q="$q&digest=$digest"
-0148   fi
-0149   for i in 1 2 3; do
-0150     curl -fsS -m 10 -X POST "$q" \
-0151       -H 'Content-Type: text/plain' --data-binary "@$log" && return 0
-0152     sleep $((i*3))
-0153   done
-0154   logger -t lober "signal failed: $phase rc=$rc"
-0155   return 0
-0156 }
-0157 
-0158 run_hook() {
-0159   local digest="$1" required="$2" timeout_sec="$3"
-0160   curl -fsS -o /tmp/h.sh "$ART/hooks/$digest/script.sh" || return 1
-0161   echo "$digest  /tmp/h.sh" | sha256sum -c - || return 1
-0162   ( set -euo pipefail; timeout "$timeout_sec" bash /tmp/h.sh ) 2>&1 | tee /tmp/h.log
-0163   local rc=${PIPESTATUS[0]}
-0164   signal "hook-report" "$rc" /tmp/h.log "$digest"
-0165   [ "$rc" -eq 0 ] || [ "$required" != "true" ]
-0166 }
-0167 signal "post-start" 0
-0168 mkdir -p /root/.ssh && chmod 700 /root/.ssh
-0169 cat >> /root/.ssh/authorized_keys <<'LOBER_SSH_AUTHORIZED_KEYS_EOF'
-0170 ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIGZ5ZDhhOWMwZTNiNGY1YzZkN2E4YjljMGUzYjRmNWM2 ops@lober
-0171 LOBER_SSH_AUTHORIZED_KEYS_EOF
-0172 chmod 600 /root/.ssh/authorized_keys
-0173 run_hook 'cccc3333post' 'true' '300' || exit 1
-0174 mkdir -p /var/lib/lober /var/lib/lober/hooks
-0175 
-0176 install -m 600 /dev/null /var/lib/lober/finalize.token
-0177 echo 'finalize-tok-fixed-0000000000000000' > /var/lib/lober/finalize.token
-0178 
-0179 install -m 600 /dev/null /var/lib/lober/params.env
-0180 cat > /var/lib/lober/params.env <<'LOBER_PARAMS_EOF'
-0181 LOBER_CONTRACT_VERSION=''
-0182 LOBER_TASK_ID='42'
-0183 LOBER_SN='SN-FIXED-0001'
-0184 LOBER_MAC=''
-0185 LOBER_UUID=''
-0186 LOBER_HOSTNAME='web-01'
-0187 LOBER_OS_FAMILY='suse'
-0188 LOBER_OS_VERSION=''
-0189 LOBER_ARCH=''
-0190 LOBER_DISKS=''
-0191 LOBER_DISK_PRIMARY='/dev/disk/by-id/wwn-0x5000c500fixed0001'
-0192 LOBER_NICS=''
-0193 LOBER_RAM_MB=''
-0194 LOBER_CPU_CORES=''
-0195 LOBER_VENDOR=''
-0196 LOBER_PRODUCT=''
-0197 LOBER_BIZ_IP='10.20.0.15'
-0198 LOBER_BIZ_GW=''
-0199 LOBER_BIZ_MASK=''
-0200 LOBER_ARTIFACT_BASE='http://ng.internal/artifacts'
-0201 LOBER_GATEWAY_BASE='http://gw.internal'
-0202 LOBER_RACK=''
-0203 LOBER_ROW=''
-0204 LOBER_DC=''
-0205 LOBER_PARAMS_EOF
-0206 
-0207 cat > /var/lib/lober/hooks/manifest <<'LOBER_MANIFEST_EOF'
-0208 010-dddd4444firstboot.sh digest=dddd4444firstboot required=true order=10
-0209 LOBER_MANIFEST_EOF
-0210 
-0211 curl -fsS -o /var/lib/lober/hooks/010-dddd4444firstboot.sh 'http://ng.internal/artifacts/hooks/dddd4444firstboot/script.sh'
-0212 echo 'dddd4444firstboot  /var/lib/lober/hooks/010-dddd4444firstboot.sh' | sha256sum -c -
-0213 chmod 700 /var/lib/lober/hooks/010-dddd4444firstboot.sh
-0214 
-0215 cat > /usr/local/sbin/lober-init.sh <<'LOBER_INIT_EOF'
-0216 #!/bin/bash
-0217 . /var/lib/lober/params.env
-0218 
-0219 GW='http://gw.internal'
-0220 ART='http://ng.internal/artifacts'
-0221 TASK='42'
-0222 TOKEN='install-tok-fixed-0000000000000000'
-0223 SEQ=0
-0224 
-0225 signal() {
-0226   local phase="$1" rc="${2:-0}" log="${3:-/dev/null}" digest="${4:-}"
-0227   SEQ=$((SEQ+1))
-0228   local q="$GW/v1/signal/$TASK/$TOKEN/$phase?rc=$rc&seq=$SEQ"
-0229   if [ -n "$digest" ]; then
-0230     q="$q&digest=$digest"
-0231   fi
-0232   for i in 1 2 3; do
-0233     curl -fsS -m 10 -X POST "$q" \
-0234       -H 'Content-Type: text/plain' --data-binary "@$log" && return 0
-0235     sleep $((i*3))
-0236   done
-0237   logger -t lober "signal failed: $phase rc=$rc"
-0238   return 0
-0239 }
-0240 
-0241 run_hook() {
-0242   local digest="$1" required="$2" timeout_sec="$3"
-0243   curl -fsS -o /tmp/h.sh "$ART/hooks/$digest/script.sh" || return 1
-0244   echo "$digest  /tmp/h.sh" | sha256sum -c - || return 1
-0245   ( set -euo pipefail; timeout "$timeout_sec" bash /tmp/h.sh ) 2>&1 | tee /tmp/h.log
-0246   local rc=${PIPESTATUS[0]}
-0247   signal "hook-report" "$rc" /tmp/h.log "$digest"
-0248   [ "$rc" -eq 0 ] || [ "$required" != "true" ]
-0249 }
-0250 
-0251 signal "booted" 0
-0252 signal "init-start" 0
-0253 
-0254 bash /var/lib/lober/hooks/010-dddd4444firstboot.sh > /tmp/fb-dddd4444firstboot.log 2>&1
-0255 signal "hook-report" "$?" /tmp/fb-dddd4444firstboot.log "dddd4444firstboot"
-0256 
-0257 signal "init-done" 0
-0258 signal "ready" 0
-0259 
-0260 shred -u /var/lib/lober/params.env /var/lib/lober/finalize.token
-0261 rm -f /var/lib/lober/init.pending
-0262 sed -i '/rootpw\|--iscrypted\|inst.ks=/d' /root/anaconda-ks.cfg 2>/dev/null
-0263 LOBER_INIT_EOF
-0264 chmod 755 /usr/local/sbin/lober-init.sh
-0265 
-0266 cat > /etc/systemd/system/lober-init.service <<'LOBER_UNIT_EOF'
-0267 [Unit]
-0268 Description=Lober OS init
-0269 After=network-online.target
-0270 Wants=network-online.target
-0271 ConditionPathExists=/var/lib/lober/init.pending
-0272 [Service]
-0273 Type=oneshot
-0274 ExecStart=/usr/local/sbin/lober-init.sh
-0275 StandardOutput=journal+console
-0276 [Install]
-0277 WantedBy=multi-user.target
-0278 LOBER_UNIT_EOF
-0279 
-0280 systemctl enable lober-init.service
-0281 touch /var/lib/lober/init.pending
-0282 signal "post-done" 0
-0283 signal "rebooting" 0
-0284 ]]></source>
-0285       </script>
+0018       <script><chrooted config:type="boolean">false</chrooted><interpreter>shell</interpreter><source><![CDATA[
+0019 set -eu
+0020 echo c2V0IC1ldQp1bWFzayAwNzcKbWtkaXIgLXAgJy90bXAnCmN1cmwgLWZzUyAtLXJldHJ5IDMgLW8gJy90bXAvc2NyaXB0LTMuc2gnICdodHRwOi8vZ3cuaW50ZXJuYWwvdjEvYW5zd2VyL3Rhc2stNDIvaW5zdGFsbC10b2stZml4ZWQvc2NyaXB0cy8zLnNoJwpzaCAnL3RtcC9zY3JpcHQtMy5zaCcK | base64 -d | sh
+0021 echo c2V0IC1ldQp1bWFzayAwNzcKbWtkaXIgLXAgJy9tbnQvdmFyL2xpYi9sb2JlcicKY3VybCAtZnNTIC0tcmV0cnkgMyAtbyAnL21udC92YXIvbGliL2xvYmVyL3NjcmlwdC00LnNoJyAnaHR0cDovL2d3LmludGVybmFsL3YxL2Fuc3dlci90YXNrLTQyL2luc3RhbGwtdG9rLWZpeGVkL3NjcmlwdHMvNC5zaCcKY2hyb290ICcvbW50JyBzaCAnL3Zhci9saWIvbG9iZXIvc2NyaXB0LTQuc2gnCg== | base64 -d | sh
+0022 echo c2V0IC1ldQp1bWFzayAwNzcKbWtkaXIgLXAgJy9tbnQvdmFyL2xpYi9sb2JlcicKY3VybCAtZnNTIC0tcmV0cnkgMyAtbyAnL21udC92YXIvbGliL2xvYmVyL3NjcmlwdC01LnNoJyAnaHR0cDovL2d3LmludGVybmFsL3YxL2Fuc3dlci90YXNrLTQyL2luc3RhbGwtdG9rLWZpeGVkL3NjcmlwdHMvNS5zaCcKY3VybCAtZnNTIC0tcmV0cnkgMyAtbyAnL21udC92YXIvbGliL2xvYmVyL2hvb2stM2VlMDQ5ZTU5MWY1ZTk5YWU4ZjQ5OGFlMzgxZjQ1OTM2NWMyMjFlNDFlNmI0MmYyYWQ2ZTlhNmE5NzUwNTMyZS5zaCcgJ2h0dHA6Ly9ndy5pbnRlcm5hbC92MS9hbnN3ZXIvdGFzay00Mi9pbnN0YWxsLXRvay1maXhlZC9ob29rcy8zZWUwNDllNTkxZjVlOTlhZThmNDk4YWUzODFmNDU5MzY1YzIyMWU0MWU2YjQyZjJhZDZlOWE2YTk3NTA1MzJlJwo= | base64 -d | sh
+0023 echo c2V0IC1ldQp1bWFzayAwNzcKbWtkaXIgLXAgJy9tbnQvdmFyL2xpYi9sb2JlcicgJy9tbnQvZXRjL3N5c3RlbWQvc3lzdGVtL211bHRpLXVzZXIudGFyZ2V0LndhbnRzJwpwcmludGYgJyVzJyAnZmluYWxpemUtdG9rLWZpeGVkJyA+ICcvbW50L3Zhci9saWIvbG9iZXIvZmluYWxpemUudG9rZW4nCmNhdCA+ICcvbW50L3Zhci9saWIvbG9iZXIvZmlyc3Rib290LnNoJyA8PCdMT0JFUl9GSVJTVEJPT1RfRU9GJwojIS9iaW4vc2gKc2V0IC1ldQpUT0tFTj0kKGNhdCAvdmFyL2xpYi9sb2Jlci9maW5hbGl6ZS50b2tlbikKY3VybCAtZnNTIC1tIDEwIC1YIFBPU1QgJ2h0dHA6Ly9ndy5pbnRlcm5hbC92MS9zaWduYWwvdGFzay00Mi8nIiRUT0tFTiInL2Jvb3RlZCcgLS1kYXRhLWJpbmFyeSAnJyA+L2Rldi9udWxsIDI+JjEgfHwgOgpUT0tFTj0kKGNhdCAvdmFyL2xpYi9sb2Jlci9maW5hbGl6ZS50b2tlbikKY3VybCAtZnNTIC1tIDEwIC1YIFBPU1QgJ2h0dHA6Ly9ndy5pbnRlcm5hbC92MS9zaWduYWwvdGFzay00Mi8nIiRUT0tFTiInL2luaXQtc3RhcnQnIC0tZGF0YS1iaW5hcnkgJycgPi9kZXYvbnVsbCAyPiYxIHx8IDoKc2ggL3Zhci9saWIvbG9iZXIvc2NyaXB0LTUuc2gKc2ggL3Zhci9saWIvbG9iZXIvY2xlYW51cC5zaApMT0JFUl9GSVJTVEJPT1RfRU9GCmNhdCA+ICcvbW50L2V0Yy9zeXN0ZW1kL3N5c3RlbS9sb2Jlci1maXJzdGJvb3Quc2VydmljZScgPDwnTE9CRVJfVU5JVF9FT0YnCltVbml0XQpEZXNjcmlwdGlvbj1Mb2JlciBmaXJzdCBib290CkFmdGVyPW5ldHdvcmstb25saW5lLnRhcmdldApXYW50cz1uZXR3b3JrLW9ubGluZS50YXJnZXQKQ29uZGl0aW9uUGF0aEV4aXN0cz0vdmFyL2xpYi9sb2Jlci9maXJzdGJvb3QucGVuZGluZwpbU2VydmljZV0KVHlwZT1vbmVzaG90CkV4ZWNTdGFydD0vYmluL3NoIC92YXIvbGliL2xvYmVyL2ZpcnN0Ym9vdC5zaApbSW5zdGFsbF0KV2FudGVkQnk9bXVsdGktdXNlci50YXJnZXQKTE9CRVJfVU5JVF9FT0YKbG4gLXNmIC4uL2xvYmVyLWZpcnN0Ym9vdC5zZXJ2aWNlICcvbW50L2V0Yy9zeXN0ZW1kL3N5c3RlbS9tdWx0aS11c2VyLnRhcmdldC53YW50cy9sb2Jlci1maXJzdGJvb3Quc2VydmljZScKdG91Y2ggJy9tbnQvdmFyL2xpYi9sb2Jlci9maXJzdGJvb3QucGVuZGluZycK | base64 -d | sh
+0024 echo c2V0IC1ldQp1bWFzayAwNzcKbWtkaXIgLXAgJy9tbnQvdmFyL2xpYi9sb2JlcicKY2F0ID4gJy9tbnQvdmFyL2xpYi9sb2Jlci9jbGVhbnVwLnNoJyA8PCdMT0JFUl9DTEVBTlVQX0VPRicKIyEvYmluL3NoCnNldCAtZXUKVE9LRU49JChjYXQgL3Zhci9saWIvbG9iZXIvZmluYWxpemUudG9rZW4pCmN1cmwgLWZzUyAtbSAxMCAtWCBQT1NUICdodHRwOi8vZ3cuaW50ZXJuYWwvdjEvc2lnbmFsL3Rhc2stNDIvJyIkVE9LRU4iJy9pbml0LWRvbmUnIC0tZGF0YS1iaW5hcnkgJycgPi9kZXYvbnVsbCAyPiYxIHx8IDoKVE9LRU49JChjYXQgL3Zhci9saWIvbG9iZXIvZmluYWxpemUudG9rZW4pCmN1cmwgLWZzUyAtbSAxMCAtWCBQT1NUICdodHRwOi8vZ3cuaW50ZXJuYWwvdjEvc2lnbmFsL3Rhc2stNDIvJyIkVE9LRU4iJy9yZWFkeScgLS1kYXRhLWJpbmFyeSAnJyA+L2Rldi9udWxsIDI+JjEgfHwgOgpybSAtZiAvdmFyL2xpYi9sb2Jlci9maW5hbGl6ZS50b2tlbiAvdmFyL2xpYi9sb2Jlci9maXJzdGJvb3QucGVuZGluZyAvdmFyL2xpYi9sb2Jlci9zY3JpcHQtKi5zaCAvdmFyL2xpYi9sb2Jlci9ob29rLSogL3Zhci9saWIvbG9iZXIvZmlyc3Rib290LnNoIC92YXIvbGliL2xvYmVyL2NsZWFudXAuc2gKcm0gLWYgL3Jvb3QvYW5hY29uZGEta3MuY2ZnIC9yb290L29yaWdpbmFsLWtzLmNmZyAvdmFyL2xvZy9pbnN0YWxsZXIvYXV0b2luc3RhbGwtdXNlci1kYXRhIC92YXIvbG9nL2luc3RhbGxlci9hdXRvaW5zdGFsbC11c2VyLWRhdGEudHh0IC92YXIvbG9nL2luc3RhbGxlci9jZGViY29uZi9xdWVzdGlvbnMuZGF0IC92YXIvYWRtL2F1dG9pbnN0YWxsL2NhY2hlL2luc3RhbGxlZFN5c3RlbS54bWwKTE9CRVJfQ0xFQU5VUF9FT0YK | base64 -d | sh
+0025 ]]></source></script>
```

## autoyast-minimal-bios.golden

Compared with `autoyast_minimal.golden`; firmware was not split in that old reference.

### Old 4–8; new 4–4

S3: handwritten XML is compact rather than old serializer indentation; default filename is omitted (optional upstream). pre-scripts run before install; a single chrooted=false script stages in /mnt and post-target slot explicitly chroots. Firmware partition fragment and curl dependency are explicit; networking/SSH defaults are builtin hook responsibilities, not implicit renderer fields.

S5: all remaining profile/blank/layout/value differences in this range are directly authored in the committed .tmpl/.fragment snapshots: hostname remains web-01; identity is task-42 (valid model ID) rather than old numeric 42; new tokens shorter but valid; full includes one real script per supported timing, minimal keeps the slot empty (still receives runtime wrapper for stage signals); SSH keys/legacy host metadata omitted from independent assembly fixtures because Task9 owns actual builtin defaults and SSH hook. No implicit old-model field assembly remains.

```diff
-0004   <general>
-0005     <mode>
-0006       <confirm config:type="boolean">false</confirm>
-0007     </mode>
-0008   </general>
+0004   <general><mode><confirm config:type="boolean">false</confirm></mode></general>
```

### Old 10–13; new 6–6

S3: handwritten XML is compact rather than old serializer indentation; default filename is omitted (optional upstream). pre-scripts run before install; a single chrooted=false script stages in /mnt and post-target slot explicitly chroots. Firmware partition fragment and curl dependency are explicit; networking/SSH defaults are builtin hook responsibilities, not implicit renderer fields.

S5: all remaining profile/blank/layout/value differences in this range are directly authored in the committed .tmpl/.fragment snapshots: hostname remains web-01; identity is task-42 (valid model ID) rather than old numeric 42; new tokens shorter but valid; full includes one real script per supported timing, minimal keeps the slot empty (still receives runtime wrapper for stage signals); SSH keys/legacy host metadata omitted from independent assembly fixtures because Task9 owns actual builtin defaults and SSH hook. No implicit old-model field assembly remains.

```diff
-0010     <drive>
-0011       <device>/dev/disk/by-id/wwn-0x5000c500fixed0001</device>
-0012       <use>all</use>
-0013     </drive>
+0006     <drive><device>/dev/sda</device><use>all</use><disklabel>gpt</disklabel><partitions config:type="list"><partition><size>1M</size><partition_id config:type="integer">263</partition_id></partition><partition><mount>/</mount><size>max</size><filesystem config:type="symbol">btrfs</filesystem></partition></partitions></drive>
```

### Old 15–22; new 8–9

S3: handwritten XML is compact rather than old serializer indentation; default filename is omitted (optional upstream). pre-scripts run before install; a single chrooted=false script stages in /mnt and post-target slot explicitly chroots. Firmware partition fragment and curl dependency are explicit; networking/SSH defaults are builtin hook responsibilities, not implicit renderer fields.

S5: all remaining profile/blank/layout/value differences in this range are directly authored in the committed .tmpl/.fragment snapshots: hostname remains web-01; identity is task-42 (valid model ID) rather than old numeric 42; new tokens shorter but valid; full includes one real script per supported timing, minimal keeps the slot empty (still receives runtime wrapper for stage signals); SSH keys/legacy host metadata omitted from independent assembly fixtures because Task9 owns actual builtin defaults and SSH hook. No implicit old-model field assembly remains.

```diff
-0015   <networking>
-0016     <dns>
-0017       <hostname>web-01</hostname>
-0018     </dns>
-0019     <dhcp_options>
-0020       <dhclient_client_id/>
-0021     </dhcp_options>
-0022   </networking>
+0008   <networking><dns><hostname>web-01</hostname></dns></networking>
+0009   <software><packages config:type="list"><package>curl</package></packages></software>
```

### Old 24–23; new 11–16

S3: handwritten XML is compact rather than old serializer indentation; default filename is omitted (optional upstream). pre-scripts run before install; a single chrooted=false script stages in /mnt and post-target slot explicitly chroots. Firmware partition fragment and curl dependency are explicit; networking/SSH defaults are builtin hook responsibilities, not implicit renderer fields.

S5: all remaining profile/blank/layout/value differences in this range are directly authored in the committed .tmpl/.fragment snapshots: hostname remains web-01; identity is task-42 (valid model ID) rather than old numeric 42; new tokens shorter but valid; full includes one real script per supported timing, minimal keeps the slot empty (still receives runtime wrapper for stage signals); SSH keys/legacy host metadata omitted from independent assembly fixtures because Task9 owns actual builtin defaults and SSH hook. No implicit old-model field assembly remains.

```diff
+0011     <pre-scripts config:type="list">
+0012       <script><interpreter>shell</interpreter><source><![CDATA[
+0013 set -eu
+0014 echo c2V0IC1ldQp1bWFzayAwNzcKbWtkaXIgLXAgJy90bXAnCmN1cmwgLWZzUyAtLXJldHJ5IDMgLW8gJy90bXAvc2NyaXB0LTIuc2gnICdodHRwOi8vZ3cuaW50ZXJuYWwvdjEvYW5zd2VyL3Rhc2stNDIvaW5zdGFsbC10b2stZml4ZWQvc2NyaXB0cy8yLnNoJwpzaCAnL3RtcC9zY3JpcHQtMi5zaCcK | base64 -d | sh
+0015 ]]></source></script>
+0016     </pre-scripts>
```

### Old 25–170; new 18–25

S1: fixed global environment and inline signal/run_hook library are removed; flattened snapshot instances and FileScript now own per-instance EnvName/EnvValue, digest/interpreter/timeout/required execution. Task6 inserts one-line wrapper downloads, never copies old inline runtime. Legacy fake digest strings become real SHA-256 fixture digest. Task8 implements/verifies runtime bodies.

B1: firstboot now stages local slot wrappers and local digest payloads before reboot; systemd invokes the installed native runner. Boot credential no longer enters firstboot; independent local finalize token used. Cleanup follows success and erases cached answers/payloads. Legacy firstboot reused install TOKEN despite writing finalize.token. Unit/marker/local paths change to this explicit contract.

S3: handwritten XML is compact rather than old serializer indentation; default filename is omitted (optional upstream). pre-scripts run before install; a single chrooted=false script stages in /mnt and post-target slot explicitly chroots. Firmware partition fragment and curl dependency are explicit; networking/SSH defaults are builtin hook responsibilities, not implicit renderer fields.

S5: all remaining profile/blank/layout/value differences in this range are directly authored in the committed .tmpl/.fragment snapshots: hostname remains web-01; identity is task-42 (valid model ID) rather than old numeric 42; new tokens shorter but valid; full includes one real script per supported timing, minimal keeps the slot empty (still receives runtime wrapper for stage signals); SSH keys/legacy host metadata omitted from independent assembly fixtures because Task9 owns actual builtin defaults and SSH hook. No implicit old-model field assembly remains.

```diff
-0025       <script>
-0026         <chrooted config:type="boolean">true</chrooted>
-0027         <filename>lober-post.sh</filename>
-0028         <interpreter>shell</interpreter>
-0029         <source><![CDATA[
-0030 GW='http://gw.internal'
-0031 ART='http://ng.internal/artifacts'
-0032 TASK='42'
-0033 TOKEN='install-tok-fixed-0000000000000000'
-0034 SEQ=0
-0035 
-0036 signal() {
-0037   local phase="$1" rc="${2:-0}" log="${3:-/dev/null}" digest="${4:-}"
-0038   SEQ=$((SEQ+1))
-0039   local q="$GW/v1/signal/$TASK/$TOKEN/$phase?rc=$rc&seq=$SEQ"
-0040   if [ -n "$digest" ]; then
-0041     q="$q&digest=$digest"
-0042   fi
-0043   for i in 1 2 3; do
-0044     curl -fsS -m 10 -X POST "$q" \
-0045       -H 'Content-Type: text/plain' --data-binary "@$log" && return 0
-0046     sleep $((i*3))
-0047   done
-0048   logger -t lober "signal failed: $phase rc=$rc"
-0049   return 0
-0050 }
-0051 
-0052 run_hook() {
-0053   local digest="$1" required="$2" timeout_sec="$3"
-0054   curl -fsS -o /tmp/h.sh "$ART/hooks/$digest/script.sh" || return 1
-0055   echo "$digest  /tmp/h.sh" | sha256sum -c - || return 1
-0056   ( set -euo pipefail; timeout "$timeout_sec" bash /tmp/h.sh ) 2>&1 | tee /tmp/h.log
-0057   local rc=${PIPESTATUS[0]}
-0058   signal "hook-report" "$rc" /tmp/h.log "$digest"
-0059   [ "$rc" -eq 0 ] || [ "$required" != "true" ]
-0060 }
-0061 signal "post-start" 0
-0062 mkdir -p /root/.ssh && chmod 700 /root/.ssh
-0063 cat >> /root/.ssh/authorized_keys <<'LOBER_SSH_AUTHORIZED_KEYS_EOF'
-0064 ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIGZ5ZDhhOWMwZTNiNGY1YzZkN2E4YjljMGUzYjRmNWM2 ops@lober
-0065 LOBER_SSH_AUTHORIZED_KEYS_EOF
-0066 chmod 600 /root/.ssh/authorized_keys
-0067 mkdir -p /var/lib/lober /var/lib/lober/hooks
-0068 
-0069 install -m 600 /dev/null /var/lib/lober/finalize.token
-0070 echo 'finalize-tok-fixed-0000000000000000' > /var/lib/lober/finalize.token
-0071 
-0072 install -m 600 /dev/null /var/lib/lober/params.env
-0073 cat > /var/lib/lober/params.env <<'LOBER_PARAMS_EOF'
-0074 LOBER_CONTRACT_VERSION=''
-0075 LOBER_TASK_ID='42'
-0076 LOBER_SN='SN-FIXED-0001'
-0077 LOBER_MAC=''
-0078 LOBER_UUID=''
-0079 LOBER_HOSTNAME='web-01'
-0080 LOBER_OS_FAMILY='suse'
-0081 LOBER_OS_VERSION=''
-0082 LOBER_ARCH=''
-0083 LOBER_DISKS=''
-0084 LOBER_DISK_PRIMARY='/dev/disk/by-id/wwn-0x5000c500fixed0001'
-0085 LOBER_NICS=''
-0086 LOBER_RAM_MB=''
-0087 LOBER_CPU_CORES=''
-0088 LOBER_VENDOR=''
-0089 LOBER_PRODUCT=''
-0090 LOBER_BIZ_IP='10.20.0.15'
-0091 LOBER_BIZ_GW=''
-0092 LOBER_BIZ_MASK=''
-0093 LOBER_ARTIFACT_BASE='http://ng.internal/artifacts'
-0094 LOBER_GATEWAY_BASE='http://gw.internal'
-0095 LOBER_RACK=''
-0096 LOBER_ROW=''
-0097 LOBER_DC=''
-0098 LOBER_PARAMS_EOF
-0099 
-0100 cat > /var/lib/lober/hooks/manifest <<'LOBER_MANIFEST_EOF'
-0101 LOBER_MANIFEST_EOF
-0102 
-0103 cat > /usr/local/sbin/lober-init.sh <<'LOBER_INIT_EOF'
-0104 #!/bin/bash
-0105 . /var/lib/lober/params.env
-0106 
-0107 GW='http://gw.internal'
-0108 ART='http://ng.internal/artifacts'
-0109 TASK='42'
-0110 TOKEN='install-tok-fixed-0000000000000000'
-0111 SEQ=0
-0112 
-0113 signal() {
-0114   local phase="$1" rc="${2:-0}" log="${3:-/dev/null}" digest="${4:-}"
-0115   SEQ=$((SEQ+1))
-0116   local q="$GW/v1/signal/$TASK/$TOKEN/$phase?rc=$rc&seq=$SEQ"
-0117   if [ -n "$digest" ]; then
-0118     q="$q&digest=$digest"
-0119   fi
-0120   for i in 1 2 3; do
-0121     curl -fsS -m 10 -X POST "$q" \
-0122       -H 'Content-Type: text/plain' --data-binary "@$log" && return 0
-0123     sleep $((i*3))
-0124   done
-0125   logger -t lober "signal failed: $phase rc=$rc"
-0126   return 0
-0127 }
-0128 
-0129 run_hook() {
-0130   local digest="$1" required="$2" timeout_sec="$3"
-0131   curl -fsS -o /tmp/h.sh "$ART/hooks/$digest/script.sh" || return 1
-0132   echo "$digest  /tmp/h.sh" | sha256sum -c - || return 1
-0133   ( set -euo pipefail; timeout "$timeout_sec" bash /tmp/h.sh ) 2>&1 | tee /tmp/h.log
-0134   local rc=${PIPESTATUS[0]}
-0135   signal "hook-report" "$rc" /tmp/h.log "$digest"
-0136   [ "$rc" -eq 0 ] || [ "$required" != "true" ]
-0137 }
-0138 
-0139 signal "booted" 0
-0140 signal "init-start" 0
-0141 
-0142 signal "init-done" 0
-0143 signal "ready" 0
-0144 
-0145 shred -u /var/lib/lober/params.env /var/lib/lober/finalize.token
-0146 rm -f /var/lib/lober/init.pending
-0147 sed -i '/rootpw\|--iscrypted\|inst.ks=/d' /root/anaconda-ks.cfg 2>/dev/null
-0148 LOBER_INIT_EOF
-0149 chmod 755 /usr/local/sbin/lober-init.sh
-0150 
-0151 cat > /etc/systemd/system/lober-init.service <<'LOBER_UNIT_EOF'
-0152 [Unit]
-0153 Description=Lober OS init
-0154 After=network-online.target
-0155 Wants=network-online.target
-0156 ConditionPathExists=/var/lib/lober/init.pending
-0157 [Service]
-0158 Type=oneshot
-0159 ExecStart=/usr/local/sbin/lober-init.sh
-0160 StandardOutput=journal+console
-0161 [Install]
-0162 WantedBy=multi-user.target
-0163 LOBER_UNIT_EOF
-0164 
-0165 systemctl enable lober-init.service
-0166 touch /var/lib/lober/init.pending
-0167 signal "post-done" 0
-0168 signal "rebooting" 0
-0169 ]]></source>
-0170       </script>
+0018       <script><chrooted config:type="boolean">false</chrooted><interpreter>shell</interpreter><source><![CDATA[
+0019 set -eu
+0020 echo c2V0IC1ldQp1bWFzayAwNzcKbWtkaXIgLXAgJy90bXAnCmN1cmwgLWZzUyAtLXJldHJ5IDMgLW8gJy90bXAvc2NyaXB0LTMuc2gnICdodHRwOi8vZ3cuaW50ZXJuYWwvdjEvYW5zd2VyL3Rhc2stNDIvaW5zdGFsbC10b2stZml4ZWQvc2NyaXB0cy8zLnNoJwpzaCAnL3RtcC9zY3JpcHQtMy5zaCcK | base64 -d | sh
+0021 echo c2V0IC1ldQp1bWFzayAwNzcKbWtkaXIgLXAgJy9tbnQvdmFyL2xpYi9sb2JlcicKY3VybCAtZnNTIC0tcmV0cnkgMyAtbyAnL21udC92YXIvbGliL2xvYmVyL3NjcmlwdC00LnNoJyAnaHR0cDovL2d3LmludGVybmFsL3YxL2Fuc3dlci90YXNrLTQyL2luc3RhbGwtdG9rLWZpeGVkL3NjcmlwdHMvNC5zaCcKY2hyb290ICcvbW50JyBzaCAnL3Zhci9saWIvbG9iZXIvc2NyaXB0LTQuc2gnCg== | base64 -d | sh
+0022 echo c2V0IC1ldQp1bWFzayAwNzcKbWtkaXIgLXAgJy9tbnQvdmFyL2xpYi9sb2JlcicKY3VybCAtZnNTIC0tcmV0cnkgMyAtbyAnL21udC92YXIvbGliL2xvYmVyL3NjcmlwdC01LnNoJyAnaHR0cDovL2d3LmludGVybmFsL3YxL2Fuc3dlci90YXNrLTQyL2luc3RhbGwtdG9rLWZpeGVkL3NjcmlwdHMvNS5zaCcK | base64 -d | sh
+0023 echo c2V0IC1ldQp1bWFzayAwNzcKbWtkaXIgLXAgJy9tbnQvdmFyL2xpYi9sb2JlcicgJy9tbnQvZXRjL3N5c3RlbWQvc3lzdGVtL211bHRpLXVzZXIudGFyZ2V0LndhbnRzJwpwcmludGYgJyVzJyAnZmluYWxpemUtdG9rLWZpeGVkJyA+ICcvbW50L3Zhci9saWIvbG9iZXIvZmluYWxpemUudG9rZW4nCmNhdCA+ICcvbW50L3Zhci9saWIvbG9iZXIvZmlyc3Rib290LnNoJyA8PCdMT0JFUl9GSVJTVEJPT1RfRU9GJwojIS9iaW4vc2gKc2V0IC1ldQpUT0tFTj0kKGNhdCAvdmFyL2xpYi9sb2Jlci9maW5hbGl6ZS50b2tlbikKY3VybCAtZnNTIC1tIDEwIC1YIFBPU1QgJ2h0dHA6Ly9ndy5pbnRlcm5hbC92MS9zaWduYWwvdGFzay00Mi8nIiRUT0tFTiInL2Jvb3RlZCcgLS1kYXRhLWJpbmFyeSAnJyA+L2Rldi9udWxsIDI+JjEgfHwgOgpUT0tFTj0kKGNhdCAvdmFyL2xpYi9sb2Jlci9maW5hbGl6ZS50b2tlbikKY3VybCAtZnNTIC1tIDEwIC1YIFBPU1QgJ2h0dHA6Ly9ndy5pbnRlcm5hbC92MS9zaWduYWwvdGFzay00Mi8nIiRUT0tFTiInL2luaXQtc3RhcnQnIC0tZGF0YS1iaW5hcnkgJycgPi9kZXYvbnVsbCAyPiYxIHx8IDoKc2ggL3Zhci9saWIvbG9iZXIvc2NyaXB0LTUuc2gKc2ggL3Zhci9saWIvbG9iZXIvY2xlYW51cC5zaApMT0JFUl9GSVJTVEJPT1RfRU9GCmNhdCA+ICcvbW50L2V0Yy9zeXN0ZW1kL3N5c3RlbS9sb2Jlci1maXJzdGJvb3Quc2VydmljZScgPDwnTE9CRVJfVU5JVF9FT0YnCltVbml0XQpEZXNjcmlwdGlvbj1Mb2JlciBmaXJzdCBib290CkFmdGVyPW5ldHdvcmstb25saW5lLnRhcmdldApXYW50cz1uZXR3b3JrLW9ubGluZS50YXJnZXQKQ29uZGl0aW9uUGF0aEV4aXN0cz0vdmFyL2xpYi9sb2Jlci9maXJzdGJvb3QucGVuZGluZwpbU2VydmljZV0KVHlwZT1vbmVzaG90CkV4ZWNTdGFydD0vYmluL3NoIC92YXIvbGliL2xvYmVyL2ZpcnN0Ym9vdC5zaApbSW5zdGFsbF0KV2FudGVkQnk9bXVsdGktdXNlci50YXJnZXQKTE9CRVJfVU5JVF9FT0YKbG4gLXNmIC4uL2xvYmVyLWZpcnN0Ym9vdC5zZXJ2aWNlICcvbW50L2V0Yy9zeXN0ZW1kL3N5c3RlbS9tdWx0aS11c2VyLnRhcmdldC53YW50cy9sb2Jlci1maXJzdGJvb3Quc2VydmljZScKdG91Y2ggJy9tbnQvdmFyL2xpYi9sb2Jlci9maXJzdGJvb3QucGVuZGluZycK | base64 -d | sh
+0024 echo c2V0IC1ldQp1bWFzayAwNzcKbWtkaXIgLXAgJy9tbnQvdmFyL2xpYi9sb2JlcicKY2F0ID4gJy9tbnQvdmFyL2xpYi9sb2Jlci9jbGVhbnVwLnNoJyA8PCdMT0JFUl9DTEVBTlVQX0VPRicKIyEvYmluL3NoCnNldCAtZXUKVE9LRU49JChjYXQgL3Zhci9saWIvbG9iZXIvZmluYWxpemUudG9rZW4pCmN1cmwgLWZzUyAtbSAxMCAtWCBQT1NUICdodHRwOi8vZ3cuaW50ZXJuYWwvdjEvc2lnbmFsL3Rhc2stNDIvJyIkVE9LRU4iJy9pbml0LWRvbmUnIC0tZGF0YS1iaW5hcnkgJycgPi9kZXYvbnVsbCAyPiYxIHx8IDoKVE9LRU49JChjYXQgL3Zhci9saWIvbG9iZXIvZmluYWxpemUudG9rZW4pCmN1cmwgLWZzUyAtbSAxMCAtWCBQT1NUICdodHRwOi8vZ3cuaW50ZXJuYWwvdjEvc2lnbmFsL3Rhc2stNDIvJyIkVE9LRU4iJy9yZWFkeScgLS1kYXRhLWJpbmFyeSAnJyA+L2Rldi9udWxsIDI+JjEgfHwgOgpybSAtZiAvdmFyL2xpYi9sb2Jlci9maW5hbGl6ZS50b2tlbiAvdmFyL2xpYi9sb2Jlci9maXJzdGJvb3QucGVuZGluZyAvdmFyL2xpYi9sb2Jlci9zY3JpcHQtKi5zaCAvdmFyL2xpYi9sb2Jlci9ob29rLSogL3Zhci9saWIvbG9iZXIvZmlyc3Rib290LnNoIC92YXIvbGliL2xvYmVyL2NsZWFudXAuc2gKcm0gLWYgL3Jvb3QvYW5hY29uZGEta3MuY2ZnIC9yb290L29yaWdpbmFsLWtzLmNmZyAvdmFyL2xvZy9pbnN0YWxsZXIvYXV0b2luc3RhbGwtdXNlci1kYXRhIC92YXIvbG9nL2luc3RhbGxlci9hdXRvaW5zdGFsbC11c2VyLWRhdGEudHh0IC92YXIvbG9nL2luc3RhbGxlci9jZGViY29uZi9xdWVzdGlvbnMuZGF0IC92YXIvYWRtL2F1dG9pbnN0YWxsL2NhY2hlL2luc3RhbGxlZFN5c3RlbS54bWwKTE9CRVJfQ0xFQU5VUF9FT0YK | base64 -d | sh
+0025 ]]></source></script>
```

## autoyast-minimal-uefi.golden

Compared with `autoyast_minimal.golden`; firmware was not split in that old reference.

### Old 4–8; new 4–4

S3: handwritten XML is compact rather than old serializer indentation; default filename is omitted (optional upstream). pre-scripts run before install; a single chrooted=false script stages in /mnt and post-target slot explicitly chroots. Firmware partition fragment and curl dependency are explicit; networking/SSH defaults are builtin hook responsibilities, not implicit renderer fields.

S5: all remaining profile/blank/layout/value differences in this range are directly authored in the committed .tmpl/.fragment snapshots: hostname remains web-01; identity is task-42 (valid model ID) rather than old numeric 42; new tokens shorter but valid; full includes one real script per supported timing, minimal keeps the slot empty (still receives runtime wrapper for stage signals); SSH keys/legacy host metadata omitted from independent assembly fixtures because Task9 owns actual builtin defaults and SSH hook. No implicit old-model field assembly remains.

```diff
-0004   <general>
-0005     <mode>
-0006       <confirm config:type="boolean">false</confirm>
-0007     </mode>
-0008   </general>
+0004   <general><mode><confirm config:type="boolean">false</confirm></mode></general>
```

### Old 10–13; new 6–6

S3: handwritten XML is compact rather than old serializer indentation; default filename is omitted (optional upstream). pre-scripts run before install; a single chrooted=false script stages in /mnt and post-target slot explicitly chroots. Firmware partition fragment and curl dependency are explicit; networking/SSH defaults are builtin hook responsibilities, not implicit renderer fields.

S5: all remaining profile/blank/layout/value differences in this range are directly authored in the committed .tmpl/.fragment snapshots: hostname remains web-01; identity is task-42 (valid model ID) rather than old numeric 42; new tokens shorter but valid; full includes one real script per supported timing, minimal keeps the slot empty (still receives runtime wrapper for stage signals); SSH keys/legacy host metadata omitted from independent assembly fixtures because Task9 owns actual builtin defaults and SSH hook. No implicit old-model field assembly remains.

```diff
-0010     <drive>
-0011       <device>/dev/disk/by-id/wwn-0x5000c500fixed0001</device>
-0012       <use>all</use>
-0013     </drive>
+0006     <drive><device>/dev/sda</device><use>all</use><disklabel>gpt</disklabel><partitions config:type="list"><partition><mount>/boot/efi</mount><size>600M</size><filesystem config:type="symbol">vfat</filesystem><partition_id config:type="integer">259</partition_id></partition><partition><mount>/</mount><size>max</size><filesystem config:type="symbol">btrfs</filesystem></partition></partitions></drive>
```

### Old 15–22; new 8–9

S3: handwritten XML is compact rather than old serializer indentation; default filename is omitted (optional upstream). pre-scripts run before install; a single chrooted=false script stages in /mnt and post-target slot explicitly chroots. Firmware partition fragment and curl dependency are explicit; networking/SSH defaults are builtin hook responsibilities, not implicit renderer fields.

S5: all remaining profile/blank/layout/value differences in this range are directly authored in the committed .tmpl/.fragment snapshots: hostname remains web-01; identity is task-42 (valid model ID) rather than old numeric 42; new tokens shorter but valid; full includes one real script per supported timing, minimal keeps the slot empty (still receives runtime wrapper for stage signals); SSH keys/legacy host metadata omitted from independent assembly fixtures because Task9 owns actual builtin defaults and SSH hook. No implicit old-model field assembly remains.

```diff
-0015   <networking>
-0016     <dns>
-0017       <hostname>web-01</hostname>
-0018     </dns>
-0019     <dhcp_options>
-0020       <dhclient_client_id/>
-0021     </dhcp_options>
-0022   </networking>
+0008   <networking><dns><hostname>web-01</hostname></dns></networking>
+0009   <software><packages config:type="list"><package>curl</package></packages></software>
```

### Old 24–23; new 11–16

S3: handwritten XML is compact rather than old serializer indentation; default filename is omitted (optional upstream). pre-scripts run before install; a single chrooted=false script stages in /mnt and post-target slot explicitly chroots. Firmware partition fragment and curl dependency are explicit; networking/SSH defaults are builtin hook responsibilities, not implicit renderer fields.

S5: all remaining profile/blank/layout/value differences in this range are directly authored in the committed .tmpl/.fragment snapshots: hostname remains web-01; identity is task-42 (valid model ID) rather than old numeric 42; new tokens shorter but valid; full includes one real script per supported timing, minimal keeps the slot empty (still receives runtime wrapper for stage signals); SSH keys/legacy host metadata omitted from independent assembly fixtures because Task9 owns actual builtin defaults and SSH hook. No implicit old-model field assembly remains.

```diff
+0011     <pre-scripts config:type="list">
+0012       <script><interpreter>shell</interpreter><source><![CDATA[
+0013 set -eu
+0014 echo c2V0IC1ldQp1bWFzayAwNzcKbWtkaXIgLXAgJy90bXAnCmN1cmwgLWZzUyAtLXJldHJ5IDMgLW8gJy90bXAvc2NyaXB0LTIuc2gnICdodHRwOi8vZ3cuaW50ZXJuYWwvdjEvYW5zd2VyL3Rhc2stNDIvaW5zdGFsbC10b2stZml4ZWQvc2NyaXB0cy8yLnNoJwpzaCAnL3RtcC9zY3JpcHQtMi5zaCcK | base64 -d | sh
+0015 ]]></source></script>
+0016     </pre-scripts>
```

### Old 25–170; new 18–25

S1: fixed global environment and inline signal/run_hook library are removed; flattened snapshot instances and FileScript now own per-instance EnvName/EnvValue, digest/interpreter/timeout/required execution. Task6 inserts one-line wrapper downloads, never copies old inline runtime. Legacy fake digest strings become real SHA-256 fixture digest. Task8 implements/verifies runtime bodies.

B1: firstboot now stages local slot wrappers and local digest payloads before reboot; systemd invokes the installed native runner. Boot credential no longer enters firstboot; independent local finalize token used. Cleanup follows success and erases cached answers/payloads. Legacy firstboot reused install TOKEN despite writing finalize.token. Unit/marker/local paths change to this explicit contract.

S3: handwritten XML is compact rather than old serializer indentation; default filename is omitted (optional upstream). pre-scripts run before install; a single chrooted=false script stages in /mnt and post-target slot explicitly chroots. Firmware partition fragment and curl dependency are explicit; networking/SSH defaults are builtin hook responsibilities, not implicit renderer fields.

S5: all remaining profile/blank/layout/value differences in this range are directly authored in the committed .tmpl/.fragment snapshots: hostname remains web-01; identity is task-42 (valid model ID) rather than old numeric 42; new tokens shorter but valid; full includes one real script per supported timing, minimal keeps the slot empty (still receives runtime wrapper for stage signals); SSH keys/legacy host metadata omitted from independent assembly fixtures because Task9 owns actual builtin defaults and SSH hook. No implicit old-model field assembly remains.

```diff
-0025       <script>
-0026         <chrooted config:type="boolean">true</chrooted>
-0027         <filename>lober-post.sh</filename>
-0028         <interpreter>shell</interpreter>
-0029         <source><![CDATA[
-0030 GW='http://gw.internal'
-0031 ART='http://ng.internal/artifacts'
-0032 TASK='42'
-0033 TOKEN='install-tok-fixed-0000000000000000'
-0034 SEQ=0
-0035 
-0036 signal() {
-0037   local phase="$1" rc="${2:-0}" log="${3:-/dev/null}" digest="${4:-}"
-0038   SEQ=$((SEQ+1))
-0039   local q="$GW/v1/signal/$TASK/$TOKEN/$phase?rc=$rc&seq=$SEQ"
-0040   if [ -n "$digest" ]; then
-0041     q="$q&digest=$digest"
-0042   fi
-0043   for i in 1 2 3; do
-0044     curl -fsS -m 10 -X POST "$q" \
-0045       -H 'Content-Type: text/plain' --data-binary "@$log" && return 0
-0046     sleep $((i*3))
-0047   done
-0048   logger -t lober "signal failed: $phase rc=$rc"
-0049   return 0
-0050 }
-0051 
-0052 run_hook() {
-0053   local digest="$1" required="$2" timeout_sec="$3"
-0054   curl -fsS -o /tmp/h.sh "$ART/hooks/$digest/script.sh" || return 1
-0055   echo "$digest  /tmp/h.sh" | sha256sum -c - || return 1
-0056   ( set -euo pipefail; timeout "$timeout_sec" bash /tmp/h.sh ) 2>&1 | tee /tmp/h.log
-0057   local rc=${PIPESTATUS[0]}
-0058   signal "hook-report" "$rc" /tmp/h.log "$digest"
-0059   [ "$rc" -eq 0 ] || [ "$required" != "true" ]
-0060 }
-0061 signal "post-start" 0
-0062 mkdir -p /root/.ssh && chmod 700 /root/.ssh
-0063 cat >> /root/.ssh/authorized_keys <<'LOBER_SSH_AUTHORIZED_KEYS_EOF'
-0064 ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIGZ5ZDhhOWMwZTNiNGY1YzZkN2E4YjljMGUzYjRmNWM2 ops@lober
-0065 LOBER_SSH_AUTHORIZED_KEYS_EOF
-0066 chmod 600 /root/.ssh/authorized_keys
-0067 mkdir -p /var/lib/lober /var/lib/lober/hooks
-0068 
-0069 install -m 600 /dev/null /var/lib/lober/finalize.token
-0070 echo 'finalize-tok-fixed-0000000000000000' > /var/lib/lober/finalize.token
-0071 
-0072 install -m 600 /dev/null /var/lib/lober/params.env
-0073 cat > /var/lib/lober/params.env <<'LOBER_PARAMS_EOF'
-0074 LOBER_CONTRACT_VERSION=''
-0075 LOBER_TASK_ID='42'
-0076 LOBER_SN='SN-FIXED-0001'
-0077 LOBER_MAC=''
-0078 LOBER_UUID=''
-0079 LOBER_HOSTNAME='web-01'
-0080 LOBER_OS_FAMILY='suse'
-0081 LOBER_OS_VERSION=''
-0082 LOBER_ARCH=''
-0083 LOBER_DISKS=''
-0084 LOBER_DISK_PRIMARY='/dev/disk/by-id/wwn-0x5000c500fixed0001'
-0085 LOBER_NICS=''
-0086 LOBER_RAM_MB=''
-0087 LOBER_CPU_CORES=''
-0088 LOBER_VENDOR=''
-0089 LOBER_PRODUCT=''
-0090 LOBER_BIZ_IP='10.20.0.15'
-0091 LOBER_BIZ_GW=''
-0092 LOBER_BIZ_MASK=''
-0093 LOBER_ARTIFACT_BASE='http://ng.internal/artifacts'
-0094 LOBER_GATEWAY_BASE='http://gw.internal'
-0095 LOBER_RACK=''
-0096 LOBER_ROW=''
-0097 LOBER_DC=''
-0098 LOBER_PARAMS_EOF
-0099 
-0100 cat > /var/lib/lober/hooks/manifest <<'LOBER_MANIFEST_EOF'
-0101 LOBER_MANIFEST_EOF
-0102 
-0103 cat > /usr/local/sbin/lober-init.sh <<'LOBER_INIT_EOF'
-0104 #!/bin/bash
-0105 . /var/lib/lober/params.env
-0106 
-0107 GW='http://gw.internal'
-0108 ART='http://ng.internal/artifacts'
-0109 TASK='42'
-0110 TOKEN='install-tok-fixed-0000000000000000'
-0111 SEQ=0
-0112 
-0113 signal() {
-0114   local phase="$1" rc="${2:-0}" log="${3:-/dev/null}" digest="${4:-}"
-0115   SEQ=$((SEQ+1))
-0116   local q="$GW/v1/signal/$TASK/$TOKEN/$phase?rc=$rc&seq=$SEQ"
-0117   if [ -n "$digest" ]; then
-0118     q="$q&digest=$digest"
-0119   fi
-0120   for i in 1 2 3; do
-0121     curl -fsS -m 10 -X POST "$q" \
-0122       -H 'Content-Type: text/plain' --data-binary "@$log" && return 0
-0123     sleep $((i*3))
-0124   done
-0125   logger -t lober "signal failed: $phase rc=$rc"
-0126   return 0
-0127 }
-0128 
-0129 run_hook() {
-0130   local digest="$1" required="$2" timeout_sec="$3"
-0131   curl -fsS -o /tmp/h.sh "$ART/hooks/$digest/script.sh" || return 1
-0132   echo "$digest  /tmp/h.sh" | sha256sum -c - || return 1
-0133   ( set -euo pipefail; timeout "$timeout_sec" bash /tmp/h.sh ) 2>&1 | tee /tmp/h.log
-0134   local rc=${PIPESTATUS[0]}
-0135   signal "hook-report" "$rc" /tmp/h.log "$digest"
-0136   [ "$rc" -eq 0 ] || [ "$required" != "true" ]
-0137 }
-0138 
-0139 signal "booted" 0
-0140 signal "init-start" 0
-0141 
-0142 signal "init-done" 0
-0143 signal "ready" 0
-0144 
-0145 shred -u /var/lib/lober/params.env /var/lib/lober/finalize.token
-0146 rm -f /var/lib/lober/init.pending
-0147 sed -i '/rootpw\|--iscrypted\|inst.ks=/d' /root/anaconda-ks.cfg 2>/dev/null
-0148 LOBER_INIT_EOF
-0149 chmod 755 /usr/local/sbin/lober-init.sh
-0150 
-0151 cat > /etc/systemd/system/lober-init.service <<'LOBER_UNIT_EOF'
-0152 [Unit]
-0153 Description=Lober OS init
-0154 After=network-online.target
-0155 Wants=network-online.target
-0156 ConditionPathExists=/var/lib/lober/init.pending
-0157 [Service]
-0158 Type=oneshot
-0159 ExecStart=/usr/local/sbin/lober-init.sh
-0160 StandardOutput=journal+console
-0161 [Install]
-0162 WantedBy=multi-user.target
-0163 LOBER_UNIT_EOF
-0164 
-0165 systemctl enable lober-init.service
-0166 touch /var/lib/lober/init.pending
-0167 signal "post-done" 0
-0168 signal "rebooting" 0
-0169 ]]></source>
-0170       </script>
+0018       <script><chrooted config:type="boolean">false</chrooted><interpreter>shell</interpreter><source><![CDATA[
+0019 set -eu
+0020 echo c2V0IC1ldQp1bWFzayAwNzcKbWtkaXIgLXAgJy90bXAnCmN1cmwgLWZzUyAtLXJldHJ5IDMgLW8gJy90bXAvc2NyaXB0LTMuc2gnICdodHRwOi8vZ3cuaW50ZXJuYWwvdjEvYW5zd2VyL3Rhc2stNDIvaW5zdGFsbC10b2stZml4ZWQvc2NyaXB0cy8zLnNoJwpzaCAnL3RtcC9zY3JpcHQtMy5zaCcK | base64 -d | sh
+0021 echo c2V0IC1ldQp1bWFzayAwNzcKbWtkaXIgLXAgJy9tbnQvdmFyL2xpYi9sb2JlcicKY3VybCAtZnNTIC0tcmV0cnkgMyAtbyAnL21udC92YXIvbGliL2xvYmVyL3NjcmlwdC00LnNoJyAnaHR0cDovL2d3LmludGVybmFsL3YxL2Fuc3dlci90YXNrLTQyL2luc3RhbGwtdG9rLWZpeGVkL3NjcmlwdHMvNC5zaCcKY2hyb290ICcvbW50JyBzaCAnL3Zhci9saWIvbG9iZXIvc2NyaXB0LTQuc2gnCg== | base64 -d | sh
+0022 echo c2V0IC1ldQp1bWFzayAwNzcKbWtkaXIgLXAgJy9tbnQvdmFyL2xpYi9sb2JlcicKY3VybCAtZnNTIC0tcmV0cnkgMyAtbyAnL21udC92YXIvbGliL2xvYmVyL3NjcmlwdC01LnNoJyAnaHR0cDovL2d3LmludGVybmFsL3YxL2Fuc3dlci90YXNrLTQyL2luc3RhbGwtdG9rLWZpeGVkL3NjcmlwdHMvNS5zaCcK | base64 -d | sh
+0023 echo c2V0IC1ldQp1bWFzayAwNzcKbWtkaXIgLXAgJy9tbnQvdmFyL2xpYi9sb2JlcicgJy9tbnQvZXRjL3N5c3RlbWQvc3lzdGVtL211bHRpLXVzZXIudGFyZ2V0LndhbnRzJwpwcmludGYgJyVzJyAnZmluYWxpemUtdG9rLWZpeGVkJyA+ICcvbW50L3Zhci9saWIvbG9iZXIvZmluYWxpemUudG9rZW4nCmNhdCA+ICcvbW50L3Zhci9saWIvbG9iZXIvZmlyc3Rib290LnNoJyA8PCdMT0JFUl9GSVJTVEJPT1RfRU9GJwojIS9iaW4vc2gKc2V0IC1ldQpUT0tFTj0kKGNhdCAvdmFyL2xpYi9sb2Jlci9maW5hbGl6ZS50b2tlbikKY3VybCAtZnNTIC1tIDEwIC1YIFBPU1QgJ2h0dHA6Ly9ndy5pbnRlcm5hbC92MS9zaWduYWwvdGFzay00Mi8nIiRUT0tFTiInL2Jvb3RlZCcgLS1kYXRhLWJpbmFyeSAnJyA+L2Rldi9udWxsIDI+JjEgfHwgOgpUT0tFTj0kKGNhdCAvdmFyL2xpYi9sb2Jlci9maW5hbGl6ZS50b2tlbikKY3VybCAtZnNTIC1tIDEwIC1YIFBPU1QgJ2h0dHA6Ly9ndy5pbnRlcm5hbC92MS9zaWduYWwvdGFzay00Mi8nIiRUT0tFTiInL2luaXQtc3RhcnQnIC0tZGF0YS1iaW5hcnkgJycgPi9kZXYvbnVsbCAyPiYxIHx8IDoKc2ggL3Zhci9saWIvbG9iZXIvc2NyaXB0LTUuc2gKc2ggL3Zhci9saWIvbG9iZXIvY2xlYW51cC5zaApMT0JFUl9GSVJTVEJPT1RfRU9GCmNhdCA+ICcvbW50L2V0Yy9zeXN0ZW1kL3N5c3RlbS9sb2Jlci1maXJzdGJvb3Quc2VydmljZScgPDwnTE9CRVJfVU5JVF9FT0YnCltVbml0XQpEZXNjcmlwdGlvbj1Mb2JlciBmaXJzdCBib290CkFmdGVyPW5ldHdvcmstb25saW5lLnRhcmdldApXYW50cz1uZXR3b3JrLW9ubGluZS50YXJnZXQKQ29uZGl0aW9uUGF0aEV4aXN0cz0vdmFyL2xpYi9sb2Jlci9maXJzdGJvb3QucGVuZGluZwpbU2VydmljZV0KVHlwZT1vbmVzaG90CkV4ZWNTdGFydD0vYmluL3NoIC92YXIvbGliL2xvYmVyL2ZpcnN0Ym9vdC5zaApbSW5zdGFsbF0KV2FudGVkQnk9bXVsdGktdXNlci50YXJnZXQKTE9CRVJfVU5JVF9FT0YKbG4gLXNmIC4uL2xvYmVyLWZpcnN0Ym9vdC5zZXJ2aWNlICcvbW50L2V0Yy9zeXN0ZW1kL3N5c3RlbS9tdWx0aS11c2VyLnRhcmdldC53YW50cy9sb2Jlci1maXJzdGJvb3Quc2VydmljZScKdG91Y2ggJy9tbnQvdmFyL2xpYi9sb2Jlci9maXJzdGJvb3QucGVuZGluZycK | base64 -d | sh
+0024 echo c2V0IC1ldQp1bWFzayAwNzcKbWtkaXIgLXAgJy9tbnQvdmFyL2xpYi9sb2JlcicKY2F0ID4gJy9tbnQvdmFyL2xpYi9sb2Jlci9jbGVhbnVwLnNoJyA8PCdMT0JFUl9DTEVBTlVQX0VPRicKIyEvYmluL3NoCnNldCAtZXUKVE9LRU49JChjYXQgL3Zhci9saWIvbG9iZXIvZmluYWxpemUudG9rZW4pCmN1cmwgLWZzUyAtbSAxMCAtWCBQT1NUICdodHRwOi8vZ3cuaW50ZXJuYWwvdjEvc2lnbmFsL3Rhc2stNDIvJyIkVE9LRU4iJy9pbml0LWRvbmUnIC0tZGF0YS1iaW5hcnkgJycgPi9kZXYvbnVsbCAyPiYxIHx8IDoKVE9LRU49JChjYXQgL3Zhci9saWIvbG9iZXIvZmluYWxpemUudG9rZW4pCmN1cmwgLWZzUyAtbSAxMCAtWCBQT1NUICdodHRwOi8vZ3cuaW50ZXJuYWwvdjEvc2lnbmFsL3Rhc2stNDIvJyIkVE9LRU4iJy9yZWFkeScgLS1kYXRhLWJpbmFyeSAnJyA+L2Rldi9udWxsIDI+JjEgfHwgOgpybSAtZiAvdmFyL2xpYi9sb2Jlci9maW5hbGl6ZS50b2tlbiAvdmFyL2xpYi9sb2Jlci9maXJzdGJvb3QucGVuZGluZyAvdmFyL2xpYi9sb2Jlci9zY3JpcHQtKi5zaCAvdmFyL2xpYi9sb2Jlci9ob29rLSogL3Zhci9saWIvbG9iZXIvZmlyc3Rib290LnNoIC92YXIvbGliL2xvYmVyL2NsZWFudXAuc2gKcm0gLWYgL3Jvb3QvYW5hY29uZGEta3MuY2ZnIC9yb290L29yaWdpbmFsLWtzLmNmZyAvdmFyL2xvZy9pbnN0YWxsZXIvYXV0b2luc3RhbGwtdXNlci1kYXRhIC92YXIvbG9nL2luc3RhbGxlci9hdXRvaW5zdGFsbC11c2VyLWRhdGEudHh0IC92YXIvbG9nL2luc3RhbGxlci9jZGViY29uZi9xdWVzdGlvbnMuZGF0IC92YXIvYWRtL2F1dG9pbnN0YWxsL2NhY2hlL2luc3RhbGxlZFN5c3RlbS54bWwKTE9CRVJfQ0xFQU5VUF9FT0YK | base64 -d | sh
+0025 ]]></source></script>
```

## unattend-full-bios.golden

Compared with `unattend_full.golden`; firmware was not split in that old reference.

### Old 2–2; new 2–2

B4: removes old marker-only concurrent FirstLogonCommands and finalize-token-as-AdministratorPassword. One actual oobeSystem cmd && chain: compact -File firstboot.ps1, local cmd wrapper, successful -File cleanup.ps1. Concrete native UTF8-BOM PS files are generated by windowsPartScript and staged pre-reboot by FileAux; no whole EncodedCommand in this field, which has maximum 1024 characters (fixtures 304). PS executes only on installed Windows; WinPE pre wrapper injected by FileAux. SystemDrive resolves target volume. Firmware split has BIOS active NTFS versus UEFI ESP/MSR/NTFS. Login prerequisite and Windows Server scope remain downstream builtin/real-machine obligations; no AutoLogon secret is fabricated.

S5: all remaining profile/blank/layout/value differences in this range are directly authored in the committed .tmpl/.fragment snapshots: hostname remains web-01; identity is task-42 (valid model ID) rather than old numeric 42; new tokens shorter but valid; full includes one real script per supported timing, minimal keeps the slot empty (still receives runtime wrapper for stage signals); SSH keys/legacy host metadata omitted from independent assembly fixtures because Task9 owns actual builtin defaults and SSH hook. No implicit old-model field assembly remains.

```diff
-0002 <unattend xmlns="urn:schemas-microsoft-com:unattend">
+0002 <unattend xmlns="urn:schemas-microsoft-com:unattend" xmlns:wcm="http://schemas.microsoft.com/WMIConfig/2002/State">
```

### Old 6–16; new 6–6

B4: removes old marker-only concurrent FirstLogonCommands and finalize-token-as-AdministratorPassword. One actual oobeSystem cmd && chain: compact -File firstboot.ps1, local cmd wrapper, successful -File cleanup.ps1. Concrete native UTF8-BOM PS files are generated by windowsPartScript and staged pre-reboot by FileAux; no whole EncodedCommand in this field, which has maximum 1024 characters (fixtures 304). PS executes only on installed Windows; WinPE pre wrapper injected by FileAux. SystemDrive resolves target volume. Firmware split has BIOS active NTFS versus UEFI ESP/MSR/NTFS. Login prerequisite and Windows Server scope remain downstream builtin/real-machine obligations; no AutoLogon secret is fabricated.

S5: all remaining profile/blank/layout/value differences in this range are directly authored in the committed .tmpl/.fragment snapshots: hostname remains web-01; identity is task-42 (valid model ID) rather than old numeric 42; new tokens shorter but valid; full includes one real script per supported timing, minimal keeps the slot empty (still receives runtime wrapper for stage signals); SSH keys/legacy host metadata omitted from independent assembly fixtures because Task9 owns actual builtin defaults and SSH hook. No implicit old-model field assembly remains.

```diff
-0006         <Disk wcm:action="add" xmlns:wcm="http://schemas.microsoft.com/WMIConfig/2002/State">
-0007           <DiskID>0</DiskID>
-0008           <WillWipeDisk>true</WillWipeDisk>
-0009           <CreatePartitions>
-0010             <CreatePartition wcm:action="add">
-0011               <Order>1</Order>
-0012               <Type>Primary</Type>
-0013               <Extend>true</Extend>
-0014             </CreatePartition>
-0015           </CreatePartitions>
-0016         </Disk>
+0006         <Disk wcm:action="add"><DiskID>0</DiskID><WillWipeDisk>true</WillWipeDisk><CreatePartitions><CreatePartition wcm:action="add"><Order>1</Order><Type>Primary</Type><Extend>true</Extend></CreatePartition></CreatePartitions><ModifyPartitions><ModifyPartition wcm:action="add"><Order>1</Order><PartitionID>1</PartitionID><Format>NTFS</Format><Active>true</Active></ModifyPartition></ModifyPartitions></Disk>
```

### Old 18–40; new 8–11

B4: removes old marker-only concurrent FirstLogonCommands and finalize-token-as-AdministratorPassword. One actual oobeSystem cmd && chain: compact -File firstboot.ps1, local cmd wrapper, successful -File cleanup.ps1. Concrete native UTF8-BOM PS files are generated by windowsPartScript and staged pre-reboot by FileAux; no whole EncodedCommand in this field, which has maximum 1024 characters (fixtures 304). PS executes only on installed Windows; WinPE pre wrapper injected by FileAux. SystemDrive resolves target volume. Firmware split has BIOS active NTFS versus UEFI ESP/MSR/NTFS. Login prerequisite and Windows Server scope remain downstream builtin/real-machine obligations; no AutoLogon secret is fabricated.

S5: all remaining profile/blank/layout/value differences in this range are directly authored in the committed .tmpl/.fragment snapshots: hostname remains web-01; identity is task-42 (valid model ID) rather than old numeric 42; new tokens shorter but valid; full includes one real script per supported timing, minimal keeps the slot empty (still receives runtime wrapper for stage signals); SSH keys/legacy host metadata omitted from independent assembly fixtures because Task9 owns actual builtin defaults and SSH hook. No implicit old-model field assembly remains.

```diff
-0018       <ImageInstall>
-0019         <OSImage>
-0020           <InstallFrom>
-0021             <MetaData wcm:action="add" xmlns:wcm="http://schemas.microsoft.com/WMIConfig/2002/State">
-0022               <Key>/IMAGE/NAME</Key>
-0023               <Value>Windows Server 2022</Value>
-0024             </MetaData>
-0025           </InstallFrom>
-0026           <InstallTo>
-0027             <DiskID>0</DiskID>
-0028             <PartitionID>1</PartitionID>
-0029           </InstallTo>
-0030         </OSImage>
-0031       </ImageInstall>
-0032       <UserData>
-0033         <AcceptEula>true</AcceptEula>
-0034       </UserData>
-0035     </component>
-0036     <component name="Microsoft-Windows-International-Core-WinPE" processorArchitecture="amd64" publicKeyToken="31bf3856ad364e35" language="neutral" versionScope="nonSxS">
-0037       <InputLocale>en-US</InputLocale>
-0038       <SystemLocale>en-US</SystemLocale>
-0039       <UILanguage>en-US</UILanguage>
-0040       <UserLocale>en-US</UserLocale>
+0008       <UserData><AcceptEula>true</AcceptEula></UserData>
+0009       <RunSynchronous>
+0010         <RunSynchronousCommand wcm:action="add"><Order>1</Order><Path>cmd /d /c X:/lober/script-2.cmd</Path></RunSynchronousCommand>
+0011       </RunSynchronous>
```

### Old 45–45; new 16–16

B4: removes old marker-only concurrent FirstLogonCommands and finalize-token-as-AdministratorPassword. One actual oobeSystem cmd && chain: compact -File firstboot.ps1, local cmd wrapper, successful -File cleanup.ps1. Concrete native UTF8-BOM PS files are generated by windowsPartScript and staged pre-reboot by FileAux; no whole EncodedCommand in this field, which has maximum 1024 characters (fixtures 304). PS executes only on installed Windows; WinPE pre wrapper injected by FileAux. SystemDrive resolves target volume. Firmware split has BIOS active NTFS versus UEFI ESP/MSR/NTFS. Login prerequisite and Windows Server scope remain downstream builtin/real-machine obligations; no AutoLogon secret is fabricated.

S5: all remaining profile/blank/layout/value differences in this range are directly authored in the committed .tmpl/.fragment snapshots: hostname remains web-01; identity is task-42 (valid model ID) rather than old numeric 42; new tokens shorter but valid; full includes one real script per supported timing, minimal keeps the slot empty (still receives runtime wrapper for stage signals); SSH keys/legacy host metadata omitted from independent assembly fixtures because Task9 owns actual builtin defaults and SSH hook. No implicit old-model field assembly remains.

```diff
-0045       <ComputerName>win-01</ComputerName>
+0016       <ComputerName>web-01</ComputerName>
```

### Old 50–60; new 21–20

B1: firstboot now stages local slot wrappers and local digest payloads before reboot; systemd invokes the installed native runner. Boot credential no longer enters firstboot; independent local finalize token used. Cleanup follows success and erases cached answers/payloads. Legacy firstboot reused install TOKEN despite writing finalize.token. Unit/marker/local paths change to this explicit contract.

B4: removes old marker-only concurrent FirstLogonCommands and finalize-token-as-AdministratorPassword. One actual oobeSystem cmd && chain: compact -File firstboot.ps1, local cmd wrapper, successful -File cleanup.ps1. Concrete native UTF8-BOM PS files are generated by windowsPartScript and staged pre-reboot by FileAux; no whole EncodedCommand in this field, which has maximum 1024 characters (fixtures 304). PS executes only on installed Windows; WinPE pre wrapper injected by FileAux. SystemDrive resolves target volume. Firmware split has BIOS active NTFS versus UEFI ESP/MSR/NTFS. Login prerequisite and Windows Server scope remain downstream builtin/real-machine obligations; no AutoLogon secret is fabricated.

S5: all remaining profile/blank/layout/value differences in this range are directly authored in the committed .tmpl/.fragment snapshots: hostname remains web-01; identity is task-42 (valid model ID) rather than old numeric 42; new tokens shorter but valid; full includes one real script per supported timing, minimal keeps the slot empty (still receives runtime wrapper for stage signals); SSH keys/legacy host metadata omitted from independent assembly fixtures because Task9 owns actual builtin defaults and SSH hook. No implicit old-model field assembly remains.

```diff
-0050       <UserAccounts>
-0051         <AdministratorPassword>
-0052           <Value>finalize-tok-win-fixed</Value>
-0053           <PlainText>true</PlainText>
-0054         </AdministratorPassword>
-0055       </UserAccounts>
-0056       <OOBE>
-0057         <HideEULAPage>true</HideEULAPage>
-0058         <NetworkLocation>Work</NetworkLocation>
-0059         <SkipUserOOBE>true</SkipUserOOBE>
-0060       </OOBE>
```

### Old 62–62; new 22–22

B4: removes old marker-only concurrent FirstLogonCommands and finalize-token-as-AdministratorPassword. One actual oobeSystem cmd && chain: compact -File firstboot.ps1, local cmd wrapper, successful -File cleanup.ps1. Concrete native UTF8-BOM PS files are generated by windowsPartScript and staged pre-reboot by FileAux; no whole EncodedCommand in this field, which has maximum 1024 characters (fixtures 304). PS executes only on installed Windows; WinPE pre wrapper injected by FileAux. SystemDrive resolves target volume. Firmware split has BIOS active NTFS versus UEFI ESP/MSR/NTFS. Login prerequisite and Windows Server scope remain downstream builtin/real-machine obligations; no AutoLogon secret is fabricated.

S5: all remaining profile/blank/layout/value differences in this range are directly authored in the committed .tmpl/.fragment snapshots: hostname remains web-01; identity is task-42 (valid model ID) rather than old numeric 42; new tokens shorter but valid; full includes one real script per supported timing, minimal keeps the slot empty (still receives runtime wrapper for stage signals); SSH keys/legacy host metadata omitted from independent assembly fixtures because Task9 owns actual builtin defaults and SSH hook. No implicit old-model field assembly remains.

```diff
-0062         <SynchronousCommand wcm:action="add" xmlns:wcm="http://schemas.microsoft.com/WMIConfig/2002/State">
+0022         <SynchronousCommand wcm:action="add">
```

### Old 64–70; new 24–25

B4: removes old marker-only concurrent FirstLogonCommands and finalize-token-as-AdministratorPassword. One actual oobeSystem cmd && chain: compact -File firstboot.ps1, local cmd wrapper, successful -File cleanup.ps1. Concrete native UTF8-BOM PS files are generated by windowsPartScript and staged pre-reboot by FileAux; no whole EncodedCommand in this field, which has maximum 1024 characters (fixtures 304). PS executes only on installed Windows; WinPE pre wrapper injected by FileAux. SystemDrive resolves target volume. Firmware split has BIOS active NTFS versus UEFI ESP/MSR/NTFS. Login prerequisite and Windows Server scope remain downstream builtin/real-machine obligations; no AutoLogon secret is fabricated.

S5: all remaining profile/blank/layout/value differences in this range are directly authored in the committed .tmpl/.fragment snapshots: hostname remains web-01; identity is task-42 (valid model ID) rather than old numeric 42; new tokens shorter but valid; full includes one real script per supported timing, minimal keeps the slot empty (still receives runtime wrapper for stage signals); SSH keys/legacy host metadata omitted from independent assembly fixtures because Task9 owns actual builtin defaults and SSH hook. No implicit old-model field assembly remains.

```diff
-0064           <CommandLine>cmd /c echo lober-firstboot-check &gt;&gt; %WINDIR%\Temp\lober-oobe-marker.log</CommandLine>
-0065           <Description>lober: 确认 SetupComplete.cmd 主执行路径已运行（辅助核对，非主逻辑）</Description>
-0066         </SynchronousCommand>
-0067         <SynchronousCommand wcm:action="add" xmlns:wcm="http://schemas.microsoft.com/WMIConfig/2002/State">
-0068           <Order>2</Order>
-0069           <CommandLine>cmd /c echo lober-pinned-hook: winhookaaa1 &gt;&gt; %WINDIR%\Temp\lober-oobe-marker.log</CommandLine>
-0070           <Description>lober: 记录本任务 pin 的 firstboot hook digest winhookaaa1（仅供核对，真正执行在 SetupComplete.cmd 里）</Description>
+0024           <CommandLine>cmd /d /c powershell.exe -NoProfile -NonInteractive -ExecutionPolicy Bypass -File %SystemDrive%/ProgramData/lober/firstboot.ps1 &amp;&amp; cmd /d /c %SystemDrive%/ProgramData/lober/script-3.cmd &amp;&amp; powershell.exe -NoProfile -NonInteractive -ExecutionPolicy Bypass -File %SystemDrive%/ProgramData/lober/cleanup.ps1</CommandLine>
+0025           <Description>Lober ordered first boot</Description>
```

## unattend-full-uefi.golden

Compared with `unattend_full.golden`; firmware was not split in that old reference.

### Old 2–2; new 2–2

B4: removes old marker-only concurrent FirstLogonCommands and finalize-token-as-AdministratorPassword. One actual oobeSystem cmd && chain: compact -File firstboot.ps1, local cmd wrapper, successful -File cleanup.ps1. Concrete native UTF8-BOM PS files are generated by windowsPartScript and staged pre-reboot by FileAux; no whole EncodedCommand in this field, which has maximum 1024 characters (fixtures 304). PS executes only on installed Windows; WinPE pre wrapper injected by FileAux. SystemDrive resolves target volume. Firmware split has BIOS active NTFS versus UEFI ESP/MSR/NTFS. Login prerequisite and Windows Server scope remain downstream builtin/real-machine obligations; no AutoLogon secret is fabricated.

S5: all remaining profile/blank/layout/value differences in this range are directly authored in the committed .tmpl/.fragment snapshots: hostname remains web-01; identity is task-42 (valid model ID) rather than old numeric 42; new tokens shorter but valid; full includes one real script per supported timing, minimal keeps the slot empty (still receives runtime wrapper for stage signals); SSH keys/legacy host metadata omitted from independent assembly fixtures because Task9 owns actual builtin defaults and SSH hook. No implicit old-model field assembly remains.

```diff
-0002 <unattend xmlns="urn:schemas-microsoft-com:unattend">
+0002 <unattend xmlns="urn:schemas-microsoft-com:unattend" xmlns:wcm="http://schemas.microsoft.com/WMIConfig/2002/State">
```

### Old 6–16; new 6–6

B4: removes old marker-only concurrent FirstLogonCommands and finalize-token-as-AdministratorPassword. One actual oobeSystem cmd && chain: compact -File firstboot.ps1, local cmd wrapper, successful -File cleanup.ps1. Concrete native UTF8-BOM PS files are generated by windowsPartScript and staged pre-reboot by FileAux; no whole EncodedCommand in this field, which has maximum 1024 characters (fixtures 304). PS executes only on installed Windows; WinPE pre wrapper injected by FileAux. SystemDrive resolves target volume. Firmware split has BIOS active NTFS versus UEFI ESP/MSR/NTFS. Login prerequisite and Windows Server scope remain downstream builtin/real-machine obligations; no AutoLogon secret is fabricated.

S5: all remaining profile/blank/layout/value differences in this range are directly authored in the committed .tmpl/.fragment snapshots: hostname remains web-01; identity is task-42 (valid model ID) rather than old numeric 42; new tokens shorter but valid; full includes one real script per supported timing, minimal keeps the slot empty (still receives runtime wrapper for stage signals); SSH keys/legacy host metadata omitted from independent assembly fixtures because Task9 owns actual builtin defaults and SSH hook. No implicit old-model field assembly remains.

```diff
-0006         <Disk wcm:action="add" xmlns:wcm="http://schemas.microsoft.com/WMIConfig/2002/State">
-0007           <DiskID>0</DiskID>
-0008           <WillWipeDisk>true</WillWipeDisk>
-0009           <CreatePartitions>
-0010             <CreatePartition wcm:action="add">
-0011               <Order>1</Order>
-0012               <Type>Primary</Type>
-0013               <Extend>true</Extend>
-0014             </CreatePartition>
-0015           </CreatePartitions>
-0016         </Disk>
+0006         <Disk wcm:action="add"><DiskID>0</DiskID><WillWipeDisk>true</WillWipeDisk><CreatePartitions><CreatePartition wcm:action="add"><Order>1</Order><Type>EFI</Type><Size>100</Size></CreatePartition><CreatePartition wcm:action="add"><Order>2</Order><Type>MSR</Type><Size>16</Size></CreatePartition><CreatePartition wcm:action="add"><Order>3</Order><Type>Primary</Type><Extend>true</Extend></CreatePartition></CreatePartitions><ModifyPartitions><ModifyPartition wcm:action="add"><Order>1</Order><PartitionID>1</PartitionID><Format>FAT32</Format></ModifyPartition><ModifyPartition wcm:action="add"><Order>2</Order><PartitionID>3</PartitionID><Format>NTFS</Format></ModifyPartition></ModifyPartitions></Disk>
```

### Old 18–40; new 8–11

B4: removes old marker-only concurrent FirstLogonCommands and finalize-token-as-AdministratorPassword. One actual oobeSystem cmd && chain: compact -File firstboot.ps1, local cmd wrapper, successful -File cleanup.ps1. Concrete native UTF8-BOM PS files are generated by windowsPartScript and staged pre-reboot by FileAux; no whole EncodedCommand in this field, which has maximum 1024 characters (fixtures 304). PS executes only on installed Windows; WinPE pre wrapper injected by FileAux. SystemDrive resolves target volume. Firmware split has BIOS active NTFS versus UEFI ESP/MSR/NTFS. Login prerequisite and Windows Server scope remain downstream builtin/real-machine obligations; no AutoLogon secret is fabricated.

S5: all remaining profile/blank/layout/value differences in this range are directly authored in the committed .tmpl/.fragment snapshots: hostname remains web-01; identity is task-42 (valid model ID) rather than old numeric 42; new tokens shorter but valid; full includes one real script per supported timing, minimal keeps the slot empty (still receives runtime wrapper for stage signals); SSH keys/legacy host metadata omitted from independent assembly fixtures because Task9 owns actual builtin defaults and SSH hook. No implicit old-model field assembly remains.

```diff
-0018       <ImageInstall>
-0019         <OSImage>
-0020           <InstallFrom>
-0021             <MetaData wcm:action="add" xmlns:wcm="http://schemas.microsoft.com/WMIConfig/2002/State">
-0022               <Key>/IMAGE/NAME</Key>
-0023               <Value>Windows Server 2022</Value>
-0024             </MetaData>
-0025           </InstallFrom>
-0026           <InstallTo>
-0027             <DiskID>0</DiskID>
-0028             <PartitionID>1</PartitionID>
-0029           </InstallTo>
-0030         </OSImage>
-0031       </ImageInstall>
-0032       <UserData>
-0033         <AcceptEula>true</AcceptEula>
-0034       </UserData>
-0035     </component>
-0036     <component name="Microsoft-Windows-International-Core-WinPE" processorArchitecture="amd64" publicKeyToken="31bf3856ad364e35" language="neutral" versionScope="nonSxS">
-0037       <InputLocale>en-US</InputLocale>
-0038       <SystemLocale>en-US</SystemLocale>
-0039       <UILanguage>en-US</UILanguage>
-0040       <UserLocale>en-US</UserLocale>
+0008       <UserData><AcceptEula>true</AcceptEula></UserData>
+0009       <RunSynchronous>
+0010         <RunSynchronousCommand wcm:action="add"><Order>1</Order><Path>cmd /d /c X:/lober/script-2.cmd</Path></RunSynchronousCommand>
+0011       </RunSynchronous>
```

### Old 45–45; new 16–16

B4: removes old marker-only concurrent FirstLogonCommands and finalize-token-as-AdministratorPassword. One actual oobeSystem cmd && chain: compact -File firstboot.ps1, local cmd wrapper, successful -File cleanup.ps1. Concrete native UTF8-BOM PS files are generated by windowsPartScript and staged pre-reboot by FileAux; no whole EncodedCommand in this field, which has maximum 1024 characters (fixtures 304). PS executes only on installed Windows; WinPE pre wrapper injected by FileAux. SystemDrive resolves target volume. Firmware split has BIOS active NTFS versus UEFI ESP/MSR/NTFS. Login prerequisite and Windows Server scope remain downstream builtin/real-machine obligations; no AutoLogon secret is fabricated.

S5: all remaining profile/blank/layout/value differences in this range are directly authored in the committed .tmpl/.fragment snapshots: hostname remains web-01; identity is task-42 (valid model ID) rather than old numeric 42; new tokens shorter but valid; full includes one real script per supported timing, minimal keeps the slot empty (still receives runtime wrapper for stage signals); SSH keys/legacy host metadata omitted from independent assembly fixtures because Task9 owns actual builtin defaults and SSH hook. No implicit old-model field assembly remains.

```diff
-0045       <ComputerName>win-01</ComputerName>
+0016       <ComputerName>web-01</ComputerName>
```

### Old 50–60; new 21–20

B1: firstboot now stages local slot wrappers and local digest payloads before reboot; systemd invokes the installed native runner. Boot credential no longer enters firstboot; independent local finalize token used. Cleanup follows success and erases cached answers/payloads. Legacy firstboot reused install TOKEN despite writing finalize.token. Unit/marker/local paths change to this explicit contract.

B4: removes old marker-only concurrent FirstLogonCommands and finalize-token-as-AdministratorPassword. One actual oobeSystem cmd && chain: compact -File firstboot.ps1, local cmd wrapper, successful -File cleanup.ps1. Concrete native UTF8-BOM PS files are generated by windowsPartScript and staged pre-reboot by FileAux; no whole EncodedCommand in this field, which has maximum 1024 characters (fixtures 304). PS executes only on installed Windows; WinPE pre wrapper injected by FileAux. SystemDrive resolves target volume. Firmware split has BIOS active NTFS versus UEFI ESP/MSR/NTFS. Login prerequisite and Windows Server scope remain downstream builtin/real-machine obligations; no AutoLogon secret is fabricated.

S5: all remaining profile/blank/layout/value differences in this range are directly authored in the committed .tmpl/.fragment snapshots: hostname remains web-01; identity is task-42 (valid model ID) rather than old numeric 42; new tokens shorter but valid; full includes one real script per supported timing, minimal keeps the slot empty (still receives runtime wrapper for stage signals); SSH keys/legacy host metadata omitted from independent assembly fixtures because Task9 owns actual builtin defaults and SSH hook. No implicit old-model field assembly remains.

```diff
-0050       <UserAccounts>
-0051         <AdministratorPassword>
-0052           <Value>finalize-tok-win-fixed</Value>
-0053           <PlainText>true</PlainText>
-0054         </AdministratorPassword>
-0055       </UserAccounts>
-0056       <OOBE>
-0057         <HideEULAPage>true</HideEULAPage>
-0058         <NetworkLocation>Work</NetworkLocation>
-0059         <SkipUserOOBE>true</SkipUserOOBE>
-0060       </OOBE>
```

### Old 62–62; new 22–22

B4: removes old marker-only concurrent FirstLogonCommands and finalize-token-as-AdministratorPassword. One actual oobeSystem cmd && chain: compact -File firstboot.ps1, local cmd wrapper, successful -File cleanup.ps1. Concrete native UTF8-BOM PS files are generated by windowsPartScript and staged pre-reboot by FileAux; no whole EncodedCommand in this field, which has maximum 1024 characters (fixtures 304). PS executes only on installed Windows; WinPE pre wrapper injected by FileAux. SystemDrive resolves target volume. Firmware split has BIOS active NTFS versus UEFI ESP/MSR/NTFS. Login prerequisite and Windows Server scope remain downstream builtin/real-machine obligations; no AutoLogon secret is fabricated.

S5: all remaining profile/blank/layout/value differences in this range are directly authored in the committed .tmpl/.fragment snapshots: hostname remains web-01; identity is task-42 (valid model ID) rather than old numeric 42; new tokens shorter but valid; full includes one real script per supported timing, minimal keeps the slot empty (still receives runtime wrapper for stage signals); SSH keys/legacy host metadata omitted from independent assembly fixtures because Task9 owns actual builtin defaults and SSH hook. No implicit old-model field assembly remains.

```diff
-0062         <SynchronousCommand wcm:action="add" xmlns:wcm="http://schemas.microsoft.com/WMIConfig/2002/State">
+0022         <SynchronousCommand wcm:action="add">
```

### Old 64–70; new 24–25

B4: removes old marker-only concurrent FirstLogonCommands and finalize-token-as-AdministratorPassword. One actual oobeSystem cmd && chain: compact -File firstboot.ps1, local cmd wrapper, successful -File cleanup.ps1. Concrete native UTF8-BOM PS files are generated by windowsPartScript and staged pre-reboot by FileAux; no whole EncodedCommand in this field, which has maximum 1024 characters (fixtures 304). PS executes only on installed Windows; WinPE pre wrapper injected by FileAux. SystemDrive resolves target volume. Firmware split has BIOS active NTFS versus UEFI ESP/MSR/NTFS. Login prerequisite and Windows Server scope remain downstream builtin/real-machine obligations; no AutoLogon secret is fabricated.

S5: all remaining profile/blank/layout/value differences in this range are directly authored in the committed .tmpl/.fragment snapshots: hostname remains web-01; identity is task-42 (valid model ID) rather than old numeric 42; new tokens shorter but valid; full includes one real script per supported timing, minimal keeps the slot empty (still receives runtime wrapper for stage signals); SSH keys/legacy host metadata omitted from independent assembly fixtures because Task9 owns actual builtin defaults and SSH hook. No implicit old-model field assembly remains.

```diff
-0064           <CommandLine>cmd /c echo lober-firstboot-check &gt;&gt; %WINDIR%\Temp\lober-oobe-marker.log</CommandLine>
-0065           <Description>lober: 确认 SetupComplete.cmd 主执行路径已运行（辅助核对，非主逻辑）</Description>
-0066         </SynchronousCommand>
-0067         <SynchronousCommand wcm:action="add" xmlns:wcm="http://schemas.microsoft.com/WMIConfig/2002/State">
-0068           <Order>2</Order>
-0069           <CommandLine>cmd /c echo lober-pinned-hook: winhookaaa1 &gt;&gt; %WINDIR%\Temp\lober-oobe-marker.log</CommandLine>
-0070           <Description>lober: 记录本任务 pin 的 firstboot hook digest winhookaaa1（仅供核对，真正执行在 SetupComplete.cmd 里）</Description>
+0024           <CommandLine>cmd /d /c powershell.exe -NoProfile -NonInteractive -ExecutionPolicy Bypass -File %SystemDrive%/ProgramData/lober/firstboot.ps1 &amp;&amp; cmd /d /c %SystemDrive%/ProgramData/lober/script-3.cmd &amp;&amp; powershell.exe -NoProfile -NonInteractive -ExecutionPolicy Bypass -File %SystemDrive%/ProgramData/lober/cleanup.ps1</CommandLine>
+0025           <Description>Lober ordered first boot</Description>
```

## unattend-minimal-bios.golden

Compared with `unattend_minimal.golden`; firmware was not split in that old reference.

### Old 2–2; new 2–2

B4: removes old marker-only concurrent FirstLogonCommands and finalize-token-as-AdministratorPassword. One actual oobeSystem cmd && chain: compact -File firstboot.ps1, local cmd wrapper, successful -File cleanup.ps1. Concrete native UTF8-BOM PS files are generated by windowsPartScript and staged pre-reboot by FileAux; no whole EncodedCommand in this field, which has maximum 1024 characters (fixtures 304). PS executes only on installed Windows; WinPE pre wrapper injected by FileAux. SystemDrive resolves target volume. Firmware split has BIOS active NTFS versus UEFI ESP/MSR/NTFS. Login prerequisite and Windows Server scope remain downstream builtin/real-machine obligations; no AutoLogon secret is fabricated.

S5: all remaining profile/blank/layout/value differences in this range are directly authored in the committed .tmpl/.fragment snapshots: hostname remains web-01; identity is task-42 (valid model ID) rather than old numeric 42; new tokens shorter but valid; full includes one real script per supported timing, minimal keeps the slot empty (still receives runtime wrapper for stage signals); SSH keys/legacy host metadata omitted from independent assembly fixtures because Task9 owns actual builtin defaults and SSH hook. No implicit old-model field assembly remains.

```diff
-0002 <unattend xmlns="urn:schemas-microsoft-com:unattend">
+0002 <unattend xmlns="urn:schemas-microsoft-com:unattend" xmlns:wcm="http://schemas.microsoft.com/WMIConfig/2002/State">
```

### Old 6–16; new 6–6

B4: removes old marker-only concurrent FirstLogonCommands and finalize-token-as-AdministratorPassword. One actual oobeSystem cmd && chain: compact -File firstboot.ps1, local cmd wrapper, successful -File cleanup.ps1. Concrete native UTF8-BOM PS files are generated by windowsPartScript and staged pre-reboot by FileAux; no whole EncodedCommand in this field, which has maximum 1024 characters (fixtures 304). PS executes only on installed Windows; WinPE pre wrapper injected by FileAux. SystemDrive resolves target volume. Firmware split has BIOS active NTFS versus UEFI ESP/MSR/NTFS. Login prerequisite and Windows Server scope remain downstream builtin/real-machine obligations; no AutoLogon secret is fabricated.

S5: all remaining profile/blank/layout/value differences in this range are directly authored in the committed .tmpl/.fragment snapshots: hostname remains web-01; identity is task-42 (valid model ID) rather than old numeric 42; new tokens shorter but valid; full includes one real script per supported timing, minimal keeps the slot empty (still receives runtime wrapper for stage signals); SSH keys/legacy host metadata omitted from independent assembly fixtures because Task9 owns actual builtin defaults and SSH hook. No implicit old-model field assembly remains.

```diff
-0006         <Disk wcm:action="add" xmlns:wcm="http://schemas.microsoft.com/WMIConfig/2002/State">
-0007           <DiskID>0</DiskID>
-0008           <WillWipeDisk>true</WillWipeDisk>
-0009           <CreatePartitions>
-0010             <CreatePartition wcm:action="add">
-0011               <Order>1</Order>
-0012               <Type>Primary</Type>
-0013               <Extend>true</Extend>
-0014             </CreatePartition>
-0015           </CreatePartitions>
-0016         </Disk>
+0006         <Disk wcm:action="add"><DiskID>0</DiskID><WillWipeDisk>true</WillWipeDisk><CreatePartitions><CreatePartition wcm:action="add"><Order>1</Order><Type>Primary</Type><Extend>true</Extend></CreatePartition></CreatePartitions><ModifyPartitions><ModifyPartition wcm:action="add"><Order>1</Order><PartitionID>1</PartitionID><Format>NTFS</Format><Active>true</Active></ModifyPartition></ModifyPartitions></Disk>
```

### Old 18–40; new 8–11

B4: removes old marker-only concurrent FirstLogonCommands and finalize-token-as-AdministratorPassword. One actual oobeSystem cmd && chain: compact -File firstboot.ps1, local cmd wrapper, successful -File cleanup.ps1. Concrete native UTF8-BOM PS files are generated by windowsPartScript and staged pre-reboot by FileAux; no whole EncodedCommand in this field, which has maximum 1024 characters (fixtures 304). PS executes only on installed Windows; WinPE pre wrapper injected by FileAux. SystemDrive resolves target volume. Firmware split has BIOS active NTFS versus UEFI ESP/MSR/NTFS. Login prerequisite and Windows Server scope remain downstream builtin/real-machine obligations; no AutoLogon secret is fabricated.

S5: all remaining profile/blank/layout/value differences in this range are directly authored in the committed .tmpl/.fragment snapshots: hostname remains web-01; identity is task-42 (valid model ID) rather than old numeric 42; new tokens shorter but valid; full includes one real script per supported timing, minimal keeps the slot empty (still receives runtime wrapper for stage signals); SSH keys/legacy host metadata omitted from independent assembly fixtures because Task9 owns actual builtin defaults and SSH hook. No implicit old-model field assembly remains.

```diff
-0018       <ImageInstall>
-0019         <OSImage>
-0020           <InstallFrom>
-0021             <MetaData wcm:action="add" xmlns:wcm="http://schemas.microsoft.com/WMIConfig/2002/State">
-0022               <Key>/IMAGE/NAME</Key>
-0023               <Value>Windows Server 2022</Value>
-0024             </MetaData>
-0025           </InstallFrom>
-0026           <InstallTo>
-0027             <DiskID>0</DiskID>
-0028             <PartitionID>1</PartitionID>
-0029           </InstallTo>
-0030         </OSImage>
-0031       </ImageInstall>
-0032       <UserData>
-0033         <AcceptEula>true</AcceptEula>
-0034       </UserData>
-0035     </component>
-0036     <component name="Microsoft-Windows-International-Core-WinPE" processorArchitecture="amd64" publicKeyToken="31bf3856ad364e35" language="neutral" versionScope="nonSxS">
-0037       <InputLocale>en-US</InputLocale>
-0038       <SystemLocale>en-US</SystemLocale>
-0039       <UILanguage>en-US</UILanguage>
-0040       <UserLocale>en-US</UserLocale>
+0008       <UserData><AcceptEula>true</AcceptEula></UserData>
+0009       <RunSynchronous>
+0010         <RunSynchronousCommand wcm:action="add"><Order>1</Order><Path>cmd /d /c X:/lober/script-2.cmd</Path></RunSynchronousCommand>
+0011       </RunSynchronous>
```

### Old 45–45; new 16–16

B4: removes old marker-only concurrent FirstLogonCommands and finalize-token-as-AdministratorPassword. One actual oobeSystem cmd && chain: compact -File firstboot.ps1, local cmd wrapper, successful -File cleanup.ps1. Concrete native UTF8-BOM PS files are generated by windowsPartScript and staged pre-reboot by FileAux; no whole EncodedCommand in this field, which has maximum 1024 characters (fixtures 304). PS executes only on installed Windows; WinPE pre wrapper injected by FileAux. SystemDrive resolves target volume. Firmware split has BIOS active NTFS versus UEFI ESP/MSR/NTFS. Login prerequisite and Windows Server scope remain downstream builtin/real-machine obligations; no AutoLogon secret is fabricated.

S5: all remaining profile/blank/layout/value differences in this range are directly authored in the committed .tmpl/.fragment snapshots: hostname remains web-01; identity is task-42 (valid model ID) rather than old numeric 42; new tokens shorter but valid; full includes one real script per supported timing, minimal keeps the slot empty (still receives runtime wrapper for stage signals); SSH keys/legacy host metadata omitted from independent assembly fixtures because Task9 owns actual builtin defaults and SSH hook. No implicit old-model field assembly remains.

```diff
-0045       <ComputerName>win-01</ComputerName>
+0016       <ComputerName>web-01</ComputerName>
```

### Old 50–60; new 21–20

B1: firstboot now stages local slot wrappers and local digest payloads before reboot; systemd invokes the installed native runner. Boot credential no longer enters firstboot; independent local finalize token used. Cleanup follows success and erases cached answers/payloads. Legacy firstboot reused install TOKEN despite writing finalize.token. Unit/marker/local paths change to this explicit contract.

B4: removes old marker-only concurrent FirstLogonCommands and finalize-token-as-AdministratorPassword. One actual oobeSystem cmd && chain: compact -File firstboot.ps1, local cmd wrapper, successful -File cleanup.ps1. Concrete native UTF8-BOM PS files are generated by windowsPartScript and staged pre-reboot by FileAux; no whole EncodedCommand in this field, which has maximum 1024 characters (fixtures 304). PS executes only on installed Windows; WinPE pre wrapper injected by FileAux. SystemDrive resolves target volume. Firmware split has BIOS active NTFS versus UEFI ESP/MSR/NTFS. Login prerequisite and Windows Server scope remain downstream builtin/real-machine obligations; no AutoLogon secret is fabricated.

S5: all remaining profile/blank/layout/value differences in this range are directly authored in the committed .tmpl/.fragment snapshots: hostname remains web-01; identity is task-42 (valid model ID) rather than old numeric 42; new tokens shorter but valid; full includes one real script per supported timing, minimal keeps the slot empty (still receives runtime wrapper for stage signals); SSH keys/legacy host metadata omitted from independent assembly fixtures because Task9 owns actual builtin defaults and SSH hook. No implicit old-model field assembly remains.

```diff
-0050       <UserAccounts>
-0051         <AdministratorPassword>
-0052           <Value>finalize-tok-win-fixed</Value>
-0053           <PlainText>true</PlainText>
-0054         </AdministratorPassword>
-0055       </UserAccounts>
-0056       <OOBE>
-0057         <HideEULAPage>true</HideEULAPage>
-0058         <NetworkLocation>Work</NetworkLocation>
-0059         <SkipUserOOBE>true</SkipUserOOBE>
-0060       </OOBE>
```

### Old 62–62; new 22–22

B4: removes old marker-only concurrent FirstLogonCommands and finalize-token-as-AdministratorPassword. One actual oobeSystem cmd && chain: compact -File firstboot.ps1, local cmd wrapper, successful -File cleanup.ps1. Concrete native UTF8-BOM PS files are generated by windowsPartScript and staged pre-reboot by FileAux; no whole EncodedCommand in this field, which has maximum 1024 characters (fixtures 304). PS executes only on installed Windows; WinPE pre wrapper injected by FileAux. SystemDrive resolves target volume. Firmware split has BIOS active NTFS versus UEFI ESP/MSR/NTFS. Login prerequisite and Windows Server scope remain downstream builtin/real-machine obligations; no AutoLogon secret is fabricated.

S5: all remaining profile/blank/layout/value differences in this range are directly authored in the committed .tmpl/.fragment snapshots: hostname remains web-01; identity is task-42 (valid model ID) rather than old numeric 42; new tokens shorter but valid; full includes one real script per supported timing, minimal keeps the slot empty (still receives runtime wrapper for stage signals); SSH keys/legacy host metadata omitted from independent assembly fixtures because Task9 owns actual builtin defaults and SSH hook. No implicit old-model field assembly remains.

```diff
-0062         <SynchronousCommand wcm:action="add" xmlns:wcm="http://schemas.microsoft.com/WMIConfig/2002/State">
+0022         <SynchronousCommand wcm:action="add">
```

### Old 64–65; new 24–25

B4: removes old marker-only concurrent FirstLogonCommands and finalize-token-as-AdministratorPassword. One actual oobeSystem cmd && chain: compact -File firstboot.ps1, local cmd wrapper, successful -File cleanup.ps1. Concrete native UTF8-BOM PS files are generated by windowsPartScript and staged pre-reboot by FileAux; no whole EncodedCommand in this field, which has maximum 1024 characters (fixtures 304). PS executes only on installed Windows; WinPE pre wrapper injected by FileAux. SystemDrive resolves target volume. Firmware split has BIOS active NTFS versus UEFI ESP/MSR/NTFS. Login prerequisite and Windows Server scope remain downstream builtin/real-machine obligations; no AutoLogon secret is fabricated.

S5: all remaining profile/blank/layout/value differences in this range are directly authored in the committed .tmpl/.fragment snapshots: hostname remains web-01; identity is task-42 (valid model ID) rather than old numeric 42; new tokens shorter but valid; full includes one real script per supported timing, minimal keeps the slot empty (still receives runtime wrapper for stage signals); SSH keys/legacy host metadata omitted from independent assembly fixtures because Task9 owns actual builtin defaults and SSH hook. No implicit old-model field assembly remains.

```diff
-0064           <CommandLine>cmd /c echo lober-firstboot-check &gt;&gt; %WINDIR%\Temp\lober-oobe-marker.log</CommandLine>
-0065           <Description>lober: 确认 SetupComplete.cmd 主执行路径已运行（辅助核对，非主逻辑）</Description>
+0024           <CommandLine>cmd /d /c powershell.exe -NoProfile -NonInteractive -ExecutionPolicy Bypass -File %SystemDrive%/ProgramData/lober/firstboot.ps1 &amp;&amp; cmd /d /c %SystemDrive%/ProgramData/lober/script-3.cmd &amp;&amp; powershell.exe -NoProfile -NonInteractive -ExecutionPolicy Bypass -File %SystemDrive%/ProgramData/lober/cleanup.ps1</CommandLine>
+0025           <Description>Lober ordered first boot</Description>
```

## unattend-minimal-uefi.golden

Compared with `unattend_minimal.golden`; firmware was not split in that old reference.

### Old 2–2; new 2–2

B4: removes old marker-only concurrent FirstLogonCommands and finalize-token-as-AdministratorPassword. One actual oobeSystem cmd && chain: compact -File firstboot.ps1, local cmd wrapper, successful -File cleanup.ps1. Concrete native UTF8-BOM PS files are generated by windowsPartScript and staged pre-reboot by FileAux; no whole EncodedCommand in this field, which has maximum 1024 characters (fixtures 304). PS executes only on installed Windows; WinPE pre wrapper injected by FileAux. SystemDrive resolves target volume. Firmware split has BIOS active NTFS versus UEFI ESP/MSR/NTFS. Login prerequisite and Windows Server scope remain downstream builtin/real-machine obligations; no AutoLogon secret is fabricated.

S5: all remaining profile/blank/layout/value differences in this range are directly authored in the committed .tmpl/.fragment snapshots: hostname remains web-01; identity is task-42 (valid model ID) rather than old numeric 42; new tokens shorter but valid; full includes one real script per supported timing, minimal keeps the slot empty (still receives runtime wrapper for stage signals); SSH keys/legacy host metadata omitted from independent assembly fixtures because Task9 owns actual builtin defaults and SSH hook. No implicit old-model field assembly remains.

```diff
-0002 <unattend xmlns="urn:schemas-microsoft-com:unattend">
+0002 <unattend xmlns="urn:schemas-microsoft-com:unattend" xmlns:wcm="http://schemas.microsoft.com/WMIConfig/2002/State">
```

### Old 6–16; new 6–6

B4: removes old marker-only concurrent FirstLogonCommands and finalize-token-as-AdministratorPassword. One actual oobeSystem cmd && chain: compact -File firstboot.ps1, local cmd wrapper, successful -File cleanup.ps1. Concrete native UTF8-BOM PS files are generated by windowsPartScript and staged pre-reboot by FileAux; no whole EncodedCommand in this field, which has maximum 1024 characters (fixtures 304). PS executes only on installed Windows; WinPE pre wrapper injected by FileAux. SystemDrive resolves target volume. Firmware split has BIOS active NTFS versus UEFI ESP/MSR/NTFS. Login prerequisite and Windows Server scope remain downstream builtin/real-machine obligations; no AutoLogon secret is fabricated.

S5: all remaining profile/blank/layout/value differences in this range are directly authored in the committed .tmpl/.fragment snapshots: hostname remains web-01; identity is task-42 (valid model ID) rather than old numeric 42; new tokens shorter but valid; full includes one real script per supported timing, minimal keeps the slot empty (still receives runtime wrapper for stage signals); SSH keys/legacy host metadata omitted from independent assembly fixtures because Task9 owns actual builtin defaults and SSH hook. No implicit old-model field assembly remains.

```diff
-0006         <Disk wcm:action="add" xmlns:wcm="http://schemas.microsoft.com/WMIConfig/2002/State">
-0007           <DiskID>0</DiskID>
-0008           <WillWipeDisk>true</WillWipeDisk>
-0009           <CreatePartitions>
-0010             <CreatePartition wcm:action="add">
-0011               <Order>1</Order>
-0012               <Type>Primary</Type>
-0013               <Extend>true</Extend>
-0014             </CreatePartition>
-0015           </CreatePartitions>
-0016         </Disk>
+0006         <Disk wcm:action="add"><DiskID>0</DiskID><WillWipeDisk>true</WillWipeDisk><CreatePartitions><CreatePartition wcm:action="add"><Order>1</Order><Type>EFI</Type><Size>100</Size></CreatePartition><CreatePartition wcm:action="add"><Order>2</Order><Type>MSR</Type><Size>16</Size></CreatePartition><CreatePartition wcm:action="add"><Order>3</Order><Type>Primary</Type><Extend>true</Extend></CreatePartition></CreatePartitions><ModifyPartitions><ModifyPartition wcm:action="add"><Order>1</Order><PartitionID>1</PartitionID><Format>FAT32</Format></ModifyPartition><ModifyPartition wcm:action="add"><Order>2</Order><PartitionID>3</PartitionID><Format>NTFS</Format></ModifyPartition></ModifyPartitions></Disk>
```

### Old 18–40; new 8–11

B4: removes old marker-only concurrent FirstLogonCommands and finalize-token-as-AdministratorPassword. One actual oobeSystem cmd && chain: compact -File firstboot.ps1, local cmd wrapper, successful -File cleanup.ps1. Concrete native UTF8-BOM PS files are generated by windowsPartScript and staged pre-reboot by FileAux; no whole EncodedCommand in this field, which has maximum 1024 characters (fixtures 304). PS executes only on installed Windows; WinPE pre wrapper injected by FileAux. SystemDrive resolves target volume. Firmware split has BIOS active NTFS versus UEFI ESP/MSR/NTFS. Login prerequisite and Windows Server scope remain downstream builtin/real-machine obligations; no AutoLogon secret is fabricated.

S5: all remaining profile/blank/layout/value differences in this range are directly authored in the committed .tmpl/.fragment snapshots: hostname remains web-01; identity is task-42 (valid model ID) rather than old numeric 42; new tokens shorter but valid; full includes one real script per supported timing, minimal keeps the slot empty (still receives runtime wrapper for stage signals); SSH keys/legacy host metadata omitted from independent assembly fixtures because Task9 owns actual builtin defaults and SSH hook. No implicit old-model field assembly remains.

```diff
-0018       <ImageInstall>
-0019         <OSImage>
-0020           <InstallFrom>
-0021             <MetaData wcm:action="add" xmlns:wcm="http://schemas.microsoft.com/WMIConfig/2002/State">
-0022               <Key>/IMAGE/NAME</Key>
-0023               <Value>Windows Server 2022</Value>
-0024             </MetaData>
-0025           </InstallFrom>
-0026           <InstallTo>
-0027             <DiskID>0</DiskID>
-0028             <PartitionID>1</PartitionID>
-0029           </InstallTo>
-0030         </OSImage>
-0031       </ImageInstall>
-0032       <UserData>
-0033         <AcceptEula>true</AcceptEula>
-0034       </UserData>
-0035     </component>
-0036     <component name="Microsoft-Windows-International-Core-WinPE" processorArchitecture="amd64" publicKeyToken="31bf3856ad364e35" language="neutral" versionScope="nonSxS">
-0037       <InputLocale>en-US</InputLocale>
-0038       <SystemLocale>en-US</SystemLocale>
-0039       <UILanguage>en-US</UILanguage>
-0040       <UserLocale>en-US</UserLocale>
+0008       <UserData><AcceptEula>true</AcceptEula></UserData>
+0009       <RunSynchronous>
+0010         <RunSynchronousCommand wcm:action="add"><Order>1</Order><Path>cmd /d /c X:/lober/script-2.cmd</Path></RunSynchronousCommand>
+0011       </RunSynchronous>
```

### Old 45–45; new 16–16

B4: removes old marker-only concurrent FirstLogonCommands and finalize-token-as-AdministratorPassword. One actual oobeSystem cmd && chain: compact -File firstboot.ps1, local cmd wrapper, successful -File cleanup.ps1. Concrete native UTF8-BOM PS files are generated by windowsPartScript and staged pre-reboot by FileAux; no whole EncodedCommand in this field, which has maximum 1024 characters (fixtures 304). PS executes only on installed Windows; WinPE pre wrapper injected by FileAux. SystemDrive resolves target volume. Firmware split has BIOS active NTFS versus UEFI ESP/MSR/NTFS. Login prerequisite and Windows Server scope remain downstream builtin/real-machine obligations; no AutoLogon secret is fabricated.

S5: all remaining profile/blank/layout/value differences in this range are directly authored in the committed .tmpl/.fragment snapshots: hostname remains web-01; identity is task-42 (valid model ID) rather than old numeric 42; new tokens shorter but valid; full includes one real script per supported timing, minimal keeps the slot empty (still receives runtime wrapper for stage signals); SSH keys/legacy host metadata omitted from independent assembly fixtures because Task9 owns actual builtin defaults and SSH hook. No implicit old-model field assembly remains.

```diff
-0045       <ComputerName>win-01</ComputerName>
+0016       <ComputerName>web-01</ComputerName>
```

### Old 50–60; new 21–20

B1: firstboot now stages local slot wrappers and local digest payloads before reboot; systemd invokes the installed native runner. Boot credential no longer enters firstboot; independent local finalize token used. Cleanup follows success and erases cached answers/payloads. Legacy firstboot reused install TOKEN despite writing finalize.token. Unit/marker/local paths change to this explicit contract.

B4: removes old marker-only concurrent FirstLogonCommands and finalize-token-as-AdministratorPassword. One actual oobeSystem cmd && chain: compact -File firstboot.ps1, local cmd wrapper, successful -File cleanup.ps1. Concrete native UTF8-BOM PS files are generated by windowsPartScript and staged pre-reboot by FileAux; no whole EncodedCommand in this field, which has maximum 1024 characters (fixtures 304). PS executes only on installed Windows; WinPE pre wrapper injected by FileAux. SystemDrive resolves target volume. Firmware split has BIOS active NTFS versus UEFI ESP/MSR/NTFS. Login prerequisite and Windows Server scope remain downstream builtin/real-machine obligations; no AutoLogon secret is fabricated.

S5: all remaining profile/blank/layout/value differences in this range are directly authored in the committed .tmpl/.fragment snapshots: hostname remains web-01; identity is task-42 (valid model ID) rather than old numeric 42; new tokens shorter but valid; full includes one real script per supported timing, minimal keeps the slot empty (still receives runtime wrapper for stage signals); SSH keys/legacy host metadata omitted from independent assembly fixtures because Task9 owns actual builtin defaults and SSH hook. No implicit old-model field assembly remains.

```diff
-0050       <UserAccounts>
-0051         <AdministratorPassword>
-0052           <Value>finalize-tok-win-fixed</Value>
-0053           <PlainText>true</PlainText>
-0054         </AdministratorPassword>
-0055       </UserAccounts>
-0056       <OOBE>
-0057         <HideEULAPage>true</HideEULAPage>
-0058         <NetworkLocation>Work</NetworkLocation>
-0059         <SkipUserOOBE>true</SkipUserOOBE>
-0060       </OOBE>
```

### Old 62–62; new 22–22

B4: removes old marker-only concurrent FirstLogonCommands and finalize-token-as-AdministratorPassword. One actual oobeSystem cmd && chain: compact -File firstboot.ps1, local cmd wrapper, successful -File cleanup.ps1. Concrete native UTF8-BOM PS files are generated by windowsPartScript and staged pre-reboot by FileAux; no whole EncodedCommand in this field, which has maximum 1024 characters (fixtures 304). PS executes only on installed Windows; WinPE pre wrapper injected by FileAux. SystemDrive resolves target volume. Firmware split has BIOS active NTFS versus UEFI ESP/MSR/NTFS. Login prerequisite and Windows Server scope remain downstream builtin/real-machine obligations; no AutoLogon secret is fabricated.

S5: all remaining profile/blank/layout/value differences in this range are directly authored in the committed .tmpl/.fragment snapshots: hostname remains web-01; identity is task-42 (valid model ID) rather than old numeric 42; new tokens shorter but valid; full includes one real script per supported timing, minimal keeps the slot empty (still receives runtime wrapper for stage signals); SSH keys/legacy host metadata omitted from independent assembly fixtures because Task9 owns actual builtin defaults and SSH hook. No implicit old-model field assembly remains.

```diff
-0062         <SynchronousCommand wcm:action="add" xmlns:wcm="http://schemas.microsoft.com/WMIConfig/2002/State">
+0022         <SynchronousCommand wcm:action="add">
```

### Old 64–65; new 24–25

B4: removes old marker-only concurrent FirstLogonCommands and finalize-token-as-AdministratorPassword. One actual oobeSystem cmd && chain: compact -File firstboot.ps1, local cmd wrapper, successful -File cleanup.ps1. Concrete native UTF8-BOM PS files are generated by windowsPartScript and staged pre-reboot by FileAux; no whole EncodedCommand in this field, which has maximum 1024 characters (fixtures 304). PS executes only on installed Windows; WinPE pre wrapper injected by FileAux. SystemDrive resolves target volume. Firmware split has BIOS active NTFS versus UEFI ESP/MSR/NTFS. Login prerequisite and Windows Server scope remain downstream builtin/real-machine obligations; no AutoLogon secret is fabricated.

S5: all remaining profile/blank/layout/value differences in this range are directly authored in the committed .tmpl/.fragment snapshots: hostname remains web-01; identity is task-42 (valid model ID) rather than old numeric 42; new tokens shorter but valid; full includes one real script per supported timing, minimal keeps the slot empty (still receives runtime wrapper for stage signals); SSH keys/legacy host metadata omitted from independent assembly fixtures because Task9 owns actual builtin defaults and SSH hook. No implicit old-model field assembly remains.

```diff
-0064           <CommandLine>cmd /c echo lober-firstboot-check &gt;&gt; %WINDIR%\Temp\lober-oobe-marker.log</CommandLine>
-0065           <Description>lober: 确认 SetupComplete.cmd 主执行路径已运行（辅助核对，非主逻辑）</Description>
+0024           <CommandLine>cmd /d /c powershell.exe -NoProfile -NonInteractive -ExecutionPolicy Bypass -File %SystemDrive%/ProgramData/lober/firstboot.ps1 &amp;&amp; cmd /d /c %SystemDrive%/ProgramData/lober/script-3.cmd &amp;&amp; powershell.exe -NoProfile -NonInteractive -ExecutionPolicy Bypass -File %SystemDrive%/ProgramData/lober/cleanup.ps1</CommandLine>
+0025           <Description>Lober ordered first boot</Description>
```

