# Decoded answer scaffold evidence

Readable payloads for all 20 fixtures; these are the actual bytes carried by each encoded command, not hashes or pseudocode.

## kickstart-full-bios.golden

Golden line 16, command 1:

```sh
set -eu
umask 077
mkdir -p '/tmp'
curl -fsS --retry 3 -o '/tmp/script-2.sh' 'http://gw.internal/v1/answer/task-42/install-tok-fixed/scripts/2.sh'
sh '/tmp/script-2.sh'
```

Golden line 27, command 1:

```sh
set -eu
umask 077
mkdir -p '/tmp'
curl -fsS --retry 3 -o '/tmp/script-3.sh' 'http://gw.internal/v1/answer/task-42/install-tok-fixed/scripts/3.sh'
sh '/tmp/script-3.sh'
```

Golden line 28, command 1:

```sh
set -eu
umask 077
mkdir -p '/mnt/sysroot/var/lib/lober'
curl -fsS --retry 3 -o '/mnt/sysroot/var/lib/lober/script-4.sh' 'http://gw.internal/v1/answer/task-42/install-tok-fixed/scripts/4.sh'
chroot '/mnt/sysroot' sh '/var/lib/lober/script-4.sh'
```

Golden line 29, command 1:

```sh
set -eu
umask 077
mkdir -p '/mnt/sysroot/var/lib/lober'
curl -fsS --retry 3 -o '/mnt/sysroot/var/lib/lober/script-5.sh' 'http://gw.internal/v1/answer/task-42/install-tok-fixed/scripts/5.sh'
curl -fsS --retry 3 -o '/mnt/sysroot/var/lib/lober/hook-3ee049e591f5e99ae8f498ae381f459365c221e41e6b42f2ad6e9a6a9750532e.sh' 'http://gw.internal/v1/answer/task-42/install-tok-fixed/hooks/3ee049e591f5e99ae8f498ae381f459365c221e41e6b42f2ad6e9a6a9750532e'
```

Golden line 30, command 1:

```sh
set -eu
umask 077
mkdir -p '/mnt/sysroot/var/lib/lober' '/mnt/sysroot/etc/systemd/system/multi-user.target.wants'
printf '%s' 'finalize-tok-fixed' > '/mnt/sysroot/var/lib/lober/finalize.token'
cat > '/mnt/sysroot/var/lib/lober/firstboot.sh' <<'LOBER_FIRSTBOOT_EOF'
#!/bin/sh
set -eu
TOKEN=$(cat /var/lib/lober/finalize.token)
curl -fsS -m 10 -X POST 'http://gw.internal/v1/signal/task-42/'"$TOKEN"'/booted' --data-binary '' >/dev/null 2>&1 || :
TOKEN=$(cat /var/lib/lober/finalize.token)
curl -fsS -m 10 -X POST 'http://gw.internal/v1/signal/task-42/'"$TOKEN"'/init-start' --data-binary '' >/dev/null 2>&1 || :
sh /var/lib/lober/script-5.sh
sh /var/lib/lober/cleanup.sh
LOBER_FIRSTBOOT_EOF
cat > '/mnt/sysroot/etc/systemd/system/lober-firstboot.service' <<'LOBER_UNIT_EOF'
[Unit]
Description=Lober first boot
After=network-online.target
Wants=network-online.target
ConditionPathExists=/var/lib/lober/firstboot.pending
[Service]
Type=oneshot
ExecStart=/bin/sh /var/lib/lober/firstboot.sh
[Install]
WantedBy=multi-user.target
LOBER_UNIT_EOF
ln -sf ../lober-firstboot.service '/mnt/sysroot/etc/systemd/system/multi-user.target.wants/lober-firstboot.service'
touch '/mnt/sysroot/var/lib/lober/firstboot.pending'
```

Golden line 31, command 1:

```sh
set -eu
umask 077
mkdir -p '/mnt/sysroot/var/lib/lober'
cat > '/mnt/sysroot/var/lib/lober/cleanup.sh' <<'LOBER_CLEANUP_EOF'
#!/bin/sh
set -eu
TOKEN=$(cat /var/lib/lober/finalize.token)
curl -fsS -m 10 -X POST 'http://gw.internal/v1/signal/task-42/'"$TOKEN"'/init-done' --data-binary '' >/dev/null 2>&1 || :
TOKEN=$(cat /var/lib/lober/finalize.token)
curl -fsS -m 10 -X POST 'http://gw.internal/v1/signal/task-42/'"$TOKEN"'/ready' --data-binary '' >/dev/null 2>&1 || :
rm -f /var/lib/lober/finalize.token /var/lib/lober/firstboot.pending /var/lib/lober/script-*.sh /var/lib/lober/hook-* /var/lib/lober/firstboot.sh /var/lib/lober/cleanup.sh
rm -f /root/anaconda-ks.cfg /root/original-ks.cfg /var/log/installer/autoinstall-user-data /var/log/installer/autoinstall-user-data.txt /var/log/installer/cdebconf/questions.dat /var/adm/autoinstall/cache/installedSystem.xml
LOBER_CLEANUP_EOF
```

## kickstart-full-uefi.golden

Golden line 16, command 1:

```sh
set -eu
umask 077
mkdir -p '/tmp'
curl -fsS --retry 3 -o '/tmp/script-2.sh' 'http://gw.internal/v1/answer/task-42/install-tok-fixed/scripts/2.sh'
sh '/tmp/script-2.sh'
```

Golden line 27, command 1:

```sh
set -eu
umask 077
mkdir -p '/tmp'
curl -fsS --retry 3 -o '/tmp/script-3.sh' 'http://gw.internal/v1/answer/task-42/install-tok-fixed/scripts/3.sh'
sh '/tmp/script-3.sh'
```

Golden line 28, command 1:

```sh
set -eu
umask 077
mkdir -p '/mnt/sysroot/var/lib/lober'
curl -fsS --retry 3 -o '/mnt/sysroot/var/lib/lober/script-4.sh' 'http://gw.internal/v1/answer/task-42/install-tok-fixed/scripts/4.sh'
chroot '/mnt/sysroot' sh '/var/lib/lober/script-4.sh'
```

Golden line 29, command 1:

```sh
set -eu
umask 077
mkdir -p '/mnt/sysroot/var/lib/lober'
curl -fsS --retry 3 -o '/mnt/sysroot/var/lib/lober/script-5.sh' 'http://gw.internal/v1/answer/task-42/install-tok-fixed/scripts/5.sh'
curl -fsS --retry 3 -o '/mnt/sysroot/var/lib/lober/hook-3ee049e591f5e99ae8f498ae381f459365c221e41e6b42f2ad6e9a6a9750532e.sh' 'http://gw.internal/v1/answer/task-42/install-tok-fixed/hooks/3ee049e591f5e99ae8f498ae381f459365c221e41e6b42f2ad6e9a6a9750532e'
```

Golden line 30, command 1:

```sh
set -eu
umask 077
mkdir -p '/mnt/sysroot/var/lib/lober' '/mnt/sysroot/etc/systemd/system/multi-user.target.wants'
printf '%s' 'finalize-tok-fixed' > '/mnt/sysroot/var/lib/lober/finalize.token'
cat > '/mnt/sysroot/var/lib/lober/firstboot.sh' <<'LOBER_FIRSTBOOT_EOF'
#!/bin/sh
set -eu
TOKEN=$(cat /var/lib/lober/finalize.token)
curl -fsS -m 10 -X POST 'http://gw.internal/v1/signal/task-42/'"$TOKEN"'/booted' --data-binary '' >/dev/null 2>&1 || :
TOKEN=$(cat /var/lib/lober/finalize.token)
curl -fsS -m 10 -X POST 'http://gw.internal/v1/signal/task-42/'"$TOKEN"'/init-start' --data-binary '' >/dev/null 2>&1 || :
sh /var/lib/lober/script-5.sh
sh /var/lib/lober/cleanup.sh
LOBER_FIRSTBOOT_EOF
cat > '/mnt/sysroot/etc/systemd/system/lober-firstboot.service' <<'LOBER_UNIT_EOF'
[Unit]
Description=Lober first boot
After=network-online.target
Wants=network-online.target
ConditionPathExists=/var/lib/lober/firstboot.pending
[Service]
Type=oneshot
ExecStart=/bin/sh /var/lib/lober/firstboot.sh
[Install]
WantedBy=multi-user.target
LOBER_UNIT_EOF
ln -sf ../lober-firstboot.service '/mnt/sysroot/etc/systemd/system/multi-user.target.wants/lober-firstboot.service'
touch '/mnt/sysroot/var/lib/lober/firstboot.pending'
```

Golden line 31, command 1:

```sh
set -eu
umask 077
mkdir -p '/mnt/sysroot/var/lib/lober'
cat > '/mnt/sysroot/var/lib/lober/cleanup.sh' <<'LOBER_CLEANUP_EOF'
#!/bin/sh
set -eu
TOKEN=$(cat /var/lib/lober/finalize.token)
curl -fsS -m 10 -X POST 'http://gw.internal/v1/signal/task-42/'"$TOKEN"'/init-done' --data-binary '' >/dev/null 2>&1 || :
TOKEN=$(cat /var/lib/lober/finalize.token)
curl -fsS -m 10 -X POST 'http://gw.internal/v1/signal/task-42/'"$TOKEN"'/ready' --data-binary '' >/dev/null 2>&1 || :
rm -f /var/lib/lober/finalize.token /var/lib/lober/firstboot.pending /var/lib/lober/script-*.sh /var/lib/lober/hook-* /var/lib/lober/firstboot.sh /var/lib/lober/cleanup.sh
rm -f /root/anaconda-ks.cfg /root/original-ks.cfg /var/log/installer/autoinstall-user-data /var/log/installer/autoinstall-user-data.txt /var/log/installer/cdebconf/questions.dat /var/adm/autoinstall/cache/installedSystem.xml
LOBER_CLEANUP_EOF
```

## kickstart-minimal-bios.golden

Golden line 16, command 1:

