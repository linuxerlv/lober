import type { IncomingMessage, ServerResponse } from 'node:http';

const nowSec = () => Math.floor(Date.now() / 1000);
const toB64 = (text: string) => Buffer.from(text, 'utf8').toString('base64');

const rolePermissions: Record<string, string[]> = {
  admin: [
    'task:read',
    'task:create',
    'task:cancel',
    'task:retry',
    'host:read',
    'host:write',
    'iso:read',
    'iso:write',
    'template:read',
    'template:write',
    'template:approve',
    'hook:read',
    'hook:write',
    'hook:approve',
    'audit:read',
    'user:manage',
  ],
  operator: [
    'task:read',
    'task:create',
    'task:cancel',
    'task:retry',
    'host:read',
    'host:write',
    'iso:read',
    'iso:write',
    'template:read',
    'hook:read',
  ],
  'hook-author': [
    'task:read',
    'host:read',
    'iso:read',
    'template:read',
    'template:write',
    'hook:read',
    'hook:write',
  ],
  'hook-approver': [
    'task:read',
    'host:read',
    'iso:read',
    'template:read',
    'template:approve',
    'hook:read',
    'hook:approve',
    'audit:read',
  ],
  viewer: [
    'task:read',
    'host:read',
    'iso:read',
    'template:read',
    'hook:read',
  ],
};

const defaultRoleMenus: Record<string, string[]> = {
  admin: [
    'tasks',
    'hosts',
    'isos',
    'templates',
    'hooks',
    'reports',
    'users',
    'role-menus',
  ],
  operator: ['tasks', 'hosts', 'isos', 'templates', 'hooks'],
  'hook-author': ['tasks', 'templates', 'hooks', 'isos'],
  'hook-approver': ['tasks', 'templates', 'hooks', 'reports'],
  viewer: ['tasks', 'hosts', 'isos', 'templates', 'hooks'],
};

const menuCatalog = [
  { id: 'ops', title: '装机中心', parentId: '', permission: '', required: false },
  { id: 'assets', title: '资源与配置', parentId: '', permission: '', required: false },
  { id: 'gov', title: '审计与权限', parentId: '', permission: '', required: false },
  { id: 'tasks', title: '装机任务', parentId: 'ops', permission: 'task:read', required: false },
  { id: 'hosts', title: '主机清单', parentId: 'ops', permission: 'host:read', required: false },
  { id: 'isos', title: '系统镜像', parentId: 'assets', permission: 'iso:read', required: false },
  { id: 'templates', title: '装机模板', parentId: 'assets', permission: 'template:read', required: false },
  { id: 'hooks', title: '脚本组件', parentId: 'assets', permission: 'hook:read', required: false },
  { id: 'reports', title: '审计与报表', parentId: 'gov', permission: 'audit:read', required: false },
  { id: 'users', title: '账号管理', parentId: 'gov', permission: 'user:manage', required: false },
  { id: 'role-menus', title: '角色管理', parentId: 'gov', permission: 'user:manage', required: true },
];

const users: Array<Record<string, any>> = [
  {
    username: 'admin',
    displayName: '张维（系统管理员）',
    role: 'admin',
    disabled: false,
    passwordChangeRequired: false,
    createdAt: nowSec() - 86_400 * 60,
    rev: 3,
  },
  {
    username: 'liming',
    displayName: '李明（机房运维）',
    role: 'operator',
    disabled: false,
    passwordChangeRequired: false,
    createdAt: nowSec() - 86_400 * 45,
    rev: 2,
  },
  {
    username: 'wanggong',
    displayName: '王工（模板与脚本作者）',
    role: 'hook-author',
    disabled: false,
    passwordChangeRequired: false,
    createdAt: nowSec() - 86_400 * 30,
    rev: 1,
  },
  {
    username: 'zhaoshen',
    displayName: '赵审（架构审批人）',
    role: 'hook-approver',
    disabled: false,
    passwordChangeRequired: false,
    createdAt: nowSec() - 86_400 * 28,
    rev: 1,
  },
  {
    username: 'chenxun',
    displayName: '陈巡（值班只读巡检）',
    role: 'viewer',
    disabled: false,
    passwordChangeRequired: false,
    createdAt: nowSec() - 86_400 * 15,
    rev: 1,
  },
];

const roleMenuState: Record<string, { menuIds: string[]; rev: number }> = Object.fromEntries(
  Object.entries(defaultRoleMenus).map(([role, ids]) => [
    role,
    { menuIds: [...ids], rev: 1 },
  ]),
);

let activeUsername = 'admin';

function buildSessionUser(username: string) {
  const found = users.find((u) => u.username === username) ?? users[0]!;
  const role = found.role as string;
  const permissions = rolePermissions[role] ?? rolePermissions.viewer!;
  const menuState = roleMenuState[role] ?? { menuIds: defaultRoleMenus[role] ?? [], rev: 1 };
  return {
    username: found.username,
    displayName: found.displayName,
    role,
    permissions,
    menuIds: menuState.menuIds,
    menuRev: menuState.rev,
    disabled: found.disabled,
    passwordChangeRequired: found.passwordChangeRequired,
    createdAt: found.createdAt,
    rev: found.rev,
  };
}

const hosts: Array<Record<string, any>> = [
  {
    sn: 'SRV-BJ-A01-01',
    arch: 'x86_64',
    firmware: 'uefi',
    rev: 4,
    attrs: {
      datacenter: '北京亦庄一区',
      rack: 'A01-U12',
      ip: '10.24.1.11',
      bmc_ip: '10.24.101.11',
      env: '生产集群',
      role: '计算节点',
      inv_collected_at: String(nowSec() - 3600),
      inv_system_model: 'Dell PowerEdge R760',
      inv_cpu_model: '2 × Intel Xeon Gold 6430',
      inv_cpu_cores: '64',
      inv_mem_mib: '262144',
      inv_system_disk: 'nvme0n1 (480 GB)',
      inv_disks: JSON.stringify([
        { device: 'nvme0n1', model: 'INTEL SSDPF2KX480GZ', bytes: '480103981056' },
        { device: 'nvme1n1', model: 'SAMSUNG MZQL23T8HCLS', bytes: '3840755982336' },
      ]),
      inv_nics: JSON.stringify([
        { name: 'eno1', model: 'Intel E810-XXV 25GbE', mac: '52:54:00:a1:01:01' },
        { name: 'eno2', model: 'Intel E810-XXV 25GbE', mac: '52:54:00:a1:01:02' },
      ]),
    },
  },
  {
    sn: 'SRV-BJ-A01-02',
    arch: 'x86_64',
    firmware: 'uefi',
    rev: 3,
    attrs: {
      datacenter: '北京亦庄一区',
      rack: 'A01-U14',
      ip: '10.24.1.12',
      bmc_ip: '10.24.101.12',
      env: '生产集群',
      role: '计算节点',
      inv_collected_at: String(nowSec() - 7200),
      inv_system_model: 'Dell PowerEdge R760',
      inv_cpu_model: '2 × Intel Xeon Gold 6430',
      inv_cpu_cores: '64',
      inv_mem_mib: '262144',
      inv_system_disk: 'nvme0n1 (480 GB)',
      inv_disks: JSON.stringify([
        { device: 'nvme0n1', model: 'INTEL SSDPF2KX480GZ', bytes: '480103981056' },
        { device: 'nvme1n1', model: 'SAMSUNG MZQL23T8HCLS', bytes: '3840755982336' },
      ]),
      inv_nics: JSON.stringify([
        { name: 'eno1', model: 'Intel E810-XXV 25GbE', mac: '52:54:00:a1:02:01' },
      ]),
    },
  },
  {
    sn: 'SRV-BJ-A01-03',
    arch: 'x86_64',
    firmware: 'uefi',
    rev: 5,
    attrs: {
      datacenter: '北京亦庄一区',
      rack: 'A01-U16',
      ip: '10.24.1.13',
      bmc_ip: '10.24.101.13',
      env: '生产集群',
      role: '存储节点',
      inv_collected_at: String(nowSec() - 5400),
      inv_system_model: 'Inspur NF5280M6',
      inv_cpu_model: '2 × Intel Xeon Silver 4314',
      inv_cpu_cores: '32',
      inv_mem_mib: '131072',
      inv_system_disk: 'sda (480 GB)',
      inv_disks: JSON.stringify([
        { device: 'sda', model: 'Micron 5400 PRO 480GB', bytes: '480103981056' },
        { device: 'sdb', model: 'Seagate Exos 16TB', bytes: '16000900661248' },
        { device: 'sdc', model: 'Seagate Exos 16TB', bytes: '16000900661248' },
      ]),
      inv_nics: JSON.stringify([
        { name: 'eth0', model: 'Mellanox ConnectX-6 Lx', mac: '52:54:00:a1:03:01' },
      ]),
    },
  },
  {
    sn: 'SRV-BJ-A02-01',
    arch: 'x86_64',
    firmware: 'uefi',
    rev: 2,
    attrs: {
      datacenter: '北京亦庄二区',
      rack: 'A02-U08',
      ip: '10.24.2.21',
      bmc_ip: '10.24.102.21',
      env: '容器集群',
      role: 'K8s Worker',
      inv_collected_at: String(nowSec() - 1800),
      inv_system_model: 'H3C UniServer R4900 G6',
      inv_cpu_model: '2 × Intel Xeon Gold 6426Y',
      inv_cpu_cores: '32',
      inv_mem_mib: '131072',
      inv_system_disk: 'nvme0n1 (960 GB)',
      inv_disks: JSON.stringify([
        { device: 'nvme0n1', model: 'SAMSUNG PM9A3 960GB', bytes: '960197124096' },
        { device: 'nvme1n1', model: 'SAMSUNG PM9A3 3.84TB', bytes: '3840755982336' },
      ]),
      inv_nics: JSON.stringify([
        { name: 'ens1f0', model: 'Broadcom BCM57414 25GbE', mac: '52:54:00:a2:01:01' },
      ]),
    },
  },
  {
    sn: 'SRV-BJ-A02-02',
    arch: 'x86_64',
    firmware: 'uefi',
    rev: 2,
    attrs: {
      datacenter: '北京亦庄二区',
      rack: 'A02-U10',
      ip: '10.24.2.22',
      bmc_ip: '10.24.102.22',
      env: '容器集群',
      role: 'K8s Worker',
      inv_collected_at: String(nowSec() - 1900),
      inv_system_model: 'H3C UniServer R4900 G6',
      inv_cpu_model: '2 × Intel Xeon Gold 6426Y',
      inv_cpu_cores: '32',
      inv_mem_mib: '131072',
      inv_system_disk: 'nvme0n1 (960 GB)',
      inv_disks: JSON.stringify([
        { device: 'nvme0n1', model: 'SAMSUNG PM9A3 960GB', bytes: '960197124096' },
      ]),
      inv_nics: JSON.stringify([
        { name: 'ens1f0', model: 'Broadcom BCM57414 25GbE', mac: '52:54:00:a2:02:01' },
      ]),
    },
  },
  {
    sn: 'SRV-BJ-A02-03',
    arch: 'x86_64',
    firmware: 'uefi',
    rev: 1,
    attrs: {
      datacenter: '北京亦庄二区',
      rack: 'A02-U12',
      ip: '10.24.2.23',
      bmc_ip: '10.24.102.23',
      env: '容器集群',
      role: '空闲备机',
      inv_collected_at: String(nowSec() - 600),
      inv_system_model: 'H3C UniServer R4900 G6',
      inv_cpu_model: '2 × Intel Xeon Gold 6426Y',
      inv_cpu_cores: '32',
      inv_mem_mib: '131072',
      inv_system_disk: 'nvme0n1 (960 GB)',
      inv_disks: JSON.stringify([
        { device: 'nvme0n1', model: 'SAMSUNG PM9A3 960GB', bytes: '960197124096' },
        { device: 'nvme1n1', model: 'SAMSUNG PM9A3 3.84TB', bytes: '3840755982336' },
      ]),
      inv_nics: JSON.stringify([
        { name: 'ens1f0', model: 'Broadcom BCM57414 25GbE', mac: '52:54:00:a2:03:01' },
      ]),
    },
  },
  {
    sn: 'SRV-SH-B01-01',
    arch: 'x86_64',
    firmware: 'uefi',
    rev: 2,
    attrs: {
      datacenter: '上海嘉定机房',
      rack: 'B01-U04',
      ip: '10.32.1.15',
      bmc_ip: '10.32.101.15',
      env: '虚拟化专区',
      role: 'ESXi 宿主机',
      inv_collected_at: String(nowSec() - 14_400),
      inv_system_model: 'Lenovo ThinkSystem SR650 V3',
      inv_cpu_model: '2 × Intel Xeon Gold 6448Y',
      inv_cpu_cores: '64',
      inv_mem_mib: '524288',
      inv_system_disk: 'nvme0n1 (960 GB)',
      inv_disks: JSON.stringify([
        { device: 'nvme0n1', model: 'KIOXIA CD8P 960GB', bytes: '960197124096' },
      ]),
      inv_nics: JSON.stringify([
        { name: 'vmnic0', model: 'Intel X710 10GbE', mac: '52:54:00:b1:01:01' },
      ]),
    },
  },
  {
    sn: 'SRV-GZ-C01-01',
    arch: 'aarch64',
    firmware: 'uefi',
    rev: 1,
    attrs: {
      datacenter: '广州南沙机房',
      rack: 'C01-U20',
      ip: '10.48.1.31',
      bmc_ip: '10.48.101.31',
      env: 'ARM 算力池',
      role: '空闲备机',
      inv_collected_at: String(nowSec() - 900),
      inv_system_model: 'TaiShan 200 (Model 2280)',
      inv_cpu_model: '2 × Kunpeng 920 5250',
      inv_cpu_cores: '96',
      inv_mem_mib: '262144',
      inv_system_disk: 'sda (480 GB)',
      inv_disks: JSON.stringify([
        { device: 'sda', model: 'HUAWEI ER3 480GB', bytes: '480103981056' },
        { device: 'sdb', model: 'HUAWEI ER3 3.84TB', bytes: '3840755982336' },
      ]),
      inv_nics: JSON.stringify([
        { name: 'enp125s0f0', model: 'HiSilicon Hi1822 25GbE', mac: '52:54:00:c1:01:01' },
      ]),
    },
  },
];

