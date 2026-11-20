# Builtin release 8: content and verification limits

The current manifest ships six templates and five hooks. Linux support is limited to RHEL, AlmaLinux, Rocky Linux, Ubuntu and Debian. Windows Server and ESXi remain available. Bodies are compiled with embed; production builtin code imports only the model/catalog and the standard library.

## Immutable releases and upgrades

Release v8 removes active AutoYaST and Agama templates and implementations, plus openEuler and OpenCloudOS from Kickstart implementations. Shared hooks publish partition v3, packages v4, network v2, account v3 and sshkey v2. Templates publish kickstart/preseed v5, autoinstall/unattend/esxi v4 and preseed-inventory v2 because their hook references changed.

The v1-v7 release files and their fingerprints remain unchanged. Historical releases retain retired content as data for fingerprint verification and PostgreSQL upgrade replay. They are not current installation templates and are not rendered or activated by current-content tests. The replay test loads every release twice, then rereads earlier versions and checks their content and stored revisions. Replacing existing content under the same identity/version remains rejected.

Current rendering tests build real snapshots and use the same render entry as production. They cover answers, menus, auxiliary files, execution wrappers, parameter changes, package lists and firmware handling. The retained answers are unchanged: shared component support metadata and version references changed, while their supported-system bodies did not. Golden recording refuses to overwrite existing files.

## Retained installer behavior

| Format | Shipped behavior | Verification limit |
|---|---|---|
| Kickstart | Native GPT disk selection, reqpart, explicit /boot and root, user, SSH key, DHCP and packages. Partition commands are inlined because AlmaLinux 10 resolves %include before %pre runs. | Tests check generated commands and firmware-independent answers. Actual boot partitions and package availability require real media. |
| Preseed | Native atomic partition proposal, explicit signed repository mirror parameters, account/network/packages, and late staging of the SSH script. The inventory template takes the system disk from the collected host attribute. | Tests cover chosen disk, missing inventory, mirror values, package list and script output. They do not run the installer. |
| Autoinstall | Direct GPT layout on the selected disk, identity, SSH, DHCP, packages and late commands. RAM must hold the full ISO plus installer working memory. | Tests check rendering, hostname/NoCloud agreement and values. The optional Python check parses the retained YAML. Real Subiquity execution and memory sizing still need qualification. |
| Unattend | WinPE firmware probe selects native BIOS or UEFI disk commands. Disk0/partition3 is pinned in both the partition hook and installation target. One first-logon command chains firstboot and cleanup. | Tests parse XML, verify each partition command list and check Windows media editions. They do not run Windows Setup or certify older Windows OpenSSH support. |
| ESXi ks | Native whole-disk firstdisk selection, DHCP, encrypted root password and one firstboot chain. Packages are qualified individual VIB URLs. SSH binds root. BIOS menu delivery is rejected. | Tests check rendered lifecycle and shell syntax. They do not certify real ESXi applets, persistence, driver compatibility or Secure Boot. |

The compiled shared/linux-sshkey.sh implementation now serves Preseed only. Its test replaces NSS lookup and verifies actual key bytes, ownership and 0700/0600 permissions in Linux. The account and SSH usernames must match. Credentials have no public defaults; values in golden files are fixtures.

Package lists come from the frozen packages parameter. The Linux default is curl, systemd and openssh-server. Replacing that parameter replaces the list; no hidden base packages are added.

## Reproducing checks

Run the builtin Go package tests with PostgreSQL test credentials and Linux shell tools configured through LOBER_TEST_BASH and LOBER_TEST_BUSYBOX. Historical database replay needs the same test database setup as the repository's other persistence tests.

With PyYAML available, run `python internal/infrastructure/builtin/testdata/native_validate.py` to parse the current Ubuntu golden and check selected native YAML values. This is a development-only parser check, with no network or production dependency.

The [renderer runtime contract](../../../domain/render/testdata/runtime-contract.md) and [menu/aux contract](../../../domain/render/testdata/menu-aux-contract.md) describe the shared delivery behavior. Actual installations still need to verify installer stage order, boot partitions, package availability, network access, account/SSH behavior, firstboot services and cleanup on each supported system. A passing golden or parser check does not prove an installation succeeded.