```sh
set -eu
umask 077
mkdir -p '/tmp'
curl -fsS --retry 3 -o '/tmp/script-2.sh' 'http://gw.internal/v1/answer/task-42/install-tok-fixed/scripts/2.sh'
sh '/tmp/script-2.sh'
```

Golden line 27, command 1:

```sh
set -eu
umask 077
mkdir -p '/tmp'
curl -fsS --retry 3 -o '/tmp/script-3.sh' 'http://gw.internal/v1/answer/task-42/install-tok-fixed/scripts/3.sh'
sh '/tmp/script-3.sh'
```

Golden line 28, command 1:

```sh
set -eu
umask 077
mkdir -p '/mnt/sysroot/var/lib/lober'
curl -fsS --retry 3 -o '/mnt/sysroot/var/lib/lober/script-4.sh' 'http://gw.internal/v1/answer/task-42/install-tok-fixed/scripts/4.sh'
chroot '/mnt/sysroot' sh '/var/lib/lober/script-4.sh'
```

Golden line 29, command 1:

```sh
set -eu
umask 077
mkdir -p '/mnt/sysroot/var/lib/lober'
curl -fsS --retry 3 -o '/mnt/sysroot/var/lib/lober/script-5.sh' 'http://gw.internal/v1/answer/task-42/install-tok-fixed/scripts/5.sh'
```

Golden line 30, command 1:

```sh
set -eu
umask 077
mkdir -p '/mnt/sysroot/var/lib/lober' '/mnt/sysroot/etc/systemd/system/multi-user.target.wants'
printf '%s' 'finalize-tok-fixed' > '/mnt/sysroot/var/lib/lober/finalize.token'
cat > '/mnt/sysroot/var/lib/lober/firstboot.sh' <<'LOBER_FIRSTBOOT_EOF'
#!/bin/sh
set -eu
TOKEN=$(cat /var/lib/lober/finalize.token)
curl -fsS -m 10 -X POST 'http://gw.internal/v1/signal/task-42/'"$TOKEN"'/booted' --data-binary '' >/dev/null 2>&1 || :
TOKEN=$(cat /var/lib/lober/finalize.token)
curl -fsS -m 10 -X POST 'http://gw.internal/v1/signal/task-42/'"$TOKEN"'/init-start' --data-binary '' >/dev/null 2>&1 || :
sh /var/lib/lober/script-5.sh
sh /var/lib/lober/cleanup.sh
LOBER_FIRSTBOOT_EOF
cat > '/mnt/sysroot/etc/systemd/system/lober-firstboot.service' <<'LOBER_UNIT_EOF'
[Unit]
Description=Lober first boot
After=network-online.target
Wants=network-online.target
ConditionPathExists=/var/lib/lober/firstboot.pending
[Service]
Type=oneshot
ExecStart=/bin/sh /var/lib/lober/firstboot.sh
[Install]
WantedBy=multi-user.target
LOBER_UNIT_EOF
ln -sf ../lober-firstboot.service '/mnt/sysroot/etc/systemd/system/multi-user.target.wants/lober-firstboot.service'
touch '/mnt/sysroot/var/lib/lober/firstboot.pending'
```

Golden line 31, command 1:

```sh
set -eu
umask 077
mkdir -p '/mnt/sysroot/var/lib/lober'
cat > '/mnt/sysroot/var/lib/lober/cleanup.sh' <<'LOBER_CLEANUP_EOF'
#!/bin/sh
set -eu
TOKEN=$(cat /var/lib/lober/finalize.token)
curl -fsS -m 10 -X POST 'http://gw.internal/v1/signal/task-42/'"$TOKEN"'/init-done' --data-binary '' >/dev/null 2>&1 || :
TOKEN=$(cat /var/lib/lober/finalize.token)
curl -fsS -m 10 -X POST 'http://gw.internal/v1/signal/task-42/'"$TOKEN"'/ready' --data-binary '' >/dev/null 2>&1 || :
rm -f /var/lib/lober/finalize.token /var/lib/lober/firstboot.pending /var/lib/lober/script-*.sh /var/lib/lober/hook-* /var/lib/lober/firstboot.sh /var/lib/lober/cleanup.sh
rm -f /root/anaconda-ks.cfg /root/original-ks.cfg /var/log/installer/autoinstall-user-data /var/log/installer/autoinstall-user-data.txt /var/log/installer/cdebconf/questions.dat /var/adm/autoinstall/cache/installedSystem.xml
LOBER_CLEANUP_EOF
```

## kickstart-minimal-uefi.golden

Golden line 16, command 1:

```sh
set -eu
umask 077
mkdir -p '/tmp'
curl -fsS --retry 3 -o '/tmp/script-2.sh' 'http://gw.internal/v1/answer/task-42/install-tok-fixed/scripts/2.sh'
sh '/tmp/script-2.sh'
```

Golden line 27, command 1:

```sh
set -eu
umask 077
mkdir -p '/tmp'
curl -fsS --retry 3 -o '/tmp/script-3.sh' 'http://gw.internal/v1/answer/task-42/install-tok-fixed/scripts/3.sh'
sh '/tmp/script-3.sh'
```

Golden line 28, command 1:

```sh
set -eu
umask 077
mkdir -p '/mnt/sysroot/var/lib/lober'
curl -fsS --retry 3 -o '/mnt/sysroot/var/lib/lober/script-4.sh' 'http://gw.internal/v1/answer/task-42/install-tok-fixed/scripts/4.sh'
chroot '/mnt/sysroot' sh '/var/lib/lober/script-4.sh'
```

Golden line 29, command 1:

```sh
set -eu
umask 077
mkdir -p '/mnt/sysroot/var/lib/lober'
curl -fsS --retry 3 -o '/mnt/sysroot/var/lib/lober/script-5.sh' 'http://gw.internal/v1/answer/task-42/install-tok-fixed/scripts/5.sh'
```

Golden line 30, command 1:

```sh
set -eu
umask 077
mkdir -p '/mnt/sysroot/var/lib/lober' '/mnt/sysroot/etc/systemd/system/multi-user.target.wants'
printf '%s' 'finalize-tok-fixed' > '/mnt/sysroot/var/lib/lober/finalize.token'
cat > '/mnt/sysroot/var/lib/lober/firstboot.sh' <<'LOBER_FIRSTBOOT_EOF'
#!/bin/sh
set -eu
TOKEN=$(cat /var/lib/lober/finalize.token)
curl -fsS -m 10 -X POST 'http://gw.internal/v1/signal/task-42/'"$TOKEN"'/booted' --data-binary '' >/dev/null 2>&1 || :
TOKEN=$(cat /var/lib/lober/finalize.token)
curl -fsS -m 10 -X POST 'http://gw.internal/v1/signal/task-42/'"$TOKEN"'/init-start' --data-binary '' >/dev/null 2>&1 || :
sh /var/lib/lober/script-5.sh
sh /var/lib/lober/cleanup.sh
LOBER_FIRSTBOOT_EOF
cat > '/mnt/sysroot/etc/systemd/system/lober-firstboot.service' <<'LOBER_UNIT_EOF'
[Unit]
Description=Lober first boot
After=network-online.target
Wants=network-online.target
ConditionPathExists=/var/lib/lober/firstboot.pending
[Service]
Type=oneshot
ExecStart=/bin/sh /var/lib/lober/firstboot.sh
[Install]
WantedBy=multi-user.target
LOBER_UNIT_EOF
ln -sf ../lober-firstboot.service '/mnt/sysroot/etc/systemd/system/multi-user.target.wants/lober-firstboot.service'
touch '/mnt/sysroot/var/lib/lober/firstboot.pending'
```

Golden line 31, command 1:

```sh
set -eu
umask 077
mkdir -p '/mnt/sysroot/var/lib/lober'
cat > '/mnt/sysroot/var/lib/lober/cleanup.sh' <<'LOBER_CLEANUP_EOF'
#!/bin/sh
set -eu
TOKEN=$(cat /var/lib/lober/finalize.token)
curl -fsS -m 10 -X POST 'http://gw.internal/v1/signal/task-42/'"$TOKEN"'/init-done' --data-binary '' >/dev/null 2>&1 || :
TOKEN=$(cat /var/lib/lober/finalize.token)
curl -fsS -m 10 -X POST 'http://gw.internal/v1/signal/task-42/'"$TOKEN"'/ready' --data-binary '' >/dev/null 2>&1 || :
rm -f /var/lib/lober/finalize.token /var/lib/lober/firstboot.pending /var/lib/lober/script-*.sh /var/lib/lober/hook-* /var/lib/lober/firstboot.sh /var/lib/lober/cleanup.sh
rm -f /root/anaconda-ks.cfg /root/original-ks.cfg /var/log/installer/autoinstall-user-data /var/log/installer/autoinstall-user-data.txt /var/log/installer/cdebconf/questions.dat /var/adm/autoinstall/cache/installedSystem.xml
LOBER_CLEANUP_EOF
```

## preseed-full-bios.golden

Golden line 24, command 1:

```sh
set -eu
umask 077
mkdir -p '/tmp'
wget -O '/tmp/script-2.sh' 'http://gw.internal/v1/answer/task-42/install-tok-fixed/scripts/2.sh'
sh '/tmp/script-2.sh'
```

Golden line 25, command 1:

```sh
set -eu
umask 077
mkdir -p '/tmp'
wget -O '/tmp/script-3.sh' 'http://gw.internal/v1/answer/task-42/install-tok-fixed/scripts/3.sh'
sh '/tmp/script-3.sh'
```

Golden line 25, command 2:

```sh
set -eu
umask 077
mkdir -p '/target/var/lib/lober'
wget -O '/target/var/lib/lober/script-4.sh' 'http://gw.internal/v1/answer/task-42/install-tok-fixed/scripts/4.sh'
chroot '/target' sh '/var/lib/lober/script-4.sh'
```

Golden line 25, command 3:

```sh
set -eu
umask 077
mkdir -p '/target/var/lib/lober'
wget -O '/target/var/lib/lober/script-5.sh' 'http://gw.internal/v1/answer/task-42/install-tok-fixed/scripts/5.sh'
wget -O '/target/var/lib/lober/hook-3ee049e591f5e99ae8f498ae381f459365c221e41e6b42f2ad6e9a6a9750532e.sh' 'http://gw.internal/v1/answer/task-42/install-tok-fixed/hooks/3ee049e591f5e99ae8f498ae381f459365c221e41e6b42f2ad6e9a6a9750532e'
```

Golden line 25, command 4:

```sh
set -eu
umask 077
mkdir -p '/target/var/lib/lober' '/target/etc/systemd/system/multi-user.target.wants'
printf '%s' 'finalize-tok-fixed' > '/target/var/lib/lober/finalize.token'
cat > '/target/var/lib/lober/firstboot.sh' <<'LOBER_FIRSTBOOT_EOF'
#!/bin/sh
set -eu
TOKEN=$(cat /var/lib/lober/finalize.token)
curl -fsS -m 10 -X POST 'http://gw.internal/v1/signal/task-42/'"$TOKEN"'/booted' --data-binary '' >/dev/null 2>&1 || :
TOKEN=$(cat /var/lib/lober/finalize.token)
curl -fsS -m 10 -X POST 'http://gw.internal/v1/signal/task-42/'"$TOKEN"'/init-start' --data-binary '' >/dev/null 2>&1 || :
sh /var/lib/lober/script-5.sh
sh /var/lib/lober/cleanup.sh
LOBER_FIRSTBOOT_EOF
cat > '/target/etc/systemd/system/lober-firstboot.service' <<'LOBER_UNIT_EOF'
[Unit]
Description=Lober first boot
After=network-online.target
Wants=network-online.target
ConditionPathExists=/var/lib/lober/firstboot.pending
[Service]
Type=oneshot
ExecStart=/bin/sh /var/lib/lober/firstboot.sh
[Install]
WantedBy=multi-user.target
LOBER_UNIT_EOF
ln -sf ../lober-firstboot.service '/target/etc/systemd/system/multi-user.target.wants/lober-firstboot.service'
touch '/target/var/lib/lober/firstboot.pending'
```

Golden line 25, command 5:

```sh
set -eu
umask 077
mkdir -p '/target/var/lib/lober'
cat > '/target/var/lib/lober/cleanup.sh' <<'LOBER_CLEANUP_EOF'
#!/bin/sh
set -eu
TOKEN=$(cat /var/lib/lober/finalize.token)
curl -fsS -m 10 -X POST 'http://gw.internal/v1/signal/task-42/'"$TOKEN"'/init-done' --data-binary '' >/dev/null 2>&1 || :
TOKEN=$(cat /var/lib/lober/finalize.token)
curl -fsS -m 10 -X POST 'http://gw.internal/v1/signal/task-42/'"$TOKEN"'/ready' --data-binary '' >/dev/null 2>&1 || :
rm -f /var/lib/lober/finalize.token /var/lib/lober/firstboot.pending /var/lib/lober/script-*.sh /var/lib/lober/hook-* /var/lib/lober/firstboot.sh /var/lib/lober/cleanup.sh
rm -f /root/anaconda-ks.cfg /root/original-ks.cfg /var/log/installer/autoinstall-user-data /var/log/installer/autoinstall-user-data.txt /var/log/installer/cdebconf/questions.dat /var/adm/autoinstall/cache/installedSystem.xml
LOBER_CLEANUP_EOF
```

## preseed-full-uefi.golden

Golden line 24, command 1:

```sh
set -eu
umask 077
mkdir -p '/tmp'
wget -O '/tmp/script-2.sh' 'http://gw.internal/v1/answer/task-42/install-tok-fixed/scripts/2.sh'
sh '/tmp/script-2.sh'
```

Golden line 25, command 1:

```sh
set -eu
umask 077
mkdir -p '/tmp'
wget -O '/tmp/script-3.sh' 'http://gw.internal/v1/answer/task-42/install-tok-fixed/scripts/3.sh'
sh '/tmp/script-3.sh'
```

Golden line 25, command 2:

```sh
set -eu
umask 077
mkdir -p '/target/var/lib/lober'
wget -O '/target/var/lib/lober/script-4.sh' 'http://gw.internal/v1/answer/task-42/install-tok-fixed/scripts/4.sh'
chroot '/target' sh '/var/lib/lober/script-4.sh'
```

Golden line 25, command 3:

```sh
set -eu
umask 077
mkdir -p '/target/var/lib/lober'
wget -O '/target/var/lib/lober/script-5.sh' 'http://gw.internal/v1/answer/task-42/install-tok-fixed/scripts/5.sh'
wget -O '/target/var/lib/lober/hook-3ee049e591f5e99ae8f498ae381f459365c221e41e6b42f2ad6e9a6a9750532e.sh' 'http://gw.internal/v1/answer/task-42/install-tok-fixed/hooks/3ee049e591f5e99ae8f498ae381f459365c221e41e6b42f2ad6e9a6a9750532e'
```

Golden line 25, command 4:

```sh
set -eu
umask 077
mkdir -p '/target/var/lib/lober' '/target/etc/systemd/system/multi-user.target.wants'
printf '%s' 'finalize-tok-fixed' > '/target/var/lib/lober/finalize.token'
cat > '/target/var/lib/lober/firstboot.sh' <<'LOBER_FIRSTBOOT_EOF'
#!/bin/sh
set -eu
TOKEN=$(cat /var/lib/lober/finalize.token)
curl -fsS -m 10 -X POST 'http://gw.internal/v1/signal/task-42/'"$TOKEN"'/booted' --data-binary '' >/dev/null 2>&1 || :
TOKEN=$(cat /var/lib/lober/finalize.token)
curl -fsS -m 10 -X POST 'http://gw.internal/v1/signal/task-42/'"$TOKEN"'/init-start' --data-binary '' >/dev/null 2>&1 || :
sh /var/lib/lober/script-5.sh
sh /var/lib/lober/cleanup.sh
LOBER_FIRSTBOOT_EOF
cat > '/target/etc/systemd/system/lober-firstboot.service' <<'LOBER_UNIT_EOF'
[Unit]
Description=Lober first boot
After=network-online.target
Wants=network-online.target
ConditionPathExists=/var/lib/lober/firstboot.pending
[Service]
Type=oneshot
ExecStart=/bin/sh /var/lib/lober/firstboot.sh
[Install]
WantedBy=multi-user.target
LOBER_UNIT_EOF
ln -sf ../lober-firstboot.service '/target/etc/systemd/system/multi-user.target.wants/lober-firstboot.service'
touch '/target/var/lib/lober/firstboot.pending'
```

Golden line 25, command 5:

```sh
set -eu
umask 077
mkdir -p '/target/var/lib/lober'
cat > '/target/var/lib/lober/cleanup.sh' <<'LOBER_CLEANUP_EOF'
#!/bin/sh
set -eu
TOKEN=$(cat /var/lib/lober/finalize.token)
curl -fsS -m 10 -X POST 'http://gw.internal/v1/signal/task-42/'"$TOKEN"'/init-done' --data-binary '' >/dev/null 2>&1 || :
TOKEN=$(cat /var/lib/lober/finalize.token)
curl -fsS -m 10 -X POST 'http://gw.internal/v1/signal/task-42/'"$TOKEN"'/ready' --data-binary '' >/dev/null 2>&1 || :
rm -f /var/lib/lober/finalize.token /var/lib/lober/firstboot.pending /var/lib/lober/script-*.sh /var/lib/lober/hook-* /var/lib/lober/firstboot.sh /var/lib/lober/cleanup.sh
rm -f /root/anaconda-ks.cfg /root/original-ks.cfg /var/log/installer/autoinstall-user-data /var/log/installer/autoinstall-user-data.txt /var/log/installer/cdebconf/questions.dat /var/adm/autoinstall/cache/installedSystem.xml
LOBER_CLEANUP_EOF
```

## preseed-minimal-bios.golden

Golden line 24, command 1:

```sh
set -eu
umask 077
mkdir -p '/tmp'
wget -O '/tmp/script-2.sh' 'http://gw.internal/v1/answer/task-42/install-tok-fixed/scripts/2.sh'
sh '/tmp/script-2.sh'
```

Golden line 25, command 1:

```sh
set -eu
umask 077
mkdir -p '/tmp'
wget -O '/tmp/script-3.sh' 'http://gw.internal/v1/answer/task-42/install-tok-fixed/scripts/3.sh'
sh '/tmp/script-3.sh'
```

Golden line 25, command 2:

```sh
set -eu
umask 077
mkdir -p '/target/var/lib/lober'
wget -O '/target/var/lib/lober/script-4.sh' 'http://gw.internal/v1/answer/task-42/install-tok-fixed/scripts/4.sh'
chroot '/target' sh '/var/lib/lober/script-4.sh'
```

Golden line 25, command 3:

```sh
set -eu
umask 077
mkdir -p '/target/var/lib/lober'
wget -O '/target/var/lib/lober/script-5.sh' 'http://gw.internal/v1/answer/task-42/install-tok-fixed/scripts/5.sh'
```

Golden line 25, command 4:

```sh
set -eu
umask 077
mkdir -p '/target/var/lib/lober' '/target/etc/systemd/system/multi-user.target.wants'
printf '%s' 'finalize-tok-fixed' > '/target/var/lib/lober/finalize.token'
cat > '/target/var/lib/lober/firstboot.sh' <<'LOBER_FIRSTBOOT_EOF'
#!/bin/sh
set -eu
TOKEN=$(cat /var/lib/lober/finalize.token)
curl -fsS -m 10 -X POST 'http://gw.internal/v1/signal/task-42/'"$TOKEN"'/booted' --data-binary '' >/dev/null 2>&1 || :
TOKEN=$(cat /var/lib/lober/finalize.token)
curl -fsS -m 10 -X POST 'http://gw.internal/v1/signal/task-42/'"$TOKEN"'/init-start' --data-binary '' >/dev/null 2>&1 || :
sh /var/lib/lober/script-5.sh
sh /var/lib/lober/cleanup.sh
LOBER_FIRSTBOOT_EOF
cat > '/target/etc/systemd/system/lober-firstboot.service' <<'LOBER_UNIT_EOF'
[Unit]
Description=Lober first boot
After=network-online.target
Wants=network-online.target
ConditionPathExists=/var/lib/lober/firstboot.pending
[Service]
Type=oneshot
ExecStart=/bin/sh /var/lib/lober/firstboot.sh
[Install]
WantedBy=multi-user.target
LOBER_UNIT_EOF
ln -sf ../lober-firstboot.service '/target/etc/systemd/system/multi-user.target.wants/lober-firstboot.service'
touch '/target/var/lib/lober/firstboot.pending'
```