const discoveries: Array<Record<string, any>> = [
  {
    mac: '52:54:00:f1:88:01',
    sn: 'SRV-BJ-A03-01',
    arch: 'x86_64',
    firmware: 'uefi',
    system_model: 'Dell PowerEdge R760xs',
    status: 'received',
    first_seen_at: nowSec() - 1200,
    last_seen_at: nowSec() - 45,
    report_at: nowSec() - 45,
    report: {
      sections: [
        {
          key: 'system',
          title: '整机与主板',
          status: 'read',
          items: [
            {
              name: 'Dell PowerEdge R760xs',
              fields: { 制造商: 'Dell Inc.', 序列号: 'SRV-BJ-A03-01', BIOS版本: '2.1.5' },
            },
          ],
        },
        {
          key: 'cpu',
          title: '处理器',
          status: 'read',
          items: [
            {
              name: 'CPU 0 / CPU 1',
              fields: { 型号: 'Intel Xeon Gold 6430', 核心总数: '64 核 / 128 线程' },
            },
          ],
        },
        {
          key: 'disks',
          title: '存储磁盘',
          status: 'read',
          items: [
            {
              name: 'nvme0n1',
              fields: { 容量: '480 GB', 型号: 'INTEL SSDPF2KX480GZ', 序列号: 'PHAX3120001A480' },
            },
            {
              name: 'nvme1n1',
              fields: { 容量: '3.84 TB', 型号: 'SAMSUNG MZQL23T8HCLS', 序列号: 'S64HNE0R802911' },
            },
          ],
        },
      ],
    },
  },
  {
    mac: '52:54:00:f1:88:02',
    sn: 'SRV-BJ-A03-02',
    arch: 'x86_64',
    firmware: 'uefi',
    system_model: 'H3C UniServer R4900 G6',
    status: 'received',
    first_seen_at: nowSec() - 800,
    last_seen_at: nowSec() - 20,
    report_at: nowSec() - 20,
    report: {
      sections: [
        {
          key: 'system',
          title: '整机与主板',
          status: 'read',
          items: [
            {
              name: 'H3C UniServer R4900 G6',
              fields: { 制造商: 'New H3C Technologies', 序列号: 'SRV-BJ-A03-02' },
            },
          ],
        },
        {
          key: 'disks',
          title: '存储磁盘',
          status: 'read',
          items: [
            {
              name: 'nvme0n1',
              fields: { 容量: '960 GB', 型号: 'SAMSUNG PM9A3', 序列号: 'S69ENX0T510293' },
            },
          ],
        },
      ],
    },
  },
  {
    mac: '52:54:00:f1:88:09',
    sn: 'SRV-GZ-C02-01',
    arch: 'aarch64',
    firmware: 'uefi',
    system_model: 'TaiShan 200 (Model 2280)',
    status: 'waiting',
    reason: '主机已从网络启动进入采集内核，正在扫描 RAID 控制器与光模块…',
    first_seen_at: nowSec() - 180,
    last_seen_at: nowSec() - 10,
  },
];

const isos: Array<Record<string, any>> = [
  {
    id: 'iso-rocky-9.4-x86_64',
    name: 'Rocky Linux 9.4 Minimal x86_64',
    family: 'linux',
    series: 'rocky',
    version: '9.4',
    arch: 'x86_64',
    state: 'ready',
    sha256: '7c2e1c8b9f3a41d8e0b2a5f6c1d3e9f8a7b6c5d4e3f2a1b0c9d8e7f6a5b4c3d2',
    rev: 2,
    artifacts: {
      iso: '/var/lib/lober/isos/Rocky-9.4-x86_64-minimal.iso',
      kernel: '/var/lib/lober/isos/rocky-9.4/images/pxeboot/vmlinuz',
      initrd: '/var/lib/lober/isos/rocky-9.4/images/pxeboot/initrd.img',
    },
  },
  {
    id: 'iso-ubuntu-24.04-x86_64',
    name: 'Ubuntu Server 24.04.1 LTS x86_64',
    family: 'linux',
    series: 'ubuntu',
    version: '24.04.1',
    arch: 'x86_64',
    state: 'ready',
    sha256: 'a91f4d3b8c7e6201f3a4b5c6d7e8f9a0b1c2d3e4f5a6b7c8d9e0f1a2b3c4d5e6',
    rev: 1,
    artifacts: {
      iso: '/var/lib/lober/isos/ubuntu-24.04.1-live-server-amd64.iso',
      kernel: '/var/lib/lober/isos/ubuntu-24.04/casper/vmlinuz',
      initrd: '/var/lib/lober/isos/ubuntu-24.04/casper/initrd',
    },
  },
  {
    id: 'iso-esxi-8.0u3-x86_64',
    name: 'VMware ESXi 8.0 Update 3 Standard',
    family: 'esxi',
    series: 'esxi',
    version: '8.0.3',
    arch: 'x86_64',
    state: 'ready',
    sha256: 'f30d9e8c7b6a5f4e3d2c1b0a9f8e7d6c5b4a3f2e1d0c9b8a7f6e5d4c3b2a1f0e',
    rev: 1,
    artifacts: {
      iso: '/var/lib/lober/isos/VMware-VMvisor-Installer-8.0U3-x86_64.iso',
      boot_cfg: '/var/lib/lober/isos/esxi-8.0u3/efi/boot/boot.cfg',
    },
  },
  {
    id: 'iso-win2022-x86_64',
    name: 'Windows Server 2022 Datacenter CN',
    family: 'windows',
    series: 'windows',
    version: '2022',
    arch: 'x86_64',
    state: 'ready',
    sha256: '4b3a2c1d0e9f8a7b6c5d4e3f2a1b0c9d8e7f6a5b4c3d2e1f0a9b8c7d6e5f4a3b',
    rev: 1,
    artifacts: {
      iso: '/var/lib/lober/isos/Win_Server_2022_Datacenter_CN.iso',
      wim: '/var/lib/lober/isos/win2022/sources/install.wim',
    },
  },
  {
    id: 'iso-debian-12.7-arm64',
    name: 'Debian 12.7 Bookworm ARM64 Netinst',
    family: 'linux',
    series: 'debian',
    version: '12.7',
    arch: 'aarch64',
    state: 'acquiring',
    sha256: '',
    rev: 1,
    artifacts: {},
  },
];

const hooks: Array<Record<string, any>> = [
  {
    id: 'hook-net-static',
    name: '配置生产静态网络与网关',
    version: 2,
    rev: 3,
    state: 'approved',
    builtin: true,
    author: 'system',
    approver: 'system',
    description: '根据主机登记的 IP、网关与 DNS 生成 NetworkManager 静态网卡配置文件。',
    timeoutSec: 120,
    required: true,
    params: [
      {
        name: 'mgmt_ip',
        label: '管理网 IP/掩码',
        type: 'cidr',
        required: true,
        list: false,
        default: '10.24.1.100/24',
        options: [],
      },
      {
        name: 'gateway',
        label: '默认网关',
        type: 'ip',
        required: true,
        list: false,
        default: '10.24.1.254',
        options: [],
      },
    ],
    impls: [
      {
        family: 'linux',
        series: ['rocky', 'almalinux', 'ubuntu', 'debian'],
        kind: 'script',
        interpreter: 'bash',
        timings: ['post-target', 'firstboot'],
        body: `#!/usr/bin/env bash
set -euo pipefail
echo "[Lober] 正在配置管理网静态地址: {{ .mgmt_ip }} 网关: {{ .gateway }}"
nmcli con mod "System eno1" ipv4.addresses "{{ .mgmt_ip }}" ipv4.gateway "{{ .gateway }}" ipv4.method manual
nmcli con up "System eno1" || true`,
      },
    ],
    problems: [],
  },
  {
    id: 'hook-repo-mirror',
    name: '注入内网软件源与企业根证书',
    version: 1,
    rev: 2,
    state: 'approved',
    builtin: false,
    author: 'wanggong',
    approver: 'zhaoshen',
    description: '替换默认公网源为机房内网 Nexus 镜像源，并导入企业内部 CA 证书。',
    timeoutSec: 180,
    required: true,
    params: [
      {
        name: 'mirror_host',
        label: '内网镜像站域名',
        type: 'hostname',
        required: true,
        list: false,
        default: 'mirrors.intra.lober.local',
        options: [],
      },
    ],
    impls: [
      {
        family: 'linux',
        series: ['rocky', 'ubuntu'],
        kind: 'script',
        interpreter: 'bash',
        timings: ['post-target'],
        body: `#!/usr/bin/env bash
set -euo pipefail
echo "配置内网软件源 https://{{ .mirror_host }}/rocky"
update-ca-trust extract`,
      },
    ],
    problems: [],
  },
  {
    id: 'hook-security-baseline',
    name: '等保三级基线加固与运维公钥下发',
    version: 1,
    rev: 1,
    state: 'review',
    builtin: false,
    author: 'wanggong',
    approver: '',
    description: '关闭密码弱认证、写入堡垒机免密公钥、开启 chronyd 时间同步与内核审计规则。',
    timeoutSec: 300,
    required: true,
    params: [
      {
        name: 'ntp_server',
        label: '时钟同步服务器',
        type: 'hostname',
        required: true,
        list: false,
        default: 'ntp.intra.lober.local',
        options: [],
      },
    ],
    impls: [
      {
        family: 'linux',
        series: ['rocky', 'ubuntu', 'debian'],
        kind: 'script',
        interpreter: 'bash',
        timings: ['firstboot'],
        body: `#!/usr/bin/env bash
set -euo pipefail
sed -i 's/^#PermitRootLogin.*/PermitRootLogin prohibit-password/' /etc/ssh/sshd_config
echo "server {{ .ntp_server }} iburst" >> /etc/chrony.conf
systemctl restart chronyd`,
      },
    ],
    problems: [],
  },
  {
    id: 'hook-node-exporter',
    name: '安装 Prometheus 硬件监控采集器',
    version: 1,
    rev: 1,
    state: 'draft',
    builtin: false,
    author: 'wanggong',
    approver: '',
    description: '在首次启动阶段自动部署 node_exporter 并注册到机房 Consul 服务发现。',
    timeoutSec: 240,
    required: false,
    params: [],
    impls: [
      {
        family: 'linux',
        series: ['rocky', 'ubuntu'],
        kind: 'script',
        interpreter: 'bash',
        timings: ['firstboot'],
        body: `#!/usr/bin/env bash
echo "Installing node_exporter v1.8.2..."`,
      },
    ],
    problems: [],
  },
];

