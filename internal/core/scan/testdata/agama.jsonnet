// lober 正文扫描样例：Agama（SLES 16、openSUSE Leap 16），Jsonnet 写法
{
  product: { id: 'SLES' },
  hostname: { static: '{{ param:hostname }}' },
  software: {
    packages: [
      '{{param:pkgs}}',
    ],
  },
  extraRepositories: [
    { alias: 'lober', url: '{{sys:repo}}' },
  ],
  storage: {
    {{slot:分区}}
  },
  scripts: {
    pre: [{ name: 'lober-pre', body: '{{slot:装前@pre}}' }],
    post: [{ name: 'lober-post', chroot: true, body: '{{slot:装后@post-target}}' }],
    init: [
      { name: 'lober-firstboot', body: '{{lober:firstboot}}' },
      { name: 'lober-cleanup', body: '{{lober:cleanup}}' },
    ],
  },
}