Golden line 25, command 5:

```sh
set -eu
umask 077
mkdir -p '/target/var/lib/lober'
cat > '/target/var/lib/lober/cleanup.sh' <<'LOBER_CLEANUP_EOF'
#!/bin/sh
set -eu
TOKEN=$(cat /var/lib/lober/finalize.token)
curl -fsS -m 10 -X POST 'http://gw.internal/v1/signal/task-42/'"$TOKEN"'/init-done' --data-binary '' >/dev/null 2>&1 || :
TOKEN=$(cat /var/lib/lober/finalize.token)
curl -fsS -m 10 -X POST 'http://gw.internal/v1/signal/task-42/'"$TOKEN"'/ready' --data-binary '' >/dev/null 2>&1 || :
rm -f /var/lib/lober/finalize.token /var/lib/lober/firstboot.pending /var/lib/lober/script-*.sh /var/lib/lober/hook-* /var/lib/lober/firstboot.sh /var/lib/lober/cleanup.sh
rm -f /root/anaconda-ks.cfg /root/original-ks.cfg /var/log/installer/autoinstall-user-data /var/log/installer/autoinstall-user-data.txt /var/log/installer/cdebconf/questions.dat /var/adm/autoinstall/cache/installedSystem.xml
LOBER_CLEANUP_EOF
```

## preseed-minimal-uefi.golden

Golden line 24, command 1:

```sh
set -eu
umask 077
mkdir -p '/tmp'
wget -O '/tmp/script-2.sh' 'http://gw.internal/v1/answer/task-42/install-tok-fixed/scripts/2.sh'
sh '/tmp/script-2.sh'
```

Golden line 25, command 1:

```sh
set -eu
umask 077
mkdir -p '/tmp'
wget -O '/tmp/script-3.sh' 'http://gw.internal/v1/answer/task-42/install-tok-fixed/scripts/3.sh'
sh '/tmp/script-3.sh'
```

Golden line 25, command 2:

```sh
set -eu
umask 077
mkdir -p '/target/var/lib/lober'
wget -O '/target/var/lib/lober/script-4.sh' 'http://gw.internal/v1/answer/task-42/install-tok-fixed/scripts/4.sh'
chroot '/target' sh '/var/lib/lober/script-4.sh'
```

Golden line 25, command 3:

```sh
set -eu
umask 077
mkdir -p '/target/var/lib/lober'
wget -O '/target/var/lib/lober/script-5.sh' 'http://gw.internal/v1/answer/task-42/install-tok-fixed/scripts/5.sh'
```

Golden line 25, command 4:

```sh
set -eu
umask 077
mkdir -p '/target/var/lib/lober' '/target/etc/systemd/system/multi-user.target.wants'
printf '%s' 'finalize-tok-fixed' > '/target/var/lib/lober/finalize.token'
cat > '/target/var/lib/lober/firstboot.sh' <<'LOBER_FIRSTBOOT_EOF'
#!/bin/sh
set -eu
TOKEN=$(cat /var/lib/lober/finalize.token)
curl -fsS -m 10 -X POST 'http://gw.internal/v1/signal/task-42/'"$TOKEN"'/booted' --data-binary '' >/dev/null 2>&1 || :
TOKEN=$(cat /var/lib/lober/finalize.token)
curl -fsS -m 10 -X POST 'http://gw.internal/v1/signal/task-42/'"$TOKEN"'/init-start' --data-binary '' >/dev/null 2>&1 || :
sh /var/lib/lober/script-5.sh
sh /var/lib/lober/cleanup.sh
LOBER_FIRSTBOOT_EOF
cat > '/target/etc/systemd/system/lober-firstboot.service' <<'LOBER_UNIT_EOF'
[Unit]
Description=Lober first boot
After=network-online.target
Wants=network-online.target
ConditionPathExists=/var/lib/lober/firstboot.pending
[Service]
Type=oneshot
ExecStart=/bin/sh /var/lib/lober/firstboot.sh
[Install]
WantedBy=multi-user.target
LOBER_UNIT_EOF
ln -sf ../lober-firstboot.service '/target/etc/systemd/system/multi-user.target.wants/lober-firstboot.service'
touch '/target/var/lib/lober/firstboot.pending'
```

Golden line 25, command 5:

```sh
set -eu
umask 077
mkdir -p '/target/var/lib/lober'
cat > '/target/var/lib/lober/cleanup.sh' <<'LOBER_CLEANUP_EOF'
#!/bin/sh
set -eu
TOKEN=$(cat /var/lib/lober/finalize.token)
curl -fsS -m 10 -X POST 'http://gw.internal/v1/signal/task-42/'"$TOKEN"'/init-done' --data-binary '' >/dev/null 2>&1 || :
TOKEN=$(cat /var/lib/lober/finalize.token)
curl -fsS -m 10 -X POST 'http://gw.internal/v1/signal/task-42/'"$TOKEN"'/ready' --data-binary '' >/dev/null 2>&1 || :
rm -f /var/lib/lober/finalize.token /var/lib/lober/firstboot.pending /var/lib/lober/script-*.sh /var/lib/lober/hook-* /var/lib/lober/firstboot.sh /var/lib/lober/cleanup.sh
rm -f /root/anaconda-ks.cfg /root/original-ks.cfg /var/log/installer/autoinstall-user-data /var/log/installer/autoinstall-user-data.txt /var/log/installer/cdebconf/questions.dat /var/adm/autoinstall/cache/installedSystem.xml
LOBER_CLEANUP_EOF
```

## autoinstall-full-bios.golden

Golden line 26, command 1:

```sh
set -eu
umask 077
mkdir -p '/tmp'
curl -fsS --retry 3 -o '/tmp/script-2.sh' 'http://gw.internal/v1/answer/task-42/install-tok-fixed/scripts/2.sh'
sh '/tmp/script-2.sh'
```

Golden line 28, command 1:

```sh
set -eu
umask 077
mkdir -p '/tmp'
curl -fsS --retry 3 -o '/tmp/script-3.sh' 'http://gw.internal/v1/answer/task-42/install-tok-fixed/scripts/3.sh'
sh '/tmp/script-3.sh'
```

Golden line 29, command 1:

```sh
set -eu
umask 077
mkdir -p '/target/var/lib/lober'
curl -fsS --retry 3 -o '/target/var/lib/lober/script-4.sh' 'http://gw.internal/v1/answer/task-42/install-tok-fixed/scripts/4.sh'
chroot '/target' sh '/var/lib/lober/script-4.sh'
```

Golden line 30, command 1:

```sh
set -eu
umask 077
mkdir -p '/target/var/lib/lober'
curl -fsS --retry 3 -o '/target/var/lib/lober/script-5.sh' 'http://gw.internal/v1/answer/task-42/install-tok-fixed/scripts/5.sh'
curl -fsS --retry 3 -o '/target/var/lib/lober/hook-3ee049e591f5e99ae8f498ae381f459365c221e41e6b42f2ad6e9a6a9750532e.sh' 'http://gw.internal/v1/answer/task-42/install-tok-fixed/hooks/3ee049e591f5e99ae8f498ae381f459365c221e41e6b42f2ad6e9a6a9750532e'
```

Golden line 31, command 1:

```sh
set -eu
umask 077
mkdir -p '/target/var/lib/lober' '/target/etc/systemd/system/multi-user.target.wants'
printf '%s' 'finalize-tok-fixed' > '/target/var/lib/lober/finalize.token'
cat > '/target/var/lib/lober/firstboot.sh' <<'LOBER_FIRSTBOOT_EOF'
#!/bin/sh
set -eu
TOKEN=$(cat /var/lib/lober/finalize.token)
curl -fsS -m 10 -X POST 'http://gw.internal/v1/signal/task-42/'"$TOKEN"'/booted' --data-binary '' >/dev/null 2>&1 || :
TOKEN=$(cat /var/lib/lober/finalize.token)
curl -fsS -m 10 -X POST 'http://gw.internal/v1/signal/task-42/'"$TOKEN"'/init-start' --data-binary '' >/dev/null 2>&1 || :
sh /var/lib/lober/script-5.sh
sh /var/lib/lober/cleanup.sh
LOBER_FIRSTBOOT_EOF
cat > '/target/etc/systemd/system/lober-firstboot.service' <<'LOBER_UNIT_EOF'
[Unit]
Description=Lober first boot
After=network-online.target
Wants=network-online.target
ConditionPathExists=/var/lib/lober/firstboot.pending
[Service]
Type=oneshot
ExecStart=/bin/sh /var/lib/lober/firstboot.sh
[Install]
WantedBy=multi-user.target
LOBER_UNIT_EOF
ln -sf ../lober-firstboot.service '/target/etc/systemd/system/multi-user.target.wants/lober-firstboot.service'
touch '/target/var/lib/lober/firstboot.pending'
```

Golden line 32, command 1:

```sh
set -eu
umask 077
mkdir -p '/target/var/lib/lober'
cat > '/target/var/lib/lober/cleanup.sh' <<'LOBER_CLEANUP_EOF'
#!/bin/sh
set -eu
TOKEN=$(cat /var/lib/lober/finalize.token)
curl -fsS -m 10 -X POST 'http://gw.internal/v1/signal/task-42/'"$TOKEN"'/init-done' --data-binary '' >/dev/null 2>&1 || :
TOKEN=$(cat /var/lib/lober/finalize.token)
curl -fsS -m 10 -X POST 'http://gw.internal/v1/signal/task-42/'"$TOKEN"'/ready' --data-binary '' >/dev/null 2>&1 || :
rm -f /var/lib/lober/finalize.token /var/lib/lober/firstboot.pending /var/lib/lober/script-*.sh /var/lib/lober/hook-* /var/lib/lober/firstboot.sh /var/lib/lober/cleanup.sh
rm -f /root/anaconda-ks.cfg /root/original-ks.cfg /var/log/installer/autoinstall-user-data /var/log/installer/autoinstall-user-data.txt /var/log/installer/cdebconf/questions.dat /var/adm/autoinstall/cache/installedSystem.xml
LOBER_CLEANUP_EOF
```