const templates: Array<Record<string, any>> = [
  {
    id: 'tpl-rocky-prod',
    name: 'Rocky Linux 9 生产计算节点标准模板',
    version: 3,
    rev: 5,
    family: 'linux',
    series: ['rocky', 'almalinux'],
    state: 'approved',
    builtin: false,
    author: 'wanggong',
    approver: 'zhaoshen',
    derivedFrom: { id: 'tpl-builtin-rocky', name: 'Rocky Linux 9 内置基础模板', version: 1 },
    body: `#version=RHEL9
text
reboot --eject
lang zh_CN.UTF-8
keyboard us
timezone Asia/Shanghai --utc
rootpw --iscrypted {{ .root_pwhash }}
bootloader --location=mbr --boot-drive={{ .target_disk }}
clearpart --all --initlabel --drives={{ .target_disk }}
autopart --type=lvm

# {{lober:slot packages}}
%packages
@^minimal-environment
chrony
NetworkManager
%end

# {{lober:slot post-target exec=post-target}}
# {{lober:slot firstboot exec=firstboot}}
`,
    params: [
      {
        name: 'hostname',
        label: '主机名',
        type: 'hostname',
        required: true,
        list: false,
        default: 'node-01.lober.local',
        options: [],
      },
      {
        name: 'target_disk',
        label: '系统安装目标盘',
        type: 'word',
        required: true,
        list: false,
        default: 'nvme0n1',
        options: [],
      },
      {
        name: 'profile',
        label: '节点规格配置',
        type: 'enum',
        required: true,
        list: false,
        default: 'compute',
        options: [
          { value: 'compute', label: '标准计算节点（LVM + 25G Bond）' },
          { value: 'storage', label: '高密存储节点（独立系统盘 + 多数据盘）' },
          { value: 'minimal', label: '最小化基础系统' },
        ],
      },
      {
        name: 'root_pwhash',
        label: '管理员初始密码哈希',
        type: 'pwhash',
        required: true,
        list: false,
        default: '$6$rounds=4096$loberprod$K9x8m7V1p2Q3r4S5t6U7v8W9x0Y1z2A3B4C5D6E7F8G9',
        options: [],
      },
    ],
    bindings: {
      hostname: { source: 'inherit', value: '', attr: '', editable: true },
      target_disk: { source: 'inherit', value: '', attr: '', editable: true },
      profile: { source: 'default', value: 'compute', attr: '', editable: true },
      root_pwhash: {
        source: 'fixed',
        value: '$6$rounds=4096$loberprod$K9x8m7V1p2Q3r4S5t6U7v8W9x0Y1z2A3B4C5D6E7F8G9',
        attr: '',
        editable: false,
      },
    },
    assembly: {
      'post-target': [
        {
          hookID: 'hook-net-static',
          hookVersion: 2,
          timeoutSec: 120,
          required: true,
          bindings: {
            mgmt_ip: { source: 'attr', value: '', attr: 'ip', editable: true },
            gateway: { source: 'default', value: '10.24.1.254', attr: '', editable: true },
          },
        },
        {
          hookID: 'hook-repo-mirror',
          hookVersion: 1,
          timeoutSec: 180,
          required: true,
          bindings: {},
        },
      ],
      firstboot: [],
    },
    problems: [],
  },
  {
    id: 'tpl-ubuntu-k8s',
    name: 'Ubuntu 24.04 云原生容器节点模板',
    version: 2,
    rev: 3,
    family: 'linux',
    series: ['ubuntu'],
    state: 'approved',
    builtin: false,
    author: 'wanggong',
    approver: 'zhaoshen',
    derivedFrom: null,
    body: `#cloud-config
autoinstall:
  version: 1
  locale: zh_CN.UTF-8
  keyboard: { layout: us }
  identity:
    hostname: {{ .hostname }}
    username: ops
    password: "$6$lober$H8j9K0l1M2n3O4p5Q6r7S8t9U0v1W2x3Y4z5"
  storage:
    layout:
      name: lvm
      match:
        path: /dev/{{ .target_disk }}
# {{lober:slot post-target exec=post-target}}
`,
    params: [
      {
        name: 'hostname',
        label: '节点主机名',
        type: 'hostname',
        required: true,
        list: false,
        default: 'k8s-worker-01',
        options: [],
      },
      {
        name: 'target_disk',
        label: '系统安装目标盘',
        type: 'word',
        required: true,
        list: false,
        default: 'nvme0n1',
        options: [],
      },
    ],
    bindings: {},
    assembly: {
      'post-target': [
        {
          hookID: 'hook-repo-mirror',
          hookVersion: 1,
          timeoutSec: 180,
          required: true,
          bindings: {},
        },
      ],
    },
    problems: [],
  },
  {
    id: 'tpl-esxi-8',
    name: 'VMware ESXi 8.0 虚拟化宿主机内置模板',
    version: 1,
    rev: 1,
    family: 'esxi',
    series: ['esxi'],
    state: 'approved',
    builtin: true,
    author: 'system',
    approver: 'system',
    derivedFrom: null,
    body: `vmaccepteula
rootpw --iscrypted $6$esxi$9876543210fedcba
install --firstdisk=local --overwritevmfs
network --bootproto=dhcp
reboot
`,
    params: [
      {
        name: 'hostname',
        label: 'ESXi 主机名',
        type: 'hostname',
        required: true,
        list: false,
        default: 'esxi-node-01.lober.local',
        options: [],
      },
    ],
    bindings: {},
    assembly: {},
    problems: [],
  },
  {
    id: 'tpl-rocky-storage-draft',
    name: 'Rocky Linux 9 分布式存储节点模板（待审批版）',
    version: 1,
    rev: 2,
    family: 'linux',
    series: ['rocky'],
    state: 'review',
    builtin: false,
    author: 'wanggong',
    approver: '',
    derivedFrom: { id: 'tpl-rocky-prod', name: 'Rocky Linux 9 生产计算节点标准模板', version: 3 },
    body: `#version=RHEL9
text
reboot --eject
lang zh_CN.UTF-8
bootloader --location=mbr --boot-drive={{ .target_disk }}
# {{lober:slot post-target exec=post-target}}
`,
    params: [
      {
        name: 'target_disk',
        label: '系统盘设备名',
        type: 'word',
        required: true,
        list: false,
        default: 'sda',
        options: [],
      },
    ],
    bindings: {},
    assembly: {
      'post-target': [],
    },
    problems: [],
  },
];

