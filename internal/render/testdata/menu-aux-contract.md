# Menu and auxiliary caller contract (Task 7)

Rendering is pure. The caller supplies the frozen Snapshot and request values; rendering reads no artifact file, clock, database or network.

`Request.Firmware` is mandatory and must be `bios` or `uefi` for every FileMenu call. Layer7 converts actual iPXE `${platform}` through catalog.FirmwareFromIPXE. Snapshot.Host.Firmware remains optional registered metadata and never selects a fallback. Snapshot.Series must agree with Snapshot.Family and Snapshot.Format; catalog.Series.Firmwares limits ESXi to UEFI. Actual architecture checking, state transitions and on-disk checks remain layer7 responsibilities before menu delivery/token issuance. Firmware-independent Linux menus deliberately have identical BIOS/UEFI bytes; disk partition decisions are separate.

## Layer4 artifact roles

All values below are disk paths relative to the configured artifacts base, validated by the importer: nonempty, no leading slash, no dot/dot-dot segments; full case-sensitive extraction must be immutable for the referenced ISO version. Directory roles carry a trailing slash. Examples are illustrative and do not constrain importer directory naming.

| Role | Meaning | Example | Consumer |
|---|---|---|---|
| kernel | Actual installer kernel; Debian requires matching netboot kernel, not arbitrary netinst payload | rocky/9.4/x86_64/images/pxeboot/vmlinuz | kickstart/preseed/autoinstall menu |
| initrd | Matching installer initrd; Debian requires the same-version netboot initrd | debian/12/x86_64/netboot/initrd.gz | kickstart/preseed/autoinstall menu |
| stage2 | Root of full extracted RHEL installation tree, with .treeinfo and images/install.img as applicable; never a package-index-only directory | rocky/9.4/x86_64/install/ | kickstart inst.stage2 |
| repo | Independent package repository root with valid repository metadata | rocky/9.4/x86_64/repo/ | derived private sys:repo / inst.repo |
| iso | Complete original immutable Ubuntu ISO file; never extracted tree, squashfs, kernel or repository | ubuntu/24.04/x86_64/original.iso | derived private sys:iso / Ubuntu url= |
| mboot | Actual UEFI ESXi loader from this immutable media version, such as efi/boot/bootx64.efi (may be copied as mboot.efi) | esxi/8.0/x86_64/efi/boot/bootx64.efi | ESXi menu kernel -c boot.cfg |
| esxi-tree | Root of the complete unmodified case-sensitive ESXi extracted installation tree, containing every original kernel/module path | esxi/8.0/x86_64/ | ESXi boot.cfg HTTP prefix |

The only address source is `Request.Values[artifacts]` plus `Snapshot.ISO.Artifacts[role]`. Repo and ISO are already reserved derived system values: caller-supplied repo/iso remain content Problems, even if they match. No separate URL/provenance state was added. Layer4 must provide stage2 independently from repo; its layout is not inferred from other roles. The importer must validate real existence/completeness; renderer validates missing required roles but does no filesystem I/O.

## Caller values and routes

Menus need gateway/artifacts/task/token; RHEL additionally needs syslog-addr. Catalog.AnswerURL and ArtifactURL perform URL joining/segment escaping. Gateway/artifacts are caller-configured machine-facing base URLs and task/token follow the established safe-ID contract. Answer routes and names use catalog.DeliveryFileName: ks.cfg, preseed.cfg, user-data; ESXi loader uses boot.cfg. Ubuntu NoCloud s= points to AnswerURL(..., empty filename) with a trailing slash. The gateway must serve user-data and meta-data under that same task/token directory. Ubuntu metadata uses only task and the frozen required hostname template parameter, not network credentials or artifact paths.

## Original ESXi boot.cfg

For `FileAux` with esxi-ks, `Request.ESXiBootConfig` must contain the caller-supplied original immutable ISO boot.cfg. Layer4 preserves the source file; layer5/7 fetches its immutable bytes outside render when preparing the request. Do not synthesize a config by enumerating or sorting Artifacts. Absence, malformed content, duplicate known fields, missing kernel/modules, unsafe paths or incompatible absolute module URLs return render_esxi_bootcfg_invalid Problems and no bytes.

Rendering retains original module order, complete nested paths, arguments, unknown fields, comments and line endings. It replaces/adds HTTP prefix using esxi-tree; removes only the leading ISO-root slash from kernel/module filename tokens; replaces/adds ks= using canonical FileAnswer ks.cfg; and removes only the whole cdromBoot token required by VMware HTTP procedure. No basename conversion or module sorting occurs. An absolute HTTP/HTTPS file URL already beneath the same published esxi-tree is retained; other absolute URLs fail explicitly rather than silently moving immutable vendor references. Prefix and kernelopt may be absent in original media; missing fields are appended. Extra boot.cfg content is not authored by lober.

Source: [VMware ESXi Installation and Setup vSphere 8.0](https://d3bql97l1ytoxn.cloudfront.net/app_resources/447311/documentation/1439997_1721785831666_en-US.pdf), p83–84 config syntax and p105–106 iPXE HTTP procedure (prefix/leading slash/cdromBoot). Layer7 retains real-machine validation of loader/version compatibility, networking, path case and full module availability. No BIOS ESXi path is provided.

## Deliberately pending work

Task8 owns autoyast/agama/unattend menus, WinPE auxiliary staging, ESXi answer body and FileScript runtime. Regression hashes 686afcd (runtime timeout availability), a59a0bb (slot telemetry) and a570ad5 (early-command assembly) do not describe menu boot arguments; Task6/8 runtime checks cover their corresponding behavior. Current menu tests cover 4e32ed9, 99b6ca7, 75bdc20, 7eb805e, 7d38246, 23442b1, 2ab744d and related 9a252db. RHEL supports documented inst.syslog; no fabricated syslog boot parameter was added for Debian/Ubuntu/ESXi.