## autoinstall-full-uefi.golden

Golden line 28, command 1:

```sh
set -eu
umask 077
mkdir -p '/tmp'
curl -fsS --retry 3 -o '/tmp/script-2.sh' 'http://gw.internal/v1/answer/task-42/install-tok-fixed/scripts/2.sh'
sh '/tmp/script-2.sh'
```

Golden line 30, command 1:

```sh
set -eu
umask 077
mkdir -p '/tmp'
curl -fsS --retry 3 -o '/tmp/script-3.sh' 'http://gw.internal/v1/answer/task-42/install-tok-fixed/scripts/3.sh'
sh '/tmp/script-3.sh'
```

Golden line 31, command 1:

```sh
set -eu
umask 077
mkdir -p '/target/var/lib/lober'
curl -fsS --retry 3 -o '/target/var/lib/lober/script-4.sh' 'http://gw.internal/v1/answer/task-42/install-tok-fixed/scripts/4.sh'
chroot '/target' sh '/var/lib/lober/script-4.sh'
```

Golden line 32, command 1:

```sh
set -eu
umask 077
mkdir -p '/target/var/lib/lober'
curl -fsS --retry 3 -o '/target/var/lib/lober/script-5.sh' 'http://gw.internal/v1/answer/task-42/install-tok-fixed/scripts/5.sh'
curl -fsS --retry 3 -o '/target/var/lib/lober/hook-3ee049e591f5e99ae8f498ae381f459365c221e41e6b42f2ad6e9a6a9750532e.sh' 'http://gw.internal/v1/answer/task-42/install-tok-fixed/hooks/3ee049e591f5e99ae8f498ae381f459365c221e41e6b42f2ad6e9a6a9750532e'
```

Golden line 33, command 1:

```sh
set -eu
umask 077
mkdir -p '/target/var/lib/lober' '/target/etc/systemd/system/multi-user.target.wants'
printf '%s' 'finalize-tok-fixed' > '/target/var/lib/lober/finalize.token'
cat > '/target/var/lib/lober/firstboot.sh' <<'LOBER_FIRSTBOOT_EOF'
#!/bin/sh
set -eu
TOKEN=$(cat /var/lib/lober/finalize.token)
curl -fsS -m 10 -X POST 'http://gw.internal/v1/signal/task-42/'"$TOKEN"'/booted' --data-binary '' >/dev/null 2>&1 || :
TOKEN=$(cat /var/lib/lober/finalize.token)
curl -fsS -m 10 -X POST 'http://gw.internal/v1/signal/task-42/'"$TOKEN"'/init-start' --data-binary '' >/dev/null 2>&1 || :
sh /var/lib/lober/script-5.sh
sh /var/lib/lober/cleanup.sh
LOBER_FIRSTBOOT_EOF
cat > '/target/etc/systemd/system/lober-firstboot.service' <<'LOBER_UNIT_EOF'
[Unit]
Description=Lober first boot
After=network-online.target
Wants=network-online.target
ConditionPathExists=/var/lib/lober/firstboot.pending
[Service]
Type=oneshot
ExecStart=/bin/sh /var/lib/lober/firstboot.sh
[Install]
WantedBy=multi-user.target
LOBER_UNIT_EOF
ln -sf ../lober-firstboot.service '/target/etc/systemd/system/multi-user.target.wants/lober-firstboot.service'
touch '/target/var/lib/lober/firstboot.pending'
```

Golden line 34, command 1:

```sh
set -eu
umask 077
mkdir -p '/target/var/lib/lober'
cat > '/target/var/lib/lober/cleanup.sh' <<'LOBER_CLEANUP_EOF'
#!/bin/sh
set -eu
TOKEN=$(cat /var/lib/lober/finalize.token)
curl -fsS -m 10 -X POST 'http://gw.internal/v1/signal/task-42/'"$TOKEN"'/init-done' --data-binary '' >/dev/null 2>&1 || :
TOKEN=$(cat /var/lib/lober/finalize.token)
curl -fsS -m 10 -X POST 'http://gw.internal/v1/signal/task-42/'"$TOKEN"'/ready' --data-binary '' >/dev/null 2>&1 || :
rm -f /var/lib/lober/finalize.token /var/lib/lober/firstboot.pending /var/lib/lober/script-*.sh /var/lib/lober/hook-* /var/lib/lober/firstboot.sh /var/lib/lober/cleanup.sh
rm -f /root/anaconda-ks.cfg /root/original-ks.cfg /var/log/installer/autoinstall-user-data /var/log/installer/autoinstall-user-data.txt /var/log/installer/cdebconf/questions.dat /var/adm/autoinstall/cache/installedSystem.xml
LOBER_CLEANUP_EOF
```

## autoinstall-minimal-bios.golden

Golden line 26, command 1:

```sh
set -eu
umask 077
mkdir -p '/tmp'
curl -fsS --retry 3 -o '/tmp/script-2.sh' 'http://gw.internal/v1/answer/task-42/install-tok-fixed/scripts/2.sh'
sh '/tmp/script-2.sh'
```

Golden line 28, command 1:

```sh
set -eu
umask 077
mkdir -p '/tmp'
curl -fsS --retry 3 -o '/tmp/script-3.sh' 'http://gw.internal/v1/answer/task-42/install-tok-fixed/scripts/3.sh'
sh '/tmp/script-3.sh'
```

Golden line 29, command 1:

```sh
set -eu
umask 077
mkdir -p '/target/var/lib/lober'
curl -fsS --retry 3 -o '/target/var/lib/lober/script-4.sh' 'http://gw.internal/v1/answer/task-42/install-tok-fixed/scripts/4.sh'
chroot '/target' sh '/var/lib/lober/script-4.sh'
```

Golden line 30, command 1:

```sh
set -eu
umask 077
mkdir -p '/target/var/lib/lober'
curl -fsS --retry 3 -o '/target/var/lib/lober/script-5.sh' 'http://gw.internal/v1/answer/task-42/install-tok-fixed/scripts/5.sh'
```

Golden line 31, command 1:

```sh
set -eu
umask 077
mkdir -p '/target/var/lib/lober' '/target/etc/systemd/system/multi-user.target.wants'
printf '%s' 'finalize-tok-fixed' > '/target/var/lib/lober/finalize.token'
cat > '/target/var/lib/lober/firstboot.sh' <<'LOBER_FIRSTBOOT_EOF'
#!/bin/sh
set -eu
TOKEN=$(cat /var/lib/lober/finalize.token)
curl -fsS -m 10 -X POST 'http://gw.internal/v1/signal/task-42/'"$TOKEN"'/booted' --data-binary '' >/dev/null 2>&1 || :
TOKEN=$(cat /var/lib/lober/finalize.token)
curl -fsS -m 10 -X POST 'http://gw.internal/v1/signal/task-42/'"$TOKEN"'/init-start' --data-binary '' >/dev/null 2>&1 || :
sh /var/lib/lober/script-5.sh
sh /var/lib/lober/cleanup.sh
LOBER_FIRSTBOOT_EOF
cat > '/target/etc/systemd/system/lober-firstboot.service' <<'LOBER_UNIT_EOF'
[Unit]
Description=Lober first boot
After=network-online.target
Wants=network-online.target
ConditionPathExists=/var/lib/lober/firstboot.pending
[Service]
Type=oneshot
ExecStart=/bin/sh /var/lib/lober/firstboot.sh
[Install]
WantedBy=multi-user.target
LOBER_UNIT_EOF
ln -sf ../lober-firstboot.service '/target/etc/systemd/system/multi-user.target.wants/lober-firstboot.service'
touch '/target/var/lib/lober/firstboot.pending'
```

Golden line 32, command 1:

```sh
set -eu
umask 077
mkdir -p '/target/var/lib/lober'
cat > '/target/var/lib/lober/cleanup.sh' <<'LOBER_CLEANUP_EOF'
#!/bin/sh
set -eu
TOKEN=$(cat /var/lib/lober/finalize.token)
curl -fsS -m 10 -X POST 'http://gw.internal/v1/signal/task-42/'"$TOKEN"'/init-done' --data-binary '' >/dev/null 2>&1 || :
TOKEN=$(cat /var/lib/lober/finalize.token)
curl -fsS -m 10 -X POST 'http://gw.internal/v1/signal/task-42/'"$TOKEN"'/ready' --data-binary '' >/dev/null 2>&1 || :
rm -f /var/lib/lober/finalize.token /var/lib/lober/firstboot.pending /var/lib/lober/script-*.sh /var/lib/lober/hook-* /var/lib/lober/firstboot.sh /var/lib/lober/cleanup.sh
rm -f /root/anaconda-ks.cfg /root/original-ks.cfg /var/log/installer/autoinstall-user-data /var/log/installer/autoinstall-user-data.txt /var/log/installer/cdebconf/questions.dat /var/adm/autoinstall/cache/installedSystem.xml
LOBER_CLEANUP_EOF
```

## autoinstall-minimal-uefi.golden

Golden line 28, command 1:

```sh
set -eu
umask 077
mkdir -p '/tmp'
curl -fsS --retry 3 -o '/tmp/script-2.sh' 'http://gw.internal/v1/answer/task-42/install-tok-fixed/scripts/2.sh'
sh '/tmp/script-2.sh'
```

Golden line 30, command 1:

```sh
set -eu
umask 077
mkdir -p '/tmp'
curl -fsS --retry 3 -o '/tmp/script-3.sh' 'http://gw.internal/v1/answer/task-42/install-tok-fixed/scripts/3.sh'
sh '/tmp/script-3.sh'
```