const tasks: Array<Record<string, any>> = [
  {
    id: 'task-20261120-01',
    batchID: 'batch-20261120-prod',
    hostSN: 'SRV-BJ-A01-01',
    isoID: 'iso-rocky-9.4-x86_64',
    templateID: 'tpl-rocky-prod',
    templateVersion: 3,
    state: 'installing',
    createdBy: 'liming',
    createdAt: nowSec() - 920,
    rev: 3,
    canCancel: [],
    canRetry: [{ code: 'running', message: '任务正在执行中，无法重试' }],
    snapshot: {
      schema: 1,
      family: 'linux',
      series: 'rocky',
      format: 'kickstart',
      host: {
        sn: 'SRV-BJ-A01-01',
        arch: 'x86_64',
        firmware: 'uefi',
        attrs: { datacenter: '北京亦庄一区', rack: 'A01-U12', ip: '10.24.1.11' },
      },
      iso: {
        id: 'iso-rocky-9.4-x86_64',
        name: 'Rocky Linux 9.4 Minimal x86_64',
        version: '9.4',
        arch: 'x86_64',
        artifacts: {},
      },
      template: {
        id: 'tpl-rocky-prod',
        name: 'Rocky Linux 9 生产计算节点标准模板',
        version: 3,
        body: '',
        params: {
          hostname: { items: ['bj-compute-01.lober.local'] },
          target_disk: { items: ['nvme0n1'] },
        },
      },
      slots: [
        {
          seq: 1,
          name: 'post-target',
          timing: 'post-target',
          instances: [
            {
              hookID: 'hook-net-static',
              hookName: '配置生产静态网络与网关',
              hookVersion: 2,
              required: true,
              timeoutSec: 120,
              params: {
                mgmt_ip: { items: ['10.24.1.11/24'] },
                gateway: { items: ['10.24.1.254'] },
              },
            },
            {
              hookID: 'hook-repo-mirror',
              hookName: '注入内网软件源与企业根证书',
              hookVersion: 1,
              required: true,
              timeoutSec: 180,
              params: {
                mirror_host: { items: ['mirrors.intra.lober.local'] },
              },
            },
          ],
        },
      ],
    },
  },
  {
    id: 'task-20261120-02',
    batchID: 'batch-20261120-prod',
    hostSN: 'SRV-BJ-A01-02',
    isoID: 'iso-rocky-9.4-x86_64',
    templateID: 'tpl-rocky-prod',
    templateVersion: 3,
    state: 'initializing',
    createdBy: 'liming',
    createdAt: nowSec() - 920,
    rev: 4,
    canCancel: [{ code: 'rebooted', message: '主机已写入磁盘并进入首次启动，不支持中途取消' }],
    canRetry: [{ code: 'running', message: '任务正在执行中，无法重试' }],
    snapshot: {
      schema: 1,
      family: 'linux',
      series: 'rocky',
      format: 'kickstart',
      host: {
        sn: 'SRV-BJ-A01-02',
        arch: 'x86_64',
        firmware: 'uefi',
        attrs: { datacenter: '北京亦庄一区', rack: 'A01-U14', ip: '10.24.1.12' },
      },
      iso: {
        id: 'iso-rocky-9.4-x86_64',
        name: 'Rocky Linux 9.4 Minimal x86_64',
        version: '9.4',
        arch: 'x86_64',
        artifacts: {},
      },
      template: {
        id: 'tpl-rocky-prod',
        name: 'Rocky Linux 9 生产计算节点标准模板',
        version: 3,
        body: '',
        params: {
          hostname: { items: ['bj-compute-02.lober.local'] },
          target_disk: { items: ['nvme0n1'] },
        },
      },
      slots: [],
    },
  },
  {
    id: 'task-20261120-03',
    batchID: 'batch-20261120-prod',
    hostSN: 'SRV-BJ-A01-03',
    isoID: 'iso-rocky-9.4-x86_64',
    templateID: 'tpl-rocky-prod',
    templateVersion: 3,
    state: 'failed',
    createdBy: 'liming',
    createdAt: nowSec() - 920,
    rev: 4,
    canCancel: [{ code: 'terminal', message: '任务已结束，无需取消' }],
    canRetry: [],
    snapshot: {
      schema: 1,
      family: 'linux',
      series: 'rocky',
      format: 'kickstart',
      host: {
        sn: 'SRV-BJ-A01-03',
        arch: 'x86_64',
        firmware: 'uefi',
        attrs: { datacenter: '北京亦庄一区', rack: 'A01-U16', ip: '10.24.1.13' },
      },
      iso: {
        id: 'iso-rocky-9.4-x86_64',
        name: 'Rocky Linux 9.4 Minimal x86_64',
        version: '9.4',
        arch: 'x86_64',
        artifacts: {},
      },
      template: {
        id: 'tpl-rocky-prod',
        name: 'Rocky Linux 9 生产计算节点标准模板',
        version: 3,
        body: '',
        params: {
          hostname: { items: ['bj-storage-03.lober.local'] },
          target_disk: { items: ['sda'] },
        },
      },
      slots: [
        {
          seq: 1,
          name: 'post-target',
          timing: 'post-target',
          instances: [
            {
              hookID: 'hook-net-static',
              hookName: '配置生产静态网络与网关',
              hookVersion: 2,
              required: true,
              timeoutSec: 120,
              params: {
                mgmt_ip: { items: ['10.24.1.13/24'] },
                gateway: { items: ['10.24.1.254'] },
              },
            },
          ],
        },
      ],
    },
  },
  {
    id: 'task-20261120-04',
    batchID: 'batch-20261120-k8s',
    hostSN: 'SRV-BJ-A02-01',
    isoID: 'iso-ubuntu-24.04-x86_64',
    templateID: 'tpl-ubuntu-k8s',
    templateVersion: 2,
    state: 'pending',
    createdBy: 'admin',
    createdAt: nowSec() - 480,
    rev: 1,
    canCancel: [],
    canRetry: [{ code: 'not_failed', message: '只有失败或已取消的任务可以重试' }],
    snapshot: {
      schema: 1,
      family: 'linux',
      series: 'ubuntu',
      format: 'autoinstall',
      host: {
        sn: 'SRV-BJ-A02-01',
        arch: 'x86_64',
        firmware: 'uefi',
        attrs: { datacenter: '北京亦庄二区', rack: 'A02-U08', ip: '10.24.2.21' },
      },
      iso: {
        id: 'iso-ubuntu-24.04-x86_64',
        name: 'Ubuntu Server 24.04.1 LTS x86_64',
        version: '24.04.1',
        arch: 'x86_64',
        artifacts: {},
      },
      template: {
        id: 'tpl-ubuntu-k8s',
        name: 'Ubuntu 24.04 云原生容器节点模板',
        version: 2,
        body: '',
        params: {
          hostname: { items: ['k8s-worker-01'] },
          target_disk: { items: ['nvme0n1'] },
        },
      },
      slots: [],
    },
  },
  {
    id: 'task-20261120-05',
    batchID: 'batch-20261120-k8s',
    hostSN: 'SRV-BJ-A02-02',
    isoID: 'iso-ubuntu-24.04-x86_64',
    templateID: 'tpl-ubuntu-k8s',
    templateVersion: 2,
    state: 'booting',
    createdBy: 'admin',
    createdAt: nowSec() - 480,
    rev: 2,
    canCancel: [],
    canRetry: [{ code: 'running', message: '任务正在执行中，无法重试' }],
    snapshot: {
      schema: 1,
      family: 'linux',
      series: 'ubuntu',
      format: 'autoinstall',
      host: {
        sn: 'SRV-BJ-A02-02',
        arch: 'x86_64',
        firmware: 'uefi',
        attrs: { datacenter: '北京亦庄二区', rack: 'A02-U10', ip: '10.24.2.22' },
      },
      iso: {
        id: 'iso-ubuntu-24.04-x86_64',
        name: 'Ubuntu Server 24.04.1 LTS x86_64',
        version: '24.04.1',
        arch: 'x86_64',
        artifacts: {},
      },
      template: {
        id: 'tpl-ubuntu-k8s',
        name: 'Ubuntu 24.04 云原生容器节点模板',
        version: 2,
        body: '',
        params: {
          hostname: { items: ['k8s-worker-02'] },
          target_disk: { items: ['nvme0n1'] },
        },
      },
      slots: [],
    },
  },
  {
    id: 'task-20261119-06',
    batchID: '',
    hostSN: 'SRV-SH-B01-01',
    isoID: 'iso-esxi-8.0u3-x86_64',
    templateID: 'tpl-esxi-8',
    templateVersion: 1,
    state: 'done',
    createdBy: 'liming',
    createdAt: nowSec() - 18_600,
    rev: 6,
    canCancel: [{ code: 'terminal', message: '任务已完成，不可取消' }],
    canRetry: [{ code: 'done', message: '任务已完成，如需重装请新建任务' }],
    snapshot: {
      schema: 1,
      family: 'esxi',
      series: 'esxi',
      format: 'esxi-ks',
      host: {
        sn: 'SRV-SH-B01-01',
        arch: 'x86_64',
        firmware: 'uefi',
        attrs: { datacenter: '上海嘉定机房', rack: 'B01-U04', ip: '10.32.1.15' },
      },
      iso: {
        id: 'iso-esxi-8.0u3-x86_64',
        name: 'VMware ESXi 8.0 Update 3 Standard',
        version: '8.0.3',
        arch: 'x86_64',
        artifacts: {},
      },
      template: {
        id: 'tpl-esxi-8',
        name: 'VMware ESXi 8.0 虚拟化宿主机内置模板',
        version: 1,
        body: '',
        params: {},
      },
      slots: [],
    },
  },
];

const taskEvents: Record<string, Array<Record<string, any>>> = {
  'task-20261120-01': [
    {
      seq: 1,
      taskID: 'task-20261120-01',
      at: nowSec() - 920,
      kind: 'task.created',
      key: 'created',
      fields: { actor: 'liming', batch: 'batch-20261120-prod' },
    },
    {
      seq: 2,
      taskID: 'task-20261120-01',
      at: nowSec() - 810,
      kind: 'machine.boot',
      key: 'boot-1',
      fields: { arch: 'x86_64', firmware: 'uefi', ip: '10.24.1.11' },
    },
    {
      seq: 3,
      taskID: 'task-20261120-01',
      at: nowSec() - 780,
      kind: 'config.fetch',
      key: 'cfg-1',
      fields: { format: 'kickstart', file: 'kickstart.cfg' },
    },
    {
      seq: 4,
      taskID: 'task-20261120-01',
      at: nowSec() - 520,
      kind: 'installer.progress',
      key: 'prog-1',
      fields: { stage: '已格式化目标盘 nvme0n1，正在写入基础软件包 (248/412)…' },
    },
    {
      seq: 5,
      taskID: 'task-20261120-01',
      at: nowSec() - 120,
      kind: 'hook.report',
      key: 'hook-1-1',
      fields: {
        slot: '1',
        timing: 'post-target',
        instance: '1',
        rc: '0',
        log_base64: toB64('[Lober] 管理网 eno1 静态 IP 10.24.1.11/24 配置完成，网关连通性测试通过。'),
      },
    },
  ],
  'task-20261120-03': [
    {
      seq: 1,
      taskID: 'task-20261120-03',
      at: nowSec() - 920,
      kind: 'task.created',
      key: 'created',
      fields: { actor: 'liming' },
    },
    {
      seq: 2,
      taskID: 'task-20261120-03',
      at: nowSec() - 840,
      kind: 'machine.boot',
      key: 'boot-1',
      fields: { arch: 'x86_64', firmware: 'uefi', ip: '10.24.1.13' },
    },
    {
      seq: 3,
      taskID: 'task-20261120-03',
      at: nowSec() - 790,
      kind: 'config.fetch',
      key: 'cfg-1',
      fields: { format: 'kickstart', file: 'kickstart.cfg' },
    },
    {
      seq: 4,
      taskID: 'task-20261120-03',
      at: nowSec() - 340,
      kind: 'hook.report',
      key: 'hook-1-1-fail',
      fields: {
        slot: '1',
        timing: 'post-target',
        instance: '1',
        rc: '1',
        reason: '管理网网关 10.24.1.254 ARP 探测超时（VLAN 101 未放行）',
        log_base64: toB64(
          `+ nmcli con mod 'System eth0' ipv4.addresses 10.24.1.13/24 ipv4.gateway 10.24.1.254
+ nmcli con up 'System eth0'
+ arping -c 3 -I eth0 10.24.1.254
ARPING 10.24.1.254 from 10.24.1.13 eth0
Sent 3 probes (3 broadcast(s))
Received 0 response(s)
[ERROR] 网关 10.24.1.254 不可达，请检查交换机端口 A01-U16 的 VLAN 划分！`,
        ),
      },
    },
    {
      seq: 5,
      taskID: 'task-20261120-03',
      at: nowSec() - 339,
      kind: 'task.failed',
      key: 'failed',
      fields: { reason: '必需的装后脚本 #1（配置生产静态网络与网关）执行失败（退出码 1）' },
    },
  ],
};

const audits: Array<Record<string, any>> = [
  {
    id: 108,
    at: nowSec() - 300,
    actor: 'liming',
    action: 'task.batch_create',
    targetKind: 'task',
    targetID: 'batch-20261120-prod',
    detail: '批量创建 3 台 Rocky Linux 9.4 生产计算节点装机任务（SRV-BJ-A01-01 ~ 03）',
  },
  {
    id: 107,
    at: nowSec() - 480,
    actor: 'admin',
    action: 'task.batch_create',
    targetKind: 'task',
    targetID: 'batch-20261120-k8s',
    detail: '批量创建 2 台 Ubuntu 24.04 容器节点装机任务（SRV-BJ-A02-01 ~ 02）',
  },
  {
    id: 106,
    at: nowSec() - 1800,
    actor: 'admin',
    action: 'host.inventory_request',
    targetKind: 'host',
    targetID: 'SRV-BJ-A02-01',
    detail: '发起硬件部件与磁盘清单采集请求，主机已回传 2 块 NVMe 磁盘信息',
  },
  {
    id: 105,
    at: nowSec() - 3600,
    actor: 'zhaoshen',
    action: 'template.approve',
    targetKind: 'template',
    targetID: 'tpl-rocky-prod@v3',
    detail: '批准装机模板《Rocky Linux 9 生产计算节点标准模板》v3 版本上线',
  },
  {
    id: 104,
    at: nowSec() - 5400,
    actor: 'wanggong',
    action: 'hook.submit',
    targetKind: 'hook',
    targetID: 'hook-security-baseline@v1',
    detail: '提交脚本组件《等保三级基线加固与运维公钥下发》v1 进入审批',
  },
  {
    id: 103,
    at: nowSec() - 7200,
    actor: 'liming',
    action: 'iso.create',
    targetKind: 'iso',
    targetID: 'iso-debian-12.7-arm64',
    detail: '登记远程镜像《Debian 12.7 Bookworm ARM64 Netinst》，开始后台拉取',
  },
  {
    id: 102,
    at: nowSec() - 14_400,
    actor: 'admin',
    action: 'user.update',
    targetKind: 'user',
    targetID: 'liming',
    detail: '授予账号 liming 机房运维操作员（operator）角色',
  },
  {
    id: 101,
    at: nowSec() - 18_600,
    actor: 'liming',
    action: 'task.done',
    targetKind: 'task',
    targetID: 'task-20261119-06',
    detail: '主机 SRV-SH-B01-01 完成 VMware ESXi 8.0U3 自动化安装与首次启动回报',
  },
];

