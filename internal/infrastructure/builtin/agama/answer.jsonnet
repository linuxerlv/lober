{
  product: { id: '{{param:product}}' },
  localization: { language: 'en_US.UTF-8', keyboard: 'us', timezone: 'Etc/UTC' },
  {{slot:partition}}
  {{slot:network}}
  {{slot:account}}
  software: {
    packages: [
      'curl',
      'systemd',
      'openssh',
      {{slot:packages}}
    ],
  },
  scripts: {
    post: [{ name: 'lober-stage', chroot: false, content: |||
      #!/bin/sh
      set -eu
      chroot /mnt systemctl enable sshd
      {{slot:sshkey@firstboot}}
      {{lober:firstboot}}
      {{lober:cleanup}}
    ||| }],
  },
}