Golden line 31, command 1:

```sh
set -eu
umask 077
mkdir -p '/target/var/lib/lober'
curl -fsS --retry 3 -o '/target/var/lib/lober/script-4.sh' 'http://gw.internal/v1/answer/task-42/install-tok-fixed/scripts/4.sh'
chroot '/target' sh '/var/lib/lober/script-4.sh'
```

Golden line 32, command 1:

```sh
set -eu
umask 077
mkdir -p '/target/var/lib/lober'
curl -fsS --retry 3 -o '/target/var/lib/lober/script-5.sh' 'http://gw.internal/v1/answer/task-42/install-tok-fixed/scripts/5.sh'
```

Golden line 33, command 1:

```sh
set -eu
umask 077
mkdir -p '/target/var/lib/lober' '/target/etc/systemd/system/multi-user.target.wants'
printf '%s' 'finalize-tok-fixed' > '/target/var/lib/lober/finalize.token'
cat > '/target/var/lib/lober/firstboot.sh' <<'LOBER_FIRSTBOOT_EOF'
#!/bin/sh
set -eu
TOKEN=$(cat /var/lib/lober/finalize.token)
curl -fsS -m 10 -X POST 'http://gw.internal/v1/signal/task-42/'"$TOKEN"'/booted' --data-binary '' >/dev/null 2>&1 || :
TOKEN=$(cat /var/lib/lober/finalize.token)
curl -fsS -m 10 -X POST 'http://gw.internal/v1/signal/task-42/'"$TOKEN"'/init-start' --data-binary '' >/dev/null 2>&1 || :
sh /var/lib/lober/script-5.sh
sh /var/lib/lober/cleanup.sh
LOBER_FIRSTBOOT_EOF
cat > '/target/etc/systemd/system/lober-firstboot.service' <<'LOBER_UNIT_EOF'
[Unit]
Description=Lober first boot
After=network-online.target
Wants=network-online.target
ConditionPathExists=/var/lib/lober/firstboot.pending
[Service]
Type=oneshot
ExecStart=/bin/sh /var/lib/lober/firstboot.sh
[Install]
WantedBy=multi-user.target
LOBER_UNIT_EOF
ln -sf ../lober-firstboot.service '/target/etc/systemd/system/multi-user.target.wants/lober-firstboot.service'
touch '/target/var/lib/lober/firstboot.pending'
```

Golden line 34, command 1:

```sh
set -eu
umask 077
mkdir -p '/target/var/lib/lober'
cat > '/target/var/lib/lober/cleanup.sh' <<'LOBER_CLEANUP_EOF'
#!/bin/sh
set -eu
TOKEN=$(cat /var/lib/lober/finalize.token)
curl -fsS -m 10 -X POST 'http://gw.internal/v1/signal/task-42/'"$TOKEN"'/init-done' --data-binary '' >/dev/null 2>&1 || :
TOKEN=$(cat /var/lib/lober/finalize.token)
curl -fsS -m 10 -X POST 'http://gw.internal/v1/signal/task-42/'"$TOKEN"'/ready' --data-binary '' >/dev/null 2>&1 || :
rm -f /var/lib/lober/finalize.token /var/lib/lober/firstboot.pending /var/lib/lober/script-*.sh /var/lib/lober/hook-* /var/lib/lober/firstboot.sh /var/lib/lober/cleanup.sh
rm -f /root/anaconda-ks.cfg /root/original-ks.cfg /var/log/installer/autoinstall-user-data /var/log/installer/autoinstall-user-data.txt /var/log/installer/cdebconf/questions.dat /var/adm/autoinstall/cache/installedSystem.xml
LOBER_CLEANUP_EOF
```

## autoyast-full-bios.golden

Golden line 14, command 1:

```sh
set -eu
umask 077
mkdir -p '/tmp'
curl -fsS --retry 3 -o '/tmp/script-2.sh' 'http://gw.internal/v1/answer/task-42/install-tok-fixed/scripts/2.sh'
sh '/tmp/script-2.sh'
```

Golden line 20, command 1:

```sh
set -eu
umask 077
mkdir -p '/tmp'
curl -fsS --retry 3 -o '/tmp/script-3.sh' 'http://gw.internal/v1/answer/task-42/install-tok-fixed/scripts/3.sh'
sh '/tmp/script-3.sh'
```

Golden line 21, command 1:

```sh
set -eu
umask 077
mkdir -p '/mnt/var/lib/lober'
curl -fsS --retry 3 -o '/mnt/var/lib/lober/script-4.sh' 'http://gw.internal/v1/answer/task-42/install-tok-fixed/scripts/4.sh'
chroot '/mnt' sh '/var/lib/lober/script-4.sh'
```

Golden line 22, command 1:

```sh
set -eu
umask 077
mkdir -p '/mnt/var/lib/lober'
curl -fsS --retry 3 -o '/mnt/var/lib/lober/script-5.sh' 'http://gw.internal/v1/answer/task-42/install-tok-fixed/scripts/5.sh'
curl -fsS --retry 3 -o '/mnt/var/lib/lober/hook-3ee049e591f5e99ae8f498ae381f459365c221e41e6b42f2ad6e9a6a9750532e.sh' 'http://gw.internal/v1/answer/task-42/install-tok-fixed/hooks/3ee049e591f5e99ae8f498ae381f459365c221e41e6b42f2ad6e9a6a9750532e'
```

Golden line 23, command 1:

```sh
set -eu
umask 077
mkdir -p '/mnt/var/lib/lober' '/mnt/etc/systemd/system/multi-user.target.wants'
printf '%s' 'finalize-tok-fixed' > '/mnt/var/lib/lober/finalize.token'
cat > '/mnt/var/lib/lober/firstboot.sh' <<'LOBER_FIRSTBOOT_EOF'
#!/bin/sh
set -eu
TOKEN=$(cat /var/lib/lober/finalize.token)
curl -fsS -m 10 -X POST 'http://gw.internal/v1/signal/task-42/'"$TOKEN"'/booted' --data-binary '' >/dev/null 2>&1 || :
TOKEN=$(cat /var/lib/lober/finalize.token)
curl -fsS -m 10 -X POST 'http://gw.internal/v1/signal/task-42/'"$TOKEN"'/init-start' --data-binary '' >/dev/null 2>&1 || :
sh /var/lib/lober/script-5.sh
sh /var/lib/lober/cleanup.sh
LOBER_FIRSTBOOT_EOF
cat > '/mnt/etc/systemd/system/lober-firstboot.service' <<'LOBER_UNIT_EOF'
[Unit]
Description=Lober first boot
After=network-online.target
Wants=network-online.target
ConditionPathExists=/var/lib/lober/firstboot.pending
[Service]
Type=oneshot
ExecStart=/bin/sh /var/lib/lober/firstboot.sh
[Install]
WantedBy=multi-user.target
LOBER_UNIT_EOF
ln -sf ../lober-firstboot.service '/mnt/etc/systemd/system/multi-user.target.wants/lober-firstboot.service'
touch '/mnt/var/lib/lober/firstboot.pending'
```

Golden line 24, command 1:

```sh
set -eu
umask 077
mkdir -p '/mnt/var/lib/lober'
cat > '/mnt/var/lib/lober/cleanup.sh' <<'LOBER_CLEANUP_EOF'
#!/bin/sh
set -eu
TOKEN=$(cat /var/lib/lober/finalize.token)
curl -fsS -m 10 -X POST 'http://gw.internal/v1/signal/task-42/'"$TOKEN"'/init-done' --data-binary '' >/dev/null 2>&1 || :
TOKEN=$(cat /var/lib/lober/finalize.token)
curl -fsS -m 10 -X POST 'http://gw.internal/v1/signal/task-42/'"$TOKEN"'/ready' --data-binary '' >/dev/null 2>&1 || :
rm -f /var/lib/lober/finalize.token /var/lib/lober/firstboot.pending /var/lib/lober/script-*.sh /var/lib/lober/hook-* /var/lib/lober/firstboot.sh /var/lib/lober/cleanup.sh
rm -f /root/anaconda-ks.cfg /root/original-ks.cfg /var/log/installer/autoinstall-user-data /var/log/installer/autoinstall-user-data.txt /var/log/installer/cdebconf/questions.dat /var/adm/autoinstall/cache/installedSystem.xml
LOBER_CLEANUP_EOF
```

## autoyast-full-uefi.golden

Golden line 14, command 1:

```sh
set -eu
umask 077
mkdir -p '/tmp'
curl -fsS --retry 3 -o '/tmp/script-2.sh' 'http://gw.internal/v1/answer/task-42/install-tok-fixed/scripts/2.sh'
sh '/tmp/script-2.sh'
```

Golden line 20, command 1:

```sh
set -eu
umask 077
mkdir -p '/tmp'
curl -fsS --retry 3 -o '/tmp/script-3.sh' 'http://gw.internal/v1/answer/task-42/install-tok-fixed/scripts/3.sh'
sh '/tmp/script-3.sh'
```

Golden line 21, command 1:

```sh
set -eu
umask 077
mkdir -p '/mnt/var/lib/lober'
curl -fsS --retry 3 -o '/mnt/var/lib/lober/script-4.sh' 'http://gw.internal/v1/answer/task-42/install-tok-fixed/scripts/4.sh'
chroot '/mnt' sh '/var/lib/lober/script-4.sh'
```

Golden line 22, command 1:

```sh
set -eu
umask 077
mkdir -p '/mnt/var/lib/lober'
curl -fsS --retry 3 -o '/mnt/var/lib/lober/script-5.sh' 'http://gw.internal/v1/answer/task-42/install-tok-fixed/scripts/5.sh'
curl -fsS --retry 3 -o '/mnt/var/lib/lober/hook-3ee049e591f5e99ae8f498ae381f459365c221e41e6b42f2ad6e9a6a9750532e.sh' 'http://gw.internal/v1/answer/task-42/install-tok-fixed/hooks/3ee049e591f5e99ae8f498ae381f459365c221e41e6b42f2ad6e9a6a9750532e'
```

Golden line 23, command 1:

