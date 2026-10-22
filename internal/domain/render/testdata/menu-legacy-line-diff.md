# Task 7 complete legacy golden line audit

All changed lines are enumerated below. No fixture update was generated from renderer output. Both Linux firmware goldens are byte-identical by design: actual firmware is checked, while these Linux kernel/initrd menu shapes do not partition disks. ESXi has only UEFI coverage.

## menu_rocky.golden → menu-rocky-bios.golden

```diff
--- menu_rocky.golden
+++ menu-rocky-bios.golden
@@ -1,4 +1,8 @@
+#!ipxe
 kernel http://ng.internal/artifacts/as-rocky94/vmlinuz \
+  inst.stage2=http://ng.internal/artifacts/as-rocky94/install/ \
   inst.repo=http://ng.internal/artifacts/as-rocky94/repo/ \
-  inst.ks=http://gw.internal/v1/answer/42/install-tok-fixed-0000000000000000/ks.cfg inst.text ip=dhcp
+  inst.ks=http://gw.internal/v1/answer/42/install-tok-fixed-0000000000000000/ks.cfg inst.cmdline inst.notmux ip=dhcp \
+  console=tty0 console=ttyS0,115200n8 inst.syslog=10.10.0.1:514
 initrd http://ng.internal/artifacts/as-rocky94/initrd.img
+boot
```

- add line 1: `#!ipxe` — 99b6ca7: iPXE recognizes executable script via mandatory first-line shebang.

- add line 3: `  inst.stage2=http://ng.internal/artifacts/as-rocky94/install/ \` — 75bdc20 / 7eb805e: explicit full extracted installer tree, independent of package repository.

- delete line 3: `  inst.ks=http://gw.internal/v1/answer/42/install-tok-fixed-0000000000000000/ks.cfg inst.text ip=dhcp` — Replace interactive inst.text with noninteractive inst.cmdline; add inst.notmux so failures are visible; retain ks URL and DHCP.

- add line 5: `  inst.ks=http://gw.internal/v1/answer/42/install-tok-fixed-0000000000000000/ks.cfg inst.cmdline inst.notmux ip=dhcp \` — 9a252db / 2ab744d: noninteractive installer, no tmux; continuation connects logging/console arguments.

- add line 6: `  console=tty0 console=ttyS0,115200n8 inst.syslog=10.10.0.1:514` — 7d38246 / 23442b1: local and serial output plus remote installer-startup syslog.

- add line 8: `boot` — 4e32ed9: run the loaded kernel/initrd instead of stopping after loading.

## menu_debian.golden → menu-debian-bios.golden

```diff
--- menu_debian.golden
+++ menu-debian-bios.golden
@@ -1,3 +1,6 @@
+#!ipxe
 kernel http://ng.internal/artifacts/as-debian12/linux auto=true priority=critical \
-  preseed/url=http://gw.internal/v1/answer/42/install-tok-fixed-0000000000000000/preseed.cfg
+  preseed/url=http://gw.internal/v1/answer/42/install-tok-fixed-0000000000000000/preseed.cfg \
+  console=tty0 console=ttyS0,115200n8
 initrd http://ng.internal/artifacts/as-debian12/initrd.gz
+boot
```

- add line 1: `#!ipxe` — 99b6ca7: recognizable iPXE script.

- delete line 2: `  preseed/url=http://gw.internal/v1/answer/42/install-tok-fixed-0000000000000000/preseed.cfg` — Retain the exact preseed URL; line needs continuation to attach serial console arguments.

- add line 3: `  preseed/url=http://gw.internal/v1/answer/42/install-tok-fixed-0000000000000000/preseed.cfg \` — Same machine route as legacy; continuation introduces console options.

- add line 4: `  console=tty0 console=ttyS0,115200n8` — 7d38246: boot output to local and serial consoles.

- add line 6: `boot` — 4e32ed9: execute loaded images.

## menu_ubuntu.golden → menu-ubuntu-bios.golden

```diff
--- menu_ubuntu.golden
+++ menu-ubuntu-bios.golden
@@ -1,2 +1,4 @@
-kernel http://ng.internal/artifacts/as-ubuntu2404/vmlinuz autoinstall ds=nocloud-net;s=http://gw.internal/v1/answer/42/install-tok-fixed-0000000000000000/ ---
+#!ipxe
+kernel http://ng.internal/artifacts/as-ubuntu2404/vmlinuz autoinstall ip=dhcp url=http://ng.internal/artifacts/as-ubuntu2404/original.iso ds=nocloud-net;s=http://gw.internal/v1/answer/42/install-tok-fixed-0000000000000000/ console=tty0 console=ttyS0,115200n8 ---
 initrd http://ng.internal/artifacts/as-ubuntu2404/initrd