function paginate<T>(items: T[], query: URLSearchParams) {
  const page = Math.max(1, Number(query.get('page') || 1));
  const pageSize = Math.max(1, Number(query.get('page_size') || 20));
  const start = (page - 1) * pageSize;
  return {
    items: items.slice(start, start + pageSize),
    total: items.length,
    next: '',
  };
}

function readBody(req: IncomingMessage): Promise<string> {
  return new Promise((resolve) => {
    const chunks: Buffer[] = [];
    req.on('data', (chunk) => chunks.push(Buffer.from(chunk)));
    req.on('end', () => resolve(Buffer.concat(chunks).toString('utf8')));
    req.on('error', () => resolve(''));
  });
}

function sendJSON(res: ServerResponse, data: unknown, status = 200) {
  res.statusCode = status;
  res.setHeader('Content-Type', 'application/json; charset=utf-8');
  res.end(JSON.stringify({ code: status >= 400 ? status : 0, data, message: '' }));
}

function sendError(
  res: ServerResponse,
  status: number,
  message: string,
  problems: Array<Record<string, any>> = [],
) {
  res.statusCode = status;
  res.setHeader('Content-Type', 'application/json; charset=utf-8');
  res.end(
    JSON.stringify({
      code: status,
      message,
      data: problems.length ? { problems } : undefined,
    }),
  );
}