```sh
set -eu
umask 077
mkdir -p '/mnt/var/lib/lober' '/mnt/etc/systemd/system/multi-user.target.wants'
printf '%s' 'finalize-tok-fixed' > '/mnt/var/lib/lober/finalize.token'
cat > '/mnt/var/lib/lober/firstboot.sh' <<'LOBER_FIRSTBOOT_EOF'
#!/bin/sh
set -eu
TOKEN=$(cat /var/lib/lober/finalize.token)
curl -fsS -m 10 -X POST 'http://gw.internal/v1/signal/task-42/'"$TOKEN"'/booted' --data-binary '' >/dev/null 2>&1 || :
TOKEN=$(cat /var/lib/lober/finalize.token)
curl -fsS -m 10 -X POST 'http://gw.internal/v1/signal/task-42/'"$TOKEN"'/init-start' --data-binary '' >/dev/null 2>&1 || :
sh /var/lib/lober/script-5.sh
sh /var/lib/lober/cleanup.sh
LOBER_FIRSTBOOT_EOF
cat > '/mnt/etc/systemd/system/lober-firstboot.service' <<'LOBER_UNIT_EOF'
[Unit]
Description=Lober first boot
After=network-online.target
Wants=network-online.target
ConditionPathExists=/var/lib/lober/firstboot.pending
[Service]
Type=oneshot
ExecStart=/bin/sh /var/lib/lober/firstboot.sh
[Install]
WantedBy=multi-user.target
LOBER_UNIT_EOF
ln -sf ../lober-firstboot.service '/mnt/etc/systemd/system/multi-user.target.wants/lober-firstboot.service'
touch '/mnt/var/lib/lober/firstboot.pending'
```

Golden line 24, command 1:

```sh
set -eu
umask 077
mkdir -p '/mnt/var/lib/lober'
cat > '/mnt/var/lib/lober/cleanup.sh' <<'LOBER_CLEANUP_EOF'
#!/bin/sh
set -eu
TOKEN=$(cat /var/lib/lober/finalize.token)
curl -fsS -m 10 -X POST 'http://gw.internal/v1/signal/task-42/'"$TOKEN"'/init-done' --data-binary '' >/dev/null 2>&1 || :
TOKEN=$(cat /var/lib/lober/finalize.token)
curl -fsS -m 10 -X POST 'http://gw.internal/v1/signal/task-42/'"$TOKEN"'/ready' --data-binary '' >/dev/null 2>&1 || :
rm -f /var/lib/lober/finalize.token /var/lib/lober/firstboot.pending /var/lib/lober/script-*.sh /var/lib/lober/hook-* /var/lib/lober/firstboot.sh /var/lib/lober/cleanup.sh
rm -f /root/anaconda-ks.cfg /root/original-ks.cfg /var/log/installer/autoinstall-user-data /var/log/installer/autoinstall-user-data.txt /var/log/installer/cdebconf/questions.dat /var/adm/autoinstall/cache/installedSystem.xml
LOBER_CLEANUP_EOF
```

## autoyast-minimal-bios.golden

Golden line 14, command 1:

```sh
set -eu
umask 077
mkdir -p '/tmp'
curl -fsS --retry 3 -o '/tmp/script-2.sh' 'http://gw.internal/v1/answer/task-42/install-tok-fixed/scripts/2.sh'
sh '/tmp/script-2.sh'
```

Golden line 20, command 1:

```sh
set -eu
umask 077
mkdir -p '/tmp'
curl -fsS --retry 3 -o '/tmp/script-3.sh' 'http://gw.internal/v1/answer/task-42/install-tok-fixed/scripts/3.sh'
sh '/tmp/script-3.sh'
```

Golden line 21, command 1:

```sh
set -eu
umask 077
mkdir -p '/mnt/var/lib/lober'
curl -fsS --retry 3 -o '/mnt/var/lib/lober/script-4.sh' 'http://gw.internal/v1/answer/task-42/install-tok-fixed/scripts/4.sh'
chroot '/mnt' sh '/var/lib/lober/script-4.sh'
```

Golden line 22, command 1:

```sh
set -eu
umask 077
mkdir -p '/mnt/var/lib/lober'
curl -fsS --retry 3 -o '/mnt/var/lib/lober/script-5.sh' 'http://gw.internal/v1/answer/task-42/install-tok-fixed/scripts/5.sh'
```

Golden line 23, command 1:

```sh
set -eu
umask 077
mkdir -p '/mnt/var/lib/lober' '/mnt/etc/systemd/system/multi-user.target.wants'
printf '%s' 'finalize-tok-fixed' > '/mnt/var/lib/lober/finalize.token'
cat > '/mnt/var/lib/lober/firstboot.sh' <<'LOBER_FIRSTBOOT_EOF'
#!/bin/sh
set -eu
TOKEN=$(cat /var/lib/lober/finalize.token)
curl -fsS -m 10 -X POST 'http://gw.internal/v1/signal/task-42/'"$TOKEN"'/booted' --data-binary '' >/dev/null 2>&1 || :
TOKEN=$(cat /var/lib/lober/finalize.token)
curl -fsS -m 10 -X POST 'http://gw.internal/v1/signal/task-42/'"$TOKEN"'/init-start' --data-binary '' >/dev/null 2>&1 || :
sh /var/lib/lober/script-5.sh
sh /var/lib/lober/cleanup.sh
LOBER_FIRSTBOOT_EOF
cat > '/mnt/etc/systemd/system/lober-firstboot.service' <<'LOBER_UNIT_EOF'
[Unit]
Description=Lober first boot
After=network-online.target
Wants=network-online.target
ConditionPathExists=/var/lib/lober/firstboot.pending
[Service]
Type=oneshot
ExecStart=/bin/sh /var/lib/lober/firstboot.sh
[Install]
WantedBy=multi-user.target
LOBER_UNIT_EOF
ln -sf ../lober-firstboot.service '/mnt/etc/systemd/system/multi-user.target.wants/lober-firstboot.service'
touch '/mnt/var/lib/lober/firstboot.pending'
```

Golden line 24, command 1:

```sh
set -eu
umask 077
mkdir -p '/mnt/var/lib/lober'
cat > '/mnt/var/lib/lober/cleanup.sh' <<'LOBER_CLEANUP_EOF'
#!/bin/sh
set -eu
TOKEN=$(cat /var/lib/lober/finalize.token)
curl -fsS -m 10 -X POST 'http://gw.internal/v1/signal/task-42/'"$TOKEN"'/init-done' --data-binary '' >/dev/null 2>&1 || :
TOKEN=$(cat /var/lib/lober/finalize.token)
curl -fsS -m 10 -X POST 'http://gw.internal/v1/signal/task-42/'"$TOKEN"'/ready' --data-binary '' >/dev/null 2>&1 || :
rm -f /var/lib/lober/finalize.token /var/lib/lober/firstboot.pending /var/lib/lober/script-*.sh /var/lib/lober/hook-* /var/lib/lober/firstboot.sh /var/lib/lober/cleanup.sh
rm -f /root/anaconda-ks.cfg /root/original-ks.cfg /var/log/installer/autoinstall-user-data /var/log/installer/autoinstall-user-data.txt /var/log/installer/cdebconf/questions.dat /var/adm/autoinstall/cache/installedSystem.xml
LOBER_CLEANUP_EOF
```

## autoyast-minimal-uefi.golden

Golden line 14, command 1:

```sh
set -eu
umask 077
mkdir -p '/tmp'
curl -fsS --retry 3 -o '/tmp/script-2.sh' 'http://gw.internal/v1/answer/task-42/install-tok-fixed/scripts/2.sh'
sh '/tmp/script-2.sh'
```

Golden line 20, command 1:

```sh
set -eu
umask 077
mkdir -p '/tmp'
curl -fsS --retry 3 -o '/tmp/script-3.sh' 'http://gw.internal/v1/answer/task-42/install-tok-fixed/scripts/3.sh'
sh '/tmp/script-3.sh'
```

Golden line 21, command 1:

```sh
set -eu
umask 077
mkdir -p '/mnt/var/lib/lober'
curl -fsS --retry 3 -o '/mnt/var/lib/lober/script-4.sh' 'http://gw.internal/v1/answer/task-42/install-tok-fixed/scripts/4.sh'
chroot '/mnt' sh '/var/lib/lober/script-4.sh'
```

Golden line 22, command 1:

```sh
set -eu
umask 077
mkdir -p '/mnt/var/lib/lober'
curl -fsS --retry 3 -o '/mnt/var/lib/lober/script-5.sh' 'http://gw.internal/v1/answer/task-42/install-tok-fixed/scripts/5.sh'
```

Golden line 23, command 1:

```sh
set -eu
umask 077
mkdir -p '/mnt/var/lib/lober' '/mnt/etc/systemd/system/multi-user.target.wants'
printf '%s' 'finalize-tok-fixed' > '/mnt/var/lib/lober/finalize.token'
cat > '/mnt/var/lib/lober/firstboot.sh' <<'LOBER_FIRSTBOOT_EOF'
#!/bin/sh
set -eu
TOKEN=$(cat /var/lib/lober/finalize.token)
curl -fsS -m 10 -X POST 'http://gw.internal/v1/signal/task-42/'"$TOKEN"'/booted' --data-binary '' >/dev/null 2>&1 || :
TOKEN=$(cat /var/lib/lober/finalize.token)
curl -fsS -m 10 -X POST 'http://gw.internal/v1/signal/task-42/'"$TOKEN"'/init-start' --data-binary '' >/dev/null 2>&1 || :
sh /var/lib/lober/script-5.sh
sh /var/lib/lober/cleanup.sh
LOBER_FIRSTBOOT_EOF
cat > '/mnt/etc/systemd/system/lober-firstboot.service' <<'LOBER_UNIT_EOF'
[Unit]
Description=Lober first boot
After=network-online.target
Wants=network-online.target
ConditionPathExists=/var/lib/lober/firstboot.pending
[Service]
Type=oneshot
ExecStart=/bin/sh /var/lib/lober/firstboot.sh
[Install]
WantedBy=multi-user.target
LOBER_UNIT_EOF
ln -sf ../lober-firstboot.service '/mnt/etc/systemd/system/multi-user.target.wants/lober-firstboot.service'
touch '/mnt/var/lib/lober/firstboot.pending'
```