+boot
```

- delete line 1: `kernel http://ng.internal/artifacts/as-ubuntu2404/vmlinuz autoinstall ds=nocloud-net;s=http://gw.internal/v1/answer/42/install-tok-fixed-0000000000000000/ ---` — This legacy path cannot install: it omits the full ISO URL. Preserve kernel, autoinstall, NoCloud directory and delimiter while adding required ISO/DHCP/console options.

- add line 1: `#!ipxe` — 99b6ca7: recognizable iPXE script.

- add line 2: `kernel http://ng.internal/artifacts/as-ubuntu2404/vmlinuz autoinstall ip=dhcp url=http://ng.internal/artifacts/as-ubuntu2404/original.iso ds=nocloud-net;s=http://gw.internal/v1/answer/42/install-tok-fixed-0000000000000000/ console=tty0 console=ttyS0,115200n8 ---` — Spec §2.2 original ISO requirement; ip=dhcp brings up the network before ISO/seed fetching; 7d38246 serial output. Seed still ends in / and resolves user-data and meta-data siblings.

- add line 4: `boot` — 4e32ed9: execute loaded images.

## esxi_bootcfg.golden → esxi-bootcfg.golden

```diff
--- esxi_bootcfg.golden
+++ esxi-bootcfg.golden
@@ -2,8 +2,8 @@
 title=Loading ESXi installer
 timeout=5
 prefix=http://ng.internal/artifacts/esxi8
-kernel=/b.b00
-kernelopt=runweasel ks=http://gw.internal/v1/answer/42/install-tok-fixed-0000000000000000/boot.cfg
+kernel=b.b00
+kernelopt=runweasel ks=http://gw.internal/v1/answer/42/install-tok-fixed-0000000000000000/ks.cfg
 modules=jumpstrt.gz --- useropts.gz --- k.b00 --- chain.b00
 build=8.0.0-2000000
 updated=0
```

- delete line 5: `kernel=/b.b00` — A leading ISO-root slash bypasses HTTP prefix. VMware HTTP boot procedure requires its removal.

- delete line 6: `kernelopt=runweasel ks=http://gw.internal/v1/answer/42/install-tok-fixed-0000000000000000/boot.cfg` — Legacy ks wrongly names boot.cfg, which is loader configuration rather than the installer answer.

- add line 5: `kernel=b.b00` — Rebase only the leading slash; preserve complete kernel path (nested paths tested separately).

- add line 6: `kernelopt=runweasel ks=http://gw.internal/v1/answer/42/install-tok-fixed-0000000000000000/ks.cfg` — Use canonical catalog FileAnswer name ks.cfg, preserving runweasel.

## meta_data.golden → meta-data.golden

```diff

```

Byte-identical to legacy; instance-id and local-hostname lines retain their original values.

## Additional original-media changes absent from old golden

The original ISO media fixture contains leading `/` on all module file tokens, unlike the old generated golden. Remove only that leading slash so HTTP prefix is honored; keep each full path, module order and options. Tests cover nested `z/vendor.v00`, `a/module.v00`, `z/other.v00`, options, original tabs/spaces, and CRLF. No artifact-map-derived module list exists.

Original-media `cdromBoot` is removed only as a complete kernelopt token, per VMware HTTP step 7c (p105). `cdromBootExtra`, `runweasel`, `keep=1` and every unrelated option remain in the original order. Existing `ks=` is replaced in place; absent `ks=` is appended. Missing optional prefix/kernelopt fields are added while preserving the original newline convention. Compatible absolute HTTP/HTTPS module URLs remain byte-identical; incompatible published-tree URLs return Problems, not rewritten URLs.

ESXi iPXE menu has no legacy golden; it follows inherited-spec §4.5 with the original EFI loader artifact, canonical boot.cfg URL, mandatory shebang and terminal boot. Neither source nor body/script work was added for Task8.

Primary source: [VMware ESXi Installation and Setup vSphere 8.0](https://d3bql97l1ytoxn.cloudfront.net/app_resources/447311/documentation/1439997_1721785831666_en-US.pdf), p83–84 boot.cfg syntax; p105–106 iPXE HTTP procedure. Red Hat arguments: [RHEL 8 boot options](https://docs.redhat.com/en/documentation/red_hat_enterprise_linux/8/html/automatically_installing_rhel/custom-boot-options_rhel-installer). Ubuntu original-ISO parameter is pinned by rewrite spec §2.2; [Canonical netboot testing source](https://github.com/canonical/subiquity/blob/main/NETBOOT_TESTS.md) documents the installer ISO URL. [Canonical NoCloud reference](https://cloudinit.readthedocs.io/en/latest/reference/datasources/nocloud.html) documents seed sibling files and directory slash.
