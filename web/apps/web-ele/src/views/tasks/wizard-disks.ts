import type { Schemas } from '#/api/console/generated';

import { computed, ref } from 'vue';

import { errorMessage } from '#/api/console/problem';
import { formatCapacity } from '#/views/hosts/hardware-details';

type Form = Schemas['WizardParamsHost'];
type Disk = Schemas['DiscoveryHostDisk'];
type Report = Schemas['DiscoveryHostDiskReport'];

export function useHostDiskReports(read: (sn: string) => Promise<Report>) {
  const reports = ref<Record<string, Report>>({});
  const errors = ref<Record<string, string>>({});
  const loading = ref(false);
  const blocked = computed(
    () => loading.value || Object.keys(errors.value).length > 0,
  );
  let revision = 0;

  function reset() {
    revision++;
    reports.value = {};
    errors.value = {};
    loading.value = false;
  }

  async function readHosts(sns: string[]) {
    const ticket = ++revision;
    loading.value = sns.length > 0;
    errors.value = {};
    await Promise.all(
      sns.map(async (sn) => {
        try {
          const report = await read(sn);
          if (ticket === revision) reports.value[sn] = report;
        } catch (cause) {
          if (ticket === revision) errors.value[sn] = errorMessage(cause);
        }
      }),
    );
    if (ticket === revision) loading.value = false;
  }

  function load(forms: Form[], selected: { sn: string }[]) {
    reset();
    const needed = new Set(
      forms.filter((host) => deviceField(host)).map((host) => host.sn),
    );
    return readHosts(
      selected.map(({ sn }) => sn).filter((sn) => needed.has(sn)),
    );
  }

  function retry() {
    if (!loading.value) return readHosts(Object.keys(errors.value));
  }

  return { reports, errors, loading, blocked, reset, load, retry };
}

function isDeviceMode(mode?: string) {
  return (
    mode === 'device' || mode === 'device-or-path'
  );
}

export function deviceField(host: Form) {
  const target = host.targetDisk;
  if (target && isDeviceMode(target.mode)) {
    return host.fields?.find((field) => field.key === target.key);
  }
}

export function isPersistentDiskPath(mode: string | undefined, value: string) {
  if (!isDeviceMode(mode)) return false;
  // 部分模板自行补 /dev/，其输入中的持久路径也是相对的。
  if (mode === 'device' && value.startsWith('/dev/')) return false;
  return /^(?:\/dev\/)?(?:disk\/by-(?:id|path)|mapper)\/[^/]+$/.test(value);
}

export function diskInputProblem(host: Form, raw: string, disks?: Disk[] | null) {
  const field = deviceField(host);
  if (!field) return '';
  const value = raw.trim();
  if (!value) return field.required ? '请填写目标盘。' : '';
  const mode = host.targetDisk!.mode;
  if (mode === 'device' && value.startsWith('/')) {
    return '此模板会补上 /dev/。请输入设备名（如 vdb）或相对路径（如 disk/by-id/磁盘标识）。';
  }
  if (value.startsWith('/') && !value.startsWith('/dev/')) {
    return '请输入 /dev/ 下的设备路径。';
  }
  const device = value.replace(/^\/dev\//, '');
  if (device.split('/').some((part) => !part || part === '.' || part === '..')) {
    return '目标盘路径含有无效的目录，请检查设备名或路径。';
  }
  if (isPersistentDiskPath(mode, value) || !disks?.length || disks.some((disk) => disk.device === device)) return '';
  return `默认目标盘或当前值（${value}）与磁盘清单不匹配，请选择可用磁盘或核对手工值。`;
}

export function matchesTargetDisk(host: Form, value: string, disks?: Disk[] | null) {
  return !diskInputProblem(host, value, disks);
}

export function diskNotice(report?: Report) {
  if (report?.notice) return report.notice;
  return report?.status === 'none' ? '暂无磁盘报告，默认目标盘未经硬件核对。可按模板手工填写目标盘。' : '';
}

function diskDetails(disk: Disk) {
  const capacity = disk.bytes
    ? formatCapacity(BigInt(disk.bytes))
    : disk.capacityText;
  return [capacity, disk.serial ? '序列号 ' + disk.serial : disk.model]
    .filter(Boolean)
    .join(' · ');
}

export function diskLabel(disk: Disk) {
  return [disk.device, diskDetails(disk)].filter(Boolean).join(' · ');
}

export function targetDiskSummary(
  host: Form | undefined,
  disks?: Disk[] | null,
) {
  const target = host?.targetDisk;
  if (!target || !host?.fields?.some((field) => field.key === target.key))
    return '目标盘由自定义模板决定';
  const { mode, value } = target;
  if (mode === 'windows-index') return '目标盘：Windows 磁盘 ' + value;
  if (mode === 'first-disk-rule') return '选盘规则：' + value;
  if (!isDeviceMode(mode)) return '目标盘由自定义模板决定';
  if (isPersistentDiskPath(mode, value))
    return `目标盘：${value}（安装时解析此路径）`;
  const disk = disks?.find(
    (item) => item.device === value.replace(/^\/dev\//, ''),
  );
  const details = disk && diskDetails(disk);
  return details ? `目标盘：${value}（${details}）` : `目标盘：${value}`;
}