export async function handleConsoleMockRequest(
  req: IncomingMessage,
  res: ServerResponse,
): Promise<boolean> {
  const rawUrl = req.url || '/';
  if (!rawUrl.startsWith('/console/v1')) return false;

  const parsed = new URL(rawUrl, 'http://localhost');
  const path = parsed.pathname.slice('/console/v1'.length) || '/';
  const query = parsed.searchParams;
  const method = (req.method || 'GET').toUpperCase();

  // Check role token header if mock-token-<username> is passed
  const authHeader = req.headers.authorization || '';
  if (authHeader.startsWith('Bearer mock-user-')) {
    const requestedUser = authHeader.slice('Bearer mock-user-'.length).trim();
    if (users.some((u) => u.username === requestedUser)) {
      activeUsername = requestedUser;
    }
  }

  // 1. Session & Auth (/auth/*, /user/info, and /session/*)
  if ((path === '/auth/login' || path === '/session/login') && method === 'POST') {
    const body = JSON.parse((await readBody(req)) || '{}');
    const username = String(body.username || 'admin').trim();
    const matched = users.find((u) => u.username === username) ?? users[0]!;
    activeUsername = matched.username;
    const sessionUser = buildSessionUser(activeUsername);
    sendJSON(res, {
      token: `mock-user-${activeUsername}`,
      expiresAt: nowSec() + 86_400,
      user: sessionUser,
    });
    return true;
  }

  if (
    (path === '/auth/switch-role' || path === '/session/switch-role') &&
    method === 'POST'
  ) {
    const body = JSON.parse((await readBody(req)) || '{}');
    const targetRole = String(body.role || 'admin');
    const matched = users.find((u) => u.role === targetRole) ?? users[0]!;
    activeUsername = matched.username;
    const sessionUser = buildSessionUser(activeUsername);
    sendJSON(res, {
      token: `mock-user-${activeUsername}`,
      expiresAt: nowSec() + 86_400,
      user: sessionUser,
    });
    return true;
  }

  if (
    (path === '/auth/logout' || path === '/session/logout') &&
    method === 'POST'
  ) {
    sendJSON(res, null);
    return true;
  }

  if ((path === '/user/info' || path === '/session') && method === 'GET') {
    sendJSON(res, buildSessionUser(activeUsername));
    return true;
  }

  if (
    (path === '/auth/password' || path === '/session/password') &&
    (method === 'POST' || method === 'PUT')
  ) {
    const user = users.find((u) => u.username === activeUsername);
    if (user) user.passwordChangeRequired = false;
    sendJSON(res, null);
    return true;
  }

  // 2. Tasks & SSE Stream
  const streamMatch = /^\/tasks\/([^/]+)\/stream$/.exec(path);
  if (streamMatch && method === 'GET') {
    const taskId = decodeURIComponent(streamMatch[1]!);
    const task = tasks.find((t) => t.id === taskId);
    if (!task) {
      res.statusCode = 404;
      res.end();
      return true;
    }
    res.statusCode = 200;
    res.setHeader('Content-Type', 'text/event-stream; charset=utf-8');
    res.setHeader('Cache-Control', 'no-cache');
    res.setHeader('Connection', 'keep-alive');
    res.write(': connected to lober mock sse\n\n');

    const timer = setInterval(() => {
      if (res.writableEnded) {
        clearInterval(timer);
        return;
      }
      if (task.state === 'installing' || task.state === 'booting') {
        const list = (taskEvents[taskId] ??= []);
        const nextSeq = (list.at(-1)?.seq ?? 0) + 1;
        const ev = {
          seq: nextSeq,
          taskID: taskId,
          at: nowSec(),
          kind: 'installer.progress',
          key: `live-${nextSeq}`,
          fields: {
            stage: `正在同步内核组件与装后配置 (数据包 #${nextSeq})…`,
          },
        };
        list.push(ev);
        res.write(`id: ${ev.seq}\nevent: task\ndata: ${JSON.stringify(ev)}\n\n`);
      }
    }, 6000);

    req.on('close', () => clearInterval(timer));
    return true;
  }

  const taskEventsMatch = /^\/tasks\/([^/]+)\/events$/.exec(path);
  if (taskEventsMatch && method === 'GET') {
    const taskId = decodeURIComponent(taskEventsMatch[1]!);
    const afterSeq = Number(query.get('after_seq') || 0);
    const list = taskEvents[taskId] ?? [
      {
        seq: 1,
        taskID: taskId,
        at: nowSec() - 600,
        kind: 'task.created',
        key: 'created',
        fields: { actor: 'admin' },
      },
    ];
    sendJSON(res, {
      items: list.filter((e) => e.seq > afterSeq),
    });
    return true;
  }

  const taskActionMatch = /^\/tasks\/([^/]+)\/(cancel|retry|preview)$/.exec(path);
  if (taskActionMatch && method === 'POST') {
    const taskId = decodeURIComponent(taskActionMatch[1]!);
    const action = taskActionMatch[2]!;
    const task = tasks.find((t) => t.id === taskId);
    if (!task) {
      sendError(res, 404, '未找到该任务');
      return true;
    }
    if (action === 'preview') {
      sendJSON(res, {
        sample: [],
        problems: [],
        files: [
          {
            kind: 'config',
            name: task.snapshot.format === 'autoinstall' ? 'autoinstall.yaml' : 'kickstart.cfg',
            slot: '',
            digest: 'sha256:9f86d081884c7d659a2feaa0c55ad015',
            sample: false,
            body: `# Lober 固化快照生成文件 (${task.hostSN})\n# 镜像: ${task.snapshot.iso.name}\n# 模板: ${task.snapshot.template.name} v${task.templateVersion}\n\ntext\nreboot --eject\nlang zh_CN.UTF-8\ntimezone Asia/Shanghai --utc\nbootloader --location=mbr --boot-drive=nvme0n1\nclearpart --all --initlabel --drives=nvme0n1\nautopart --type=lvm\n`,
          },
          {
            kind: 'hook',
            name: '01-hook-net-static.sh',
            slot: 'post-target',
            digest: 'sha256:4b227777d4dd1fc61c6f884f48641d02',
            sample: false,
            body: `#!/usr/bin/env bash\nset -euo pipefail\nnmcli con mod "System eno1" ipv4.addresses "${task.snapshot.host.attrs?.ip || '10.24.1.11'}/24" ipv4.gateway "10.24.1.254" ipv4.method manual\n`,
          },
        ],
      });
      return true;
    }
    if (action === 'cancel') {
      task.state = 'cancelled';
      task.rev += 1;
      task.canCancel = [{ code: 'terminal', message: '任务已取消' }];
      task.canRetry = [];
      (taskEvents[taskId] ??= []).push({
        seq: (taskEvents[taskId]?.at(-1)?.seq ?? 0) + 1,
        taskID: taskId,
        at: nowSec(),
        kind: 'task.cancelled',
        key: `cancel-${nowSec()}`,
        fields: { actor: activeUsername, reason: '运维人员在控制台手动取消任务' },
      });
      sendJSON(res, task);
      return true;
    }
    if (action === 'retry') {
      task.state = 'pending';
      task.rev += 1;
      task.canCancel = [];
      task.canRetry = [{ code: 'pending', message: '任务已在等待机器引导' }];
      (taskEvents[taskId] ??= []).push({
        seq: (taskEvents[taskId]?.at(-1)?.seq ?? 0) + 1,
        taskID: taskId,
        at: nowSec(),
        kind: 'task.retried',
        key: `retry-${nowSec()}`,
        fields: { actor: activeUsername },
      });
      sendJSON(res, task);
      return true;
    }
  }

  const taskDetailMatch = /^\/tasks\/([^/]+)$/.exec(path);
  if (taskDetailMatch && method === 'GET') {
    const taskId = decodeURIComponent(taskDetailMatch[1]!);
    const task = tasks.find((t) => t.id === taskId);
    if (!task) {
      sendError(res, 404, '任务不存在');
      return true;
    }
    sendJSON(res, task);
    return true;
  }

  if (path === '/tasks' && method === 'GET') {
    let filtered = [...tasks];
    const sn = (query.get('host_sn') || '').trim();
    const state = (query.get('state') || '').trim();
    const batchId = (query.get('batch_id') || '').trim();
    const from = Number(query.get('created_from') || 0);
    const to = Number(query.get('created_to') || 0);
    if (sn) filtered = filtered.filter((t) => t.hostSN.toLowerCase().includes(sn.toLowerCase()));
    if (state) filtered = filtered.filter((t) => t.state === state);
    if (batchId) filtered = filtered.filter((t) => t.batchID === batchId);
    if (from > 0) filtered = filtered.filter((t) => t.createdAt >= from);
    if (to > 0) filtered = filtered.filter((t) => t.createdAt <= to);
    sendJSON(res, paginate(filtered, query));
    return true;
  }

  const batchMatch = /^\/task-batches\/([^/]+)$/.exec(path);
  if (batchMatch && method === 'GET') {
    const batchId = decodeURIComponent(batchMatch[1]!);
    const batchTasks = tasks.filter((t) => t.batchID === batchId);
    const first = batchTasks[0] ?? tasks[0]!;
    sendJSON(res, {
      id: batchId,
      total: batchTasks.length,
      pending: batchTasks.filter((t) => t.state === 'pending').length,
      active: batchTasks.filter((t) => ['booting', 'installing', 'rebooting', 'initializing'].includes(t.state)).length,
      done: batchTasks.filter((t) => t.state === 'done').length,
      failed: batchTasks.filter((t) => t.state === 'failed').length,
      cancelled: batchTasks.filter((t) => t.state === 'cancelled').length,
      createdBy: first.createdBy,
      createdAt: first.createdAt,
      isoName: first.snapshot.iso.name,
      templateName: first.snapshot.template.name,
      templateVersion: first.templateVersion,
    });
    return true;
  }

  // 3. Wizard endpoints
  if (path === '/wizard/hosts' && method === 'GET') {
    const q = (query.get('q') || '').trim().toLowerCase();
    const attrKey = (query.get('attr_key') || '').trim();
    const attrValue = (query.get('attr_value') || '').trim();
    const activeHostSNs = new Set(
      tasks
        .filter((t) => ['pending', 'booting', 'installing', 'rebooting', 'initializing'].includes(t.state))
        .map((t) => t.hostSN),
    );
    let items = hosts.map((h) => {
      const busy = activeHostSNs.has(h.sn);
      const cleanAttrs = Object.fromEntries(
        Object.entries(h.attrs ?? {}).filter(([k]) => !k.startsWith('inv_')),
      );
      return {
        sn: h.sn,
        arch: h.arch,
        firmware: h.firmware,
        attrs: cleanAttrs,
        selectable: !busy,
        problems: busy
          ? [
              {
                code: 'host_busy',
                message: '该主机存在未结束的装机任务',
                next: '可前往任务列表查看进度，或取消/等待当前任务完成后再选。',
              },
            ]
          : [],
      };
    });
    if (q) items = items.filter((h) => h.sn.toLowerCase().includes(q));
    if (attrKey) {
      items = items.filter((h) =>
        attrValue
          ? String(h.attrs[attrKey] ?? '').includes(attrValue)
          : Object.hasOwn(h.attrs, attrKey),
      );
    }
    sendJSON(res, paginate(items, query));
    return true;
  }

  if (path === '/wizard/isos' && method === 'POST') {
    const items = isos.map((iso) => ({
      id: iso.id,
      name: iso.name,
      family: iso.family,
      series: iso.series,
      version: iso.version,
      arch: iso.arch,
      usable: iso.state === 'ready',
      problems:
        iso.state === 'ready'
          ? []
          : [
              {
                code: 'iso_not_ready',
                message:
                  iso.state === 'deprecated'
                    ? '该镜像已禁用，如需使用请先在系统镜像页面启用'
                    : '该镜像尚未完成导入，暂时不可用于装机',
              },
            ],
    }));
    sendJSON(res, { items, total: items.length, next: '' });
    return true;
  }

  if (path === '/wizard/templates' && method === 'POST') {
    const body = JSON.parse((await readBody(req)) || '{}');
    const iso = isos.find((i) => i.id === body.isoID);
    const items = templates
      .filter((t) => !iso || t.family === iso.family)
      .map((t) => ({
        id: t.id,
        name: t.name,
        version: t.version,
        builtin: t.builtin,
        usable: t.state === 'approved',
        problems:
          t.state === 'approved'
            ? []
            : [{ code: 'not_approved', message: '该模板版本尚未审批通过，审批后才可用于创建装机任务' }],
      }));
    sendJSON(res, { items, total: items.length, next: '' });
    return true;
  }

  if (path === '/wizard/params' && method === 'POST') {
    const body = JSON.parse((await readBody(req)) || '{}');
    const hostSNs: string[] = body.hostSNs ?? [];
    const entered: Record<string, Record<string, string>> = body.entered ?? {};
    const tpl = templates.find((t) => t.id === body.templateID) ?? templates[0]!;
    const hostForms = hostSNs.map((sn) => {
      const hostObj = hosts.find((h) => h.sn === sn);
      const parsedDisks = (() => {
        try {
          return JSON.parse(hostObj?.attrs?.inv_disks || '[]');
        } catch {
          return [];
        }
      })();
      const defaultDisk = parsedDisks[0]?.device || 'nvme0n1';
      const fields = (tpl.params ?? []).map((p: any) => {
        const key = `tpl/${p.name}`;
        const prefill =
          p.name === 'hostname'
            ? `${sn.toLowerCase()}.lober.local`
            : p.name === 'target_disk'
              ? defaultDisk
              : p.default || '';
        const locked = tpl.bindings?.[p.name]?.editable === false;
        return {
          key,
          label: p.label || p.name,
          type: p.type,
          required: p.required,
          list: p.list,
          locked,
          prefill,
          options: p.options ?? [],
        };
      });
      const currentDiskVal = entered[sn]?.['tpl/target_disk'] || defaultDisk;
      const hostProblems: Array<Record<string, any>> = [];
      const hostnameVal = entered[sn]?.['tpl/hostname'] ?? `${sn.toLowerCase()}.lober.local`;
      if (!hostnameVal.trim() || hostnameVal.includes(' ')) {
        hostProblems.push({
          code: 'invalid_hostname',
          message: '主机名不能为空且不能包含空格',
          subject: { kind: 'param', id: sn, path: 'tpl/hostname', line: 0, col: 0 },
        });
      }
      return {
        sn,
        fields,
        problems: hostProblems,
        targetDisk: fields.some((f: any) => f.key === 'tpl/target_disk')
          ? {
              key: 'tpl/target_disk',
              mode: 'device',
              value: currentDiskVal,
            }
          : undefined,
      };
    });
    sendJSON(res, { common: [], hosts: hostForms });
    return true;
  }

  if (path === '/wizard/tasks' && method === 'POST') {
    const body = JSON.parse((await readBody(req)) || '{}');
    const iso = isos.find((i) => i.id === body.isoID) ?? isos[0]!;
    const tpl = templates.find((t) => t.id === body.templateID) ?? templates[0]!;
    const batchID = `batch-${nowSec()}`;
    const createdTasks = (body.hosts ?? []).map((h: any, index: number) => {
      const hostObj = hosts.find((item) => item.sn === h.sn) ?? {
        sn: h.sn,
        arch: 'x86_64',
        firmware: 'uefi',
        attrs: {},
      };
      const newTask = {
        id: `task-${nowSec()}-0${index + 1}`,
        batchID,
        hostSN: h.sn,
        isoID: iso.id,
        templateID: tpl.id,
        templateVersion: tpl.version,
        state: 'pending',
        createdBy: activeUsername,
        createdAt: nowSec(),
        rev: 1,
        canCancel: [],
        canRetry: [{ code: 'pending', message: '任务正在等待引导' }],
        snapshot: {
          schema: 1,
          family: iso.family,
          series: iso.series,
          format: 'kickstart',
          host: {
            sn: hostObj.sn,
            arch: hostObj.arch,
            firmware: hostObj.firmware,
            attrs: hostObj.attrs,
          },
          iso: {
            id: iso.id,
            name: iso.name,
            version: iso.version,
            arch: iso.arch,
            artifacts: {},
          },
          template: {
            id: tpl.id,
            name: tpl.name,
            version: tpl.version,
            body: tpl.body,
            params: {},
          },
          slots: [],
        },
      };
      tasks.unshift(newTask);
      return newTask;
    });
    sendJSON(res, { tasks: createdTasks });
    return true;
  }

  // 4. Hosts & Discovery
  const hostDisksMatch = /^\/hosts\/([^/]+)\/disks$/.exec(path);
  if (hostDisksMatch && method === 'GET') {
    const sn = decodeURIComponent(hostDisksMatch[1]!);
    const host = hosts.find((h) => h.sn === sn);
    let disks: Array<Record<string, any>> = [];
    try {
      disks = JSON.parse(host?.attrs?.inv_disks || '[]').map((d: any, i: number) => ({
        device: d.device,
        model: d.model,
        bytes: d.bytes,
        serial: d.serial || `${sn}-DISK-0${i + 1}`,
      }));
    } catch {
      disks = [];
    }
    sendJSON(res, {
      status: disks.length ? 'ready' : 'none',
      disks,
    });
    return true;
  }

  const hostInvReqMatch = /^\/hosts\/([^/]+)\/inventory\/request$/.exec(path);
  if (hostInvReqMatch && method === 'POST') {
    const sn = decodeURIComponent(hostInvReqMatch[1]!);
    const host = hosts.find((h) => h.sn === sn);
    if (!host) {
      sendError(res, 404, '未找到主机');
      return true;
    }
    host.attrs.inv_requested_at = String(nowSec());
    host.rev += 1;
    sendJSON(res, host);
    return true;
  }

  if (path === '/hosts/import' && method === 'POST') {
    const raw = await readBody(req);
    const dryRun = query.get('dryRun') === 'true';
    const lines = raw
      .split(/\r?\n/)
      .map((l) => l.trim())
      .filter(Boolean);
    const headers = (lines[0] || 'sn,arch,firmware').split(',').map((h) => h.trim());
    const rows = lines.slice(1).map((line, i) => {
      const cols = line.split(',').map((c) => c.trim());
      const sn = cols[0] || `ROW-${i + 2}`;
      const exists = hosts.some((h) => h.sn === sn);
      return {
        row: i + 2,
        sn,
        action: exists ? 'update' : 'create',
        problems: [],
        cols,
      };
    });
    if (dryRun) {
      sendJSON(res, {
        total: rows.length,
        rows: rows.map(({ row, sn, action, problems }) => ({ row, sn, action, problems })),
      });
      return true;
    }
    for (const r of rows) {
      const attrs: Record<string, string> = {};
      headers.forEach((h, idx) => {
        if (!['sn', 'arch', 'firmware'].includes(h) && r.cols[idx]) {
          attrs[h] = r.cols[idx]!;
        }
      });
      const existing = hosts.find((h) => h.sn === r.sn);
      if (existing) {
        Object.assign(existing.attrs, attrs);
        existing.rev += 1;
      } else {
        hosts.unshift({
          sn: r.sn,
          arch: r.cols[1] || 'x86_64',
          firmware: r.cols[2] || 'uefi',
          rev: 1,
          attrs,
        });
      }
    }
    sendJSON(res, paginate(hosts, query));
    return true;
  }

  const hostSingleMatch = /^\/hosts\/([^/]+)$/.exec(path);
  if (hostSingleMatch) {
    const sn = decodeURIComponent(hostSingleMatch[1]!);
    const index = hosts.findIndex((h) => h.sn === sn);
    if (method === 'GET') {
      if (index === -1) {
        sendError(res, 404, '主机不存在');
        return true;
      }
      sendJSON(res, hosts[index]);
      return true;
    }
    if (method === 'PUT') {
      const body = JSON.parse((await readBody(req)) || '{}');
      if (index === -1) {
        sendError(res, 404, '主机不存在');
        return true;
      }
      hosts[index] = {
        ...hosts[index],
        arch: body.arch ?? hosts[index]!.arch,
        firmware: body.firmware ?? hosts[index]!.firmware,
        attrs: { ...hosts[index]!.attrs, ...(body.attrs ?? {}) },
        rev: hosts[index]!.rev + 1,
      };
      sendJSON(res, hosts[index]);
      return true;
    }
    if (method === 'DELETE') {
      if (index !== -1) hosts.splice(index, 1);
      sendJSON(res, null);
      return true;
    }
  }

  if (path === '/hosts' && method === 'GET') {
    let filtered = [...hosts];
    const prefix = (query.get('prefix') || '').trim().toLowerCase();
    const attrKey = (query.get('attr_key') || '').trim();
    const attrValue = (query.get('attr_value') || '').trim();
    if (prefix) filtered = filtered.filter((h) => h.sn.toLowerCase().includes(prefix));
    if (attrKey) {
      filtered = filtered.filter((h) =>
        attrValue
          ? String(h.attrs?.[attrKey] ?? '').includes(attrValue)
          : Object.hasOwn(h.attrs ?? {}, attrKey),
      );
    }
    sendJSON(res, paginate(filtered, query));
    return true;
  }

  if (path === '/hosts' && method === 'POST') {
    const body = JSON.parse((await readBody(req)) || '{}');
    const created = {
      sn: body.sn,
      arch: body.arch || 'x86_64',
      firmware: body.firmware || 'uefi',
      attrs: body.attrs || {},
      rev: 1,
    };
    hosts.unshift(created);
    sendJSON(res, created);
    return true;
  }

  // Discoveries
  const discRegisterMatch = /^\/discoveries\/([^/]+)\/register$/.exec(path);
  if (discRegisterMatch && method === 'POST') {
    const mac = decodeURIComponent(discRegisterMatch[1]!);
    const body = JSON.parse((await readBody(req)) || '{}');
    const sn = String(body.sn || '').trim();
    const item = discoveries.find((d) => d.mac === mac);
    if (!item) {
      sendError(res, 404, '未找到发现记录');
      return true;
    }
    item.registered_sn = sn;
    const newHost = {
      sn,
      arch: item.arch || 'x86_64',
      firmware: item.firmware || 'uefi',
      rev: 1,
      attrs: {
        datacenter: '自动发现待分配',
        mac: item.mac,
        inv_collected_at: String(nowSec()),
        inv_system_model: item.system_model || '新发现服务器',
      },
    };
    if (!hosts.some((h) => h.sn === sn)) hosts.unshift(newHost);
    sendJSON(res, newHost);
    return true;
  }

  const discSingleMatch = /^\/discoveries\/([^/]+)$/.exec(path);
  if (discSingleMatch && method === 'GET') {
    const mac = decodeURIComponent(discSingleMatch[1]!);
    const item = discoveries.find((d) => d.mac === mac) ?? discoveries[0]!;
    sendJSON(res, item);
    return true;
  }

  if (path === '/discoveries' && method === 'GET') {
    const regSn = query.get('registered_sn');
    const items = regSn
      ? discoveries.filter((d) => d.registered_sn === regSn)
      : discoveries;
    sendJSON(res, { items, next_cursor: '' });
    return true;
  }

  // 5. ISOs
  const isoProgressMatch = /^\/isos\/([^/]+)\/progress$/.exec(path);
  if (isoProgressMatch && method === 'GET') {
    const id = decodeURIComponent(isoProgressMatch[1]!);
    const iso = isos.find((i) => i.id === id) ?? isos[0]!;
    const isReady = iso.state === 'ready';
    sendJSON(res, {
      id: iso.id,
      state: iso.state,
      sources: [
        {
          role: '',
          kind: 'url',
          state: isReady ? 'complete' : 'fetching',
          fetched: isReady ? 1_073_741_824 : 734_003_200,
          totalSize: 1_073_741_824,
          ranges: [{ start: 0, end: isReady ? 1_073_741_824 : 734_003_200 }],
        },
      ],
    });
    return true;
  }

  const isoSourcesMatch = /^\/isos\/([^/]+)\/(sources|content)$/.exec(path);
  if (isoSourcesMatch && (method === 'POST' || method === 'PUT')) {
    const id = decodeURIComponent(isoSourcesMatch[1]!);
    const iso = isos.find((i) => i.id === id);
    if (iso && isoSourcesMatch[2] === 'content') {
      iso.state = 'ready';
      iso.rev += 1;
    }
    sendJSON(res, null);
    return true;
  }

  const isoOpMatch =
    /^\/isos\/([^/]+)\/(cancel|retry|reimport|deprecate|enable)$/.exec(path);
  if (isoOpMatch && method === 'POST') {
    const id = decodeURIComponent(isoOpMatch[1]!);
    const action = isoOpMatch[2]!;
    const iso = isos.find((i) => i.id === id);
    if (iso) {
      if (action === 'deprecate') iso.state = 'deprecated';
      else if (action === 'enable' || action === 'retry' || action === 'reimport')
        iso.state = 'ready';
      else if (action === 'cancel') iso.state = 'failed';
      iso.rev += 1;
    }
    sendJSON(res, null);
    return true;
  }

  const isoSingleMatch = /^\/isos\/([^/]+)$/.exec(path);
  if (isoSingleMatch) {
    const id = decodeURIComponent(isoSingleMatch[1]!);
    const idx = isos.findIndex((i) => i.id === id);
    if (method === 'GET') {
      sendJSON(res, isos[idx] ?? isos[0]);
      return true;
    }
    if (method === 'DELETE') {
      if (idx !== -1) isos.splice(idx, 1);
      sendJSON(res, null);
      return true;
    }
  }

  if (path === '/isos' && method === 'GET') {
    let filtered = [...isos];
    const family = (query.get('family') || '').trim();
    const state = (query.get('state') || '').trim();
    if (family) filtered = filtered.filter((i) => i.family === family);
    if (state) filtered = filtered.filter((i) => i.state === state);
    sendJSON(res, paginate(filtered, query));
    return true;
  }

  if (path === '/isos' && method === 'POST') {
    const body = JSON.parse((await readBody(req)) || '{}');
    const created = {
      id: `iso-${body.series || 'custom'}-${nowSec()}`,
      name: body.name || '新登记系统镜像',
      family: body.family || 'linux',
      series: body.series || 'rocky',
      version: '9.4',
      arch: 'x86_64',
      state: 'ready',
      sha256: 'e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855',
      rev: 1,
      artifacts: {
        iso: `/var/lib/lober/isos/${body.name || 'custom'}.iso`,
      },
    };
    isos.unshift(created);
    sendJSON(res, created);
    return true;
  }

  // 6. Templates
  const tplSlotsMatch = /^\/templates\/([^/]+)\/versions\/(\d+)\/slots$/.exec(path);
  if (tplSlotsMatch && method === 'GET') {
    sendJSON(res, {
      slots: [
        { name: 'packages', exec: false, timing: '', line: 13, col: 1 },
        { name: 'post-target', exec: true, timing: 'post-target', line: 20, col: 1 },
        { name: 'firstboot', exec: true, timing: 'firstboot', line: 21, col: 1 },
      ],
      problems: [],
    });
    return true;
  }

  const tplDiffMatch = /^\/templates\/([^/]+)\/versions\/(\d+)\/source-diff$/.exec(path);
  if (tplDiffMatch && method === 'GET') {
    sendJSON(res, { original: null, current: null, changed: false });
    return true;
  }

  const tplCandidatesMatch = /^\/templates\/([^/]+)\/versions\/(\d+)\/hook-candidates$/.exec(path);
  if (tplCandidatesMatch && method === 'GET') {
    const prefix = (query.get('prefix') || '').trim().toLowerCase();
    const list = hooks
      .filter((h) => !prefix || h.name.toLowerCase().includes(prefix))
      .map((h) => ({
        hook: h,
        usable: h.state === 'approved',
        problems:
          h.state === 'approved'
            ? []
            : [{ code: 'not_approved', message: '该脚本组件尚未批准，批准后才能装配到生产模板' }],
      }));
    sendJSON(res, paginate(list, query));
    return true;
  }

  const tplPreviewParamsMatch = /^\/templates\/([^/]+)\/versions\/(\d+)\/preview-params$/.exec(path);
  if (tplPreviewParamsMatch && method === 'POST') {
    const id = decodeURIComponent(tplPreviewParamsMatch[1]!);
    const tpl = templates.find((t) => t.id === id) ?? templates[0]!;
    const fields = (tpl.params ?? []).map((p: any) => ({
      key: `tpl/${p.name}`,
      label: p.label || p.name,
      type: p.type,
      required: p.required,
      list: p.list,
      locked: tpl.bindings?.[p.name]?.editable === false,
      prefill: p.default || '',
      options: p.options ?? [],
    }));
    sendJSON(res, {
      fields,
      sample: ['host.sn', 'iso.url'],
      problems: [],
    });
    return true;
  }

  const tplPreviewMatch = /^\/templates\/([^/]+)\/versions\/(\d+)\/preview$/.exec(path);
  if (tplPreviewMatch && method === 'POST') {
    const id = decodeURIComponent(tplPreviewMatch[1]!);
    const body = JSON.parse((await readBody(req)) || '{}');
    const tpl = templates.find((t) => t.id === id) ?? templates[0]!;
    const hostSN = body.hostSN || 'SAMPLE-HOST-01';
    const disk = body.entered?.['tpl/target_disk'] || 'nvme0n1';
    sendJSON(res, {
      sample: body.hostSN ? [] : ['host.sn'],
      problems: [],
      files: [
        {
          kind: 'config',
          name: tpl.series?.includes('ubuntu') ? 'autoinstall.yaml' : 'kickstart.cfg',
          slot: '',
          digest: 'sha256:8f434346648f6b96df89dda901c5176b',
          sample: !body.hostSN,
          body: `# 渲染预览 — 主机: ${hostSN}\n# 模板: ${tpl.name} v${tpl.version}\n# 目标磁盘: /dev/${disk}\n\n${tpl.body
            .replaceAll('{{ .target_disk }}', disk)
            .replaceAll('{{ .hostname }}', body.entered?.['tpl/hostname'] || `${hostSN.toLowerCase()}.lober.local`)
            .replaceAll('{{ .root_pwhash }}', '$6$rounds=4096$loberprod$K9x8m7V1p2')}`,
        },
        {
          kind: 'hook',
          name: 'post-target-01-network.sh',
          slot: 'post-target',
          digest: 'sha256:2c26b46b68ffc68ff99b453c1d304134',
          sample: false,
          body: `#!/usr/bin/env bash\nset -euo pipefail\necho "Configuring static network for ${hostSN}..."\n`,
        },
      ],
    });
    return true;
  }

  const tplLifecycleMatch =
    /^\/templates\/([^/]+)\/versions\/(\d+)\/(validate|submit|approve|reject|deprecate)$/.exec(path);
  if (tplLifecycleMatch && method === 'POST') {
    const id = decodeURIComponent(tplLifecycleMatch[1]!);
    const action = tplLifecycleMatch[3]!;
    const tpl = templates.find((t) => t.id === id) ?? templates[0]!;
    if (action === 'validate') {
      sendJSON(res, { problems: [] });
      return true;
    }
    if (action === 'submit') tpl.state = 'review';
    else if (action === 'approve') {
      tpl.state = 'approved';
      tpl.approver = activeUsername;
    } else if (action === 'reject') tpl.state = 'draft';
    else if (action === 'deprecate') tpl.state = 'deprecated';
    tpl.rev += 1;
    sendJSON(res, tpl);
    return true;
  }

  const tplVersionsMatch = /^\/templates\/([^/]+)\/versions$/.exec(path);
  if (tplVersionsMatch) {
    const id = decodeURIComponent(tplVersionsMatch[1]!);
    const tpl = templates.find((t) => t.id === id) ?? templates[0]!;
    if (method === 'GET') {
      sendJSON(res, { items: [tpl] });
      return true;
    }
    if (method === 'POST') {
      tpl.version += 1;
      tpl.state = 'draft';
      tpl.rev += 1;
      sendJSON(res, tpl);
      return true;
    }
  }

  const tplDeriveMatch = /^\/templates\/([^/]+)\/derive$/.exec(path);
  if (tplDeriveMatch && method === 'POST') {
    const id = decodeURIComponent(tplDeriveMatch[1]!);
    const body = JSON.parse((await readBody(req)) || '{}');
    const source = templates.find((t) => t.id === id) ?? templates[0]!;
    const derived = {
      ...structuredClone(source),
      id: `tpl-derived-${nowSec()}`,
      name: body.name || `${source.name}-副本`,
      version: 1,
      rev: 1,
      builtin: false,
      state: 'draft',
      author: activeUsername,
      approver: '',
      derivedFrom: { id: source.id, name: source.name, version: source.version },
    };
    templates.unshift(derived);
    sendJSON(res, derived);
    return true;
  }

  const tplVersionMatch = /^\/templates\/([^/]+)\/versions\/(\d+)$/.exec(path);
  if (tplVersionMatch) {
    const id = decodeURIComponent(tplVersionMatch[1]!);
    const idx = templates.findIndex((t) => t.id === id);
    if (method === 'GET') {
      sendJSON(res, templates[idx] ?? templates[0]);
      return true;
    }
    if (method === 'PUT') {
      const body = JSON.parse((await readBody(req)) || '{}');
      if (idx !== -1) {
        templates[idx] = {
          ...templates[idx],
          ...body,
          rev: templates[idx]!.rev + 1,
        };
        sendJSON(res, templates[idx]);
      } else {
        sendError(res, 404, '模板不存在');
      }
      return true;
    }
    if (method === 'DELETE') {
      if (idx !== -1) templates.splice(idx, 1);
      sendJSON(res, null);
      return true;
    }
  }

  if (path === '/templates' && method === 'GET') {
    let filtered = [...templates];
    const prefix = (query.get('prefix') || '').trim().toLowerCase();
    const state = (query.get('state') || '').trim();
    if (prefix) filtered = filtered.filter((t) => t.name.toLowerCase().includes(prefix));
    if (state) filtered = filtered.filter((t) => t.state === state);
    sendJSON(res, paginate(filtered, query));
    return true;
  }

  if (path === '/templates' && method === 'POST') {
    const body = JSON.parse((await readBody(req)) || '{}');
    const created = {
      id: `tpl-${nowSec()}`,
      version: 1,
      rev: 1,
      name: body.name || '新建装机模板',
      family: body.family || 'linux',
      series: body.series || ['rocky'],
      body: body.body || '',
      params: body.params || [],
      bindings: body.bindings || {},
      assembly: body.assembly || {},
      author: activeUsername,
      approver: '',
      builtin: false,
      state: 'draft',
      derivedFrom: null,
      problems: [],
    };
    templates.unshift(created);
    sendJSON(res, created);
    return true;
  }

  // 7. Hooks
  const hookLifecycleMatch =
    /^\/hooks\/([^/]+)\/versions\/(\d+)\/(validate|submit|approve|reject|deprecate)$/.exec(path);
  if (hookLifecycleMatch && method === 'POST') {
    const id = decodeURIComponent(hookLifecycleMatch[1]!);
    const action = hookLifecycleMatch[3]!;
    const hook = hooks.find((h) => h.id === id) ?? hooks[0]!;
    if (action === 'validate') {
      sendJSON(res, { problems: [] });
      return true;
    }
    if (action === 'submit') hook.state = 'review';
    else if (action === 'approve') {
      hook.state = 'approved';
      hook.approver = activeUsername;
    } else if (action === 'reject') hook.state = 'draft';
    else if (action === 'deprecate') hook.state = 'deprecated';
    hook.rev += 1;
    sendJSON(res, hook);
    return true;
  }

  const hookVersionsMatch = /^\/hooks\/([^/]+)\/versions$/.exec(path);
  if (hookVersionsMatch) {
    const id = decodeURIComponent(hookVersionsMatch[1]!);
    const hook = hooks.find((h) => h.id === id) ?? hooks[0]!;
    if (method === 'GET') {
      sendJSON(res, { items: [hook] });
      return true;
    }
    if (method === 'POST') {
      hook.version += 1;
      hook.state = 'draft';
      hook.rev += 1;
      sendJSON(res, hook);
      return true;
    }
  }

  const hookVersionMatch = /^\/hooks\/([^/]+)\/versions\/(\d+)$/.exec(path);
  if (hookVersionMatch) {
    const id = decodeURIComponent(hookVersionMatch[1]!);
    const idx = hooks.findIndex((h) => h.id === id);
    if (method === 'GET') {
      sendJSON(res, hooks[idx] ?? hooks[0]);
      return true;
    }
    if (method === 'PUT') {
      const body = JSON.parse((await readBody(req)) || '{}');
      if (idx !== -1) {
        hooks[idx] = {
          ...hooks[idx],
          ...body,
          rev: hooks[idx]!.rev + 1,
        };
        sendJSON(res, hooks[idx]);
      } else {
        sendError(res, 404, '脚本不存在');
      }
      return true;
    }
    if (method === 'DELETE') {
      if (idx !== -1) hooks.splice(idx, 1);
      sendJSON(res, null);
      return true;
    }
  }

  if (path === '/hooks' && method === 'GET') {
    let filtered = [...hooks];
    const prefix = (query.get('prefix') || '').trim().toLowerCase();
    const state = (query.get('state') || '').trim();
    if (prefix) filtered = filtered.filter((h) => h.name.toLowerCase().includes(prefix));
    if (state) filtered = filtered.filter((h) => h.state === state);
    sendJSON(res, paginate(filtered, query));
    return true;
  }

  if (path === '/hooks' && method === 'POST') {
    const body = JSON.parse((await readBody(req)) || '{}');
    const created = {
      id: `hook-${nowSec()}`,
      version: 1,
      rev: 1,
      name: body.name || '新建脚本组件',
      description: body.description || '',
      author: activeUsername,
      approver: '',
      builtin: false,
      state: 'draft',
      timeoutSec: body.timeoutSec ?? 300,
      required: body.required ?? true,
      params: body.params || [],
      impls: body.impls || [],
      problems: [],
    };
    hooks.unshift(created);
    sendJSON(res, created);
    return true;
  }

  // 8. Audit
  if (path === '/audit/export' && method === 'GET') {
    const csvLines = [
      '编号,时间,操作人,动作,对象类型,对象标识,详情',
      ...audits.map((a) =>
        [
          a.id,
          new Date(a.at * 1000).toISOString(),
          a.actor,
          a.action,
          a.targetKind,
          a.targetID,
          `"${String(a.detail).replaceAll('"', '""')}"`,
        ].join(','),
      ),
    ];
    res.statusCode = 200;
    res.setHeader('Content-Type', 'text/csv; charset=utf-8');
    res.end('\uFEFF' + csvLines.join('\n'));
    return true;
  }

  if (path === '/audit' && method === 'GET') {
    let filtered = [...audits];
    const kind = (query.get('target_kind') || '').trim();
    const targetId = (query.get('target_id') || '').trim().toLowerCase();
    const actor = (query.get('actor') || '').trim().toLowerCase();
    const action = (query.get('action') || '').trim().toLowerCase();
    const from = Number(query.get('from') || 0);
    const to = Number(query.get('to') || 0);
    if (kind) filtered = filtered.filter((a) => a.targetKind === kind);
    if (targetId) filtered = filtered.filter((a) => a.targetID.toLowerCase().includes(targetId));
    if (actor) filtered = filtered.filter((a) => a.actor.toLowerCase().includes(actor));
    if (action) filtered = filtered.filter((a) => a.action.toLowerCase().includes(action));
    if (from > 0) filtered = filtered.filter((a) => a.at >= from);
    if (to > 0) filtered = filtered.filter((a) => a.at <= to);
    sendJSON(res, paginate(filtered, query));
    return true;
  }

  // 9. Users & Role Menus
  if (path === '/user/profile' && method === 'PUT') {
    const body = JSON.parse((await readBody(req)) || '{}');
    const user = users.find((u) => u.username === activeUsername) ?? users[0]!;
    user.displayName = body.displayName ?? user.displayName;
    user.rev += 1;
    sendJSON(res, {
      ...user,
      permissions: rolePermissions[user.role] ?? [],
    });
    return true;
  }

  const userSingleMatch = /^\/users\/([^/]+)$/.exec(path);
  if (userSingleMatch && (method === 'PUT' || method === 'DELETE')) {
    const username = decodeURIComponent(userSingleMatch[1]!);
    const user = users.find((u) => u.username === username);
    if (!user) {
      sendError(res, 404, '未找到该账号');
      return true;
    }
    if (method === 'DELETE') {
      user.disabled = true;
      user.rev += 1;
      sendJSON(res, null);
      return true;
    }
    const body = JSON.parse((await readBody(req)) || '{}');
    user.displayName = body.displayName ?? user.displayName;
    user.role = body.role ?? user.role;
    user.disabled = body.disabled ?? user.disabled;
    user.rev += 1;
    sendJSON(res, {
      ...user,
      permissions: rolePermissions[user.role] ?? [],
    });
    return true;
  }

  if (path === '/users' && method === 'GET') {
    const enriched = users.map((u) => ({
      ...u,
      permissions: rolePermissions[u.role] ?? [],
    }));
    sendJSON(res, paginate(enriched, query));
    return true;
  }

  if (path === '/users' && method === 'POST') {
    const body = JSON.parse((await readBody(req)) || '{}');
    const created = {
      username: body.username,
      displayName: body.displayName || body.username,
      role: body.role || 'viewer',
      disabled: Boolean(body.disabled),
      passwordChangeRequired: false,
      createdAt: nowSec(),
      rev: 1,
    };
    users.push(created);
    sendJSON(res, {
      ...created,
      permissions: rolePermissions[created.role] ?? [],
    });
    return true;
  }

  if (path === '/roles/menus' && method === 'GET') {
    const roles = Object.keys(rolePermissions).map((role) => {
      const perms = new Set(rolePermissions[role] ?? []);
      const availableMenuIds = menuCatalog
        .filter((m) => m.parentId && (!m.permission || perms.has(m.permission)))
        .map((m) => m.id);
      const state = roleMenuState[role] ?? { menuIds: availableMenuIds, rev: 1 };
      return {
        role,
        availableMenuIds,
        menuIds: state.menuIds,
        rev: state.rev,
      };
    });
    sendJSON(res, { menus: menuCatalog, roles });
    return true;
  }

  const roleMenuSaveMatch = /^\/roles\/([^/]+)\/menus$/.exec(path);
  if (roleMenuSaveMatch && method === 'PUT') {
    const role = decodeURIComponent(roleMenuSaveMatch[1]!);
    const body = JSON.parse((await readBody(req)) || '{}');
    const perms = new Set(rolePermissions[role] ?? []);
    const availableMenuIds = menuCatalog
      .filter((m) => m.parentId && (!m.permission || perms.has(m.permission)))
      .map((m) => m.id);
    const current = (roleMenuState[role] ??= { menuIds: availableMenuIds, rev: 1 });
    current.menuIds = body.menuIds ?? [];
    current.rev += 1;
    sendJSON(res, {
      role,
      availableMenuIds,
      menuIds: current.menuIds,
      rev: current.rev,
    });
    return true;
  }

  return false;
}

export function viteConsoleMockPlugin(enabled = true) {
  return {
    name: 'lober-console-mock',
    configureServer(server: { middlewares: { use: (fn: any) => void } }) {
      if (!enabled) return;
      server.middlewares.use(
        async (req: IncomingMessage, res: ServerResponse, next: () => void) => {
          try {
            const handled = await handleConsoleMockRequest(req, res);
            if (!handled) next();
          } catch (error) {
            sendError(
              res,
              500,
              error instanceof Error ? error.message : 'Mock server error',
            );
          }
        },
      );
    },
    configurePreviewServer(server: { middlewares: { use: (fn: any) => void } }) {
      if (!enabled) return;
      server.middlewares.use(
        async (req: IncomingMessage, res: ServerResponse, next: () => void) => {
          try {
            const handled = await handleConsoleMockRequest(req, res);
            if (!handled) next();
          } catch (error) {
            sendError(
              res,
              500,
              error instanceof Error ? error.message : 'Mock server error',
            );
          }
        },
      );
    },
  };
}