Golden line 24, command 1:

```sh
set -eu
umask 077
mkdir -p '/mnt/var/lib/lober'
cat > '/mnt/var/lib/lober/cleanup.sh' <<'LOBER_CLEANUP_EOF'
#!/bin/sh
set -eu
TOKEN=$(cat /var/lib/lober/finalize.token)
curl -fsS -m 10 -X POST 'http://gw.internal/v1/signal/task-42/'"$TOKEN"'/init-done' --data-binary '' >/dev/null 2>&1 || :
TOKEN=$(cat /var/lib/lober/finalize.token)
curl -fsS -m 10 -X POST 'http://gw.internal/v1/signal/task-42/'"$TOKEN"'/ready' --data-binary '' >/dev/null 2>&1 || :
rm -f /var/lib/lober/finalize.token /var/lib/lober/firstboot.pending /var/lib/lober/script-*.sh /var/lib/lober/hook-* /var/lib/lober/firstboot.sh /var/lib/lober/cleanup.sh
rm -f /root/anaconda-ks.cfg /root/original-ks.cfg /var/log/installer/autoinstall-user-data /var/log/installer/autoinstall-user-data.txt /var/log/installer/cdebconf/questions.dat /var/adm/autoinstall/cache/installedSystem.xml
LOBER_CLEANUP_EOF
```

## unattend-full-bios.golden

Concrete pre-reboot staged firstboot.ps1 (UTF-8 BOM file; compact native -File invocation, not an inline encoded command):

```powershell
$ErrorActionPreference='Stop'
$dir=Join-Path $env:SystemDrive 'ProgramData/lober'
$token=(Get-Content -Raw (Join-Path $dir 'finalize.token')).Trim()
try { Invoke-WebRequest -UseBasicParsing -TimeoutSec 10 -Method Post -Uri ('http://gw.internal/v1/signal/task-42/'+$token+'/booted') -Body '' | Out-Null } catch {}
try { Invoke-WebRequest -UseBasicParsing -TimeoutSec 10 -Method Post -Uri ('http://gw.internal/v1/signal/task-42/'+$token+'/init-start') -Body '' | Out-Null } catch {}
exit 0
```

Concrete pre-reboot staged cleanup.ps1 (UTF-8 BOM file; compact native -File invocation, not an inline encoded command):

```powershell
$ErrorActionPreference='Stop'
$dir=Join-Path $env:SystemDrive 'ProgramData/lober'
$token=(Get-Content -Raw (Join-Path $dir 'finalize.token')).Trim()
try { Invoke-WebRequest -UseBasicParsing -TimeoutSec 10 -Method Post -Uri ('http://gw.internal/v1/signal/task-42/'+$token+'/init-done') -Body '' | Out-Null } catch {}
try { Invoke-WebRequest -UseBasicParsing -TimeoutSec 10 -Method Post -Uri ('http://gw.internal/v1/signal/task-42/'+$token+'/ready') -Body '' | Out-Null } catch {}
Remove-Item -Force -ErrorAction SilentlyContinue (Join-Path $dir 'finalize.token'),(Join-Path $dir 'script-*.cmd'),(Join-Path $dir 'hook-*'),(Join-Path $dir 'firstboot.ps1'),(Join-Path $dir 'cleanup.ps1')
Remove-Item -Force -ErrorAction SilentlyContinue (Join-Path $env:WINDIR 'Panther/Unattend.xml'),(Join-Path $env:WINDIR 'Panther/Unattend/Unattend.xml')
exit 0
```

## unattend-full-uefi.golden

Concrete pre-reboot staged firstboot.ps1 (UTF-8 BOM file; compact native -File invocation, not an inline encoded command):

```powershell
$ErrorActionPreference='Stop'
$dir=Join-Path $env:SystemDrive 'ProgramData/lober'
$token=(Get-Content -Raw (Join-Path $dir 'finalize.token')).Trim()
try { Invoke-WebRequest -UseBasicParsing -TimeoutSec 10 -Method Post -Uri ('http://gw.internal/v1/signal/task-42/'+$token+'/booted') -Body '' | Out-Null } catch {}
try { Invoke-WebRequest -UseBasicParsing -TimeoutSec 10 -Method Post -Uri ('http://gw.internal/v1/signal/task-42/'+$token+'/init-start') -Body '' | Out-Null } catch {}
exit 0
```

Concrete pre-reboot staged cleanup.ps1 (UTF-8 BOM file; compact native -File invocation, not an inline encoded command):

```powershell
$ErrorActionPreference='Stop'
$dir=Join-Path $env:SystemDrive 'ProgramData/lober'
$token=(Get-Content -Raw (Join-Path $dir 'finalize.token')).Trim()
try { Invoke-WebRequest -UseBasicParsing -TimeoutSec 10 -Method Post -Uri ('http://gw.internal/v1/signal/task-42/'+$token+'/init-done') -Body '' | Out-Null } catch {}
try { Invoke-WebRequest -UseBasicParsing -TimeoutSec 10 -Method Post -Uri ('http://gw.internal/v1/signal/task-42/'+$token+'/ready') -Body '' | Out-Null } catch {}
Remove-Item -Force -ErrorAction SilentlyContinue (Join-Path $dir 'finalize.token'),(Join-Path $dir 'script-*.cmd'),(Join-Path $dir 'hook-*'),(Join-Path $dir 'firstboot.ps1'),(Join-Path $dir 'cleanup.ps1')
Remove-Item -Force -ErrorAction SilentlyContinue (Join-Path $env:WINDIR 'Panther/Unattend.xml'),(Join-Path $env:WINDIR 'Panther/Unattend/Unattend.xml')
exit 0
```

## unattend-minimal-bios.golden

Concrete pre-reboot staged firstboot.ps1 (UTF-8 BOM file; compact native -File invocation, not an inline encoded command):

```powershell
$ErrorActionPreference='Stop'
$dir=Join-Path $env:SystemDrive 'ProgramData/lober'
$token=(Get-Content -Raw (Join-Path $dir 'finalize.token')).Trim()
try { Invoke-WebRequest -UseBasicParsing -TimeoutSec 10 -Method Post -Uri ('http://gw.internal/v1/signal/task-42/'+$token+'/booted') -Body '' | Out-Null } catch {}
try { Invoke-WebRequest -UseBasicParsing -TimeoutSec 10 -Method Post -Uri ('http://gw.internal/v1/signal/task-42/'+$token+'/init-start') -Body '' | Out-Null } catch {}
exit 0
```

Concrete pre-reboot staged cleanup.ps1 (UTF-8 BOM file; compact native -File invocation, not an inline encoded command):

```powershell
$ErrorActionPreference='Stop'
$dir=Join-Path $env:SystemDrive 'ProgramData/lober'
$token=(Get-Content -Raw (Join-Path $dir 'finalize.token')).Trim()
try { Invoke-WebRequest -UseBasicParsing -TimeoutSec 10 -Method Post -Uri ('http://gw.internal/v1/signal/task-42/'+$token+'/init-done') -Body '' | Out-Null } catch {}
try { Invoke-WebRequest -UseBasicParsing -TimeoutSec 10 -Method Post -Uri ('http://gw.internal/v1/signal/task-42/'+$token+'/ready') -Body '' | Out-Null } catch {}
Remove-Item -Force -ErrorAction SilentlyContinue (Join-Path $dir 'finalize.token'),(Join-Path $dir 'script-*.cmd'),(Join-Path $dir 'hook-*'),(Join-Path $dir 'firstboot.ps1'),(Join-Path $dir 'cleanup.ps1')
Remove-Item -Force -ErrorAction SilentlyContinue (Join-Path $env:WINDIR 'Panther/Unattend.xml'),(Join-Path $env:WINDIR 'Panther/Unattend/Unattend.xml')
exit 0
```

## unattend-minimal-uefi.golden

Concrete pre-reboot staged firstboot.ps1 (UTF-8 BOM file; compact native -File invocation, not an inline encoded command):

```powershell
$ErrorActionPreference='Stop'
$dir=Join-Path $env:SystemDrive 'ProgramData/lober'
$token=(Get-Content -Raw (Join-Path $dir 'finalize.token')).Trim()
try { Invoke-WebRequest -UseBasicParsing -TimeoutSec 10 -Method Post -Uri ('http://gw.internal/v1/signal/task-42/'+$token+'/booted') -Body '' | Out-Null } catch {}
try { Invoke-WebRequest -UseBasicParsing -TimeoutSec 10 -Method Post -Uri ('http://gw.internal/v1/signal/task-42/'+$token+'/init-start') -Body '' | Out-Null } catch {}
exit 0
```

Concrete pre-reboot staged cleanup.ps1 (UTF-8 BOM file; compact native -File invocation, not an inline encoded command):

```powershell
$ErrorActionPreference='Stop'
$dir=Join-Path $env:SystemDrive 'ProgramData/lober'
$token=(Get-Content -Raw (Join-Path $dir 'finalize.token')).Trim()
try { Invoke-WebRequest -UseBasicParsing -TimeoutSec 10 -Method Post -Uri ('http://gw.internal/v1/signal/task-42/'+$token+'/init-done') -Body '' | Out-Null } catch {}
try { Invoke-WebRequest -UseBasicParsing -TimeoutSec 10 -Method Post -Uri ('http://gw.internal/v1/signal/task-42/'+$token+'/ready') -Body '' | Out-Null } catch {}
Remove-Item -Force -ErrorAction SilentlyContinue (Join-Path $dir 'finalize.token'),(Join-Path $dir 'script-*.cmd'),(Join-Path $dir 'hook-*'),(Join-Path $dir 'firstboot.ps1'),(Join-Path $dir 'cleanup.ps1')
Remove-Item -Force -ErrorAction SilentlyContinue (Join-Path $env:WINDIR 'Panther/Unattend.xml'),(Join-Path $env:WINDIR 'Panther/Unattend/Unattend.xml')
exit 0
```

