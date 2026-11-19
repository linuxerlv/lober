import type { DiscoveredHost } from '#/api/console/discovery';
import type { Schemas } from '#/api/console/generated';
import type { HostListItem } from '#/api/console/host';

import { diskRows } from '#/views/hosts/hardware-details';

export interface HostDiskOption {
  bytes: string;
  capacity: string;
  device: string;
  label: string;
  model: string;
  serial: string;
}

export interface HostDiskReport {
  disks: HostDiskOption[];
  notice: string;
  status: 'none' | 'partial' | 'ready';
}

function isInstallableDeviceName(raw: string): boolean {
  const name = raw.trim().replace(/^\/dev\//, '');
  if (!/^[a-z][a-z0-9_-]{0,31}$/.test(name)) return false;
  if (
    /^(loop|ram|zram|sr|scd|cdrom|fd|dm-|md|nbd|zd)\d*$/.test(name) ||
    name.startsWith('loop') ||
    name.startsWith('ram') ||
    name.startsWith('sr') ||
    name.startsWith('dm-')
  ) {
    return false;
  }
  if (/^(nvme\d+n\d+|mmcblk\d+|nbd\d+)p\d+$/.test(name)) return false;
  if (/^(sd|vd|hd|xvd)[a-z]+\d+$/.test(name)) return false;
  return true;
}

function formatBytesText(raw?: string): undefined | { bytes: string; capacity: string } {
  const clean = (raw ?? '').trim();
  if (!/^[1-9]\d{0,18}$/.test(clean)) return undefined;
  const parsed = diskRows(`disk:${clean}:`)[0];
  if (!parsed) return undefined;
  return { bytes: parsed.bytes, capacity: parsed.capacity };
}

function buildDiskLabel(device: string, capacity: string, serial: string, model: string): string {
  const parts = [device];
  if (capacity) parts.push(capacity);
  if (serial) parts.push(`序列号 ${serial}`);
  else if (model) parts.push(model);
  return parts.join(' · ');
}

export function extractHostDiskReport(
  host?: Pick<HostListItem, 'attrs' | 'sn'> | Pick<Schemas['WizardHostItem'], 'attrs' | 'sn'>,
  discovery?: DiscoveredHost,
): HostDiskReport {
  const invAt = Number(host?.attrs?.inv_collected_at) || 0;
  const invDisks = new Map<string, HostDiskOption>();

  for (const row of diskRows(host?.attrs?.inv_disks)) {
    const device = row.device.trim().replace(/^\/dev\//, '');
    if (!isInstallableDeviceName(device) || !/^[1-9]\d{0,18}$/.test(row.bytes)) {
      continue;
    }
    invDisks.set(device, {
      device,
      bytes: row.bytes,
      capacity: row.capacity,
      model: row.model.trim(),
      serial: '',
      label: buildDiskLabel(device, row.capacity, '', row.model.trim()),
    });
  }

  const validDiscovery =
    discovery &&
    host?.sn &&
    discovery.registered_sn === host.sn &&
    Array.isArray(discovery.report?.sections)
      ? discovery
      : undefined;

  const discAt = validDiscovery?.report_at ?? 0;
  const storageSection = validDiscovery?.report?.sections?.find(
    (section) => section.key === 'storage',
  );
  let partialNotice = '';
  let missingSerial = false;
  const discDisks = new Map<string, HostDiskOption>();

  if (storageSection) {
    if (storageSection.status === 'partial' || storageSection.status === 'unread') {
      partialNotice =
        storageSection.reason?.trim() ||
        '硬件报告中的磁盘信息未完整读取，可从已识别磁盘中选择或手工输入。';
    }
    for (const item of storageSection.items ?? []) {
      const device = (item.name ?? '').trim().replace(/^\/dev\//, '');
      const kind = (item.fields?.['类型'] ?? '').trim();
      if (kind && kind !== '磁盘' && kind !== '系统可见磁盘') continue;
      if (!isInstallableDeviceName(device)) continue;

      const cap = formatBytesText(item.fields?.['容量（字节）']);
      const fallbackCap = (item.fields?.['容量'] ?? '').trim();
      const bytes = cap?.bytes ?? '';
      const capacity = cap?.capacity ?? fallbackCap;
      if (!bytes && !capacity) continue;
      if (bytes && !/^[1-9]\d{0,18}$/.test(bytes)) continue;

      const model = (item.fields?.['型号'] ?? '').trim();
      const serial = (item.fields?.['序列号'] ?? '').trim();
      if (!serial) missingSerial = true;

      discDisks.set(device, {
        device,
        bytes,
        capacity,
        model,
        serial,
        label: buildDiskLabel(device, capacity, serial, model),
      });
    }
  }

  let disks: HostDiskOption[];
  if (invDisks.size > 0 && discDisks.size > 0) {
    if (invAt > discAt) {
      // 已登记采集比发现报告更新：只采用较新的同一来源磁盘清单，不合并旧盘或旧序列号
      disks = [...invDisks.values()];
      partialNotice = '';
      missingSerial = true;
    } else if (discAt > invAt) {
      // 发现报告比已登记采集更新：只采用较新的发现报告磁盘清单
      disks = [...discDisks.values()];
    } else {
      // 同一时间或未记录时间戳：以发现报告为主，补充采集信息
      const merged = new Map(invDisks);
      for (const [device, disk] of discDisks) {
        const existing = merged.get(device);
        const bytes = disk.bytes || existing?.bytes || '';
        const capacity = disk.capacity || existing?.capacity || '';
        const model = disk.model || existing?.model || '';
        const serial = disk.serial || existing?.serial || '';
        merged.set(device, {
          device,
          bytes,
          capacity,
          model,
          serial,
          label: buildDiskLabel(device, capacity, serial, model),
        });
      }
      disks = [...merged.values()];
    }
  } else if (discDisks.size > 0) {
    disks = [...discDisks.values()];
  } else {
    disks = [...invDisks.values()];
  }

  if (disks.length > 0 && disks.some((d) => !d.serial)) {
    missingSerial = true;
  }

  if (disks.length === 0) {
    return {
      status: 'none',
      disks: [],
      notice:
        partialNotice ||
        '暂无可用硬件报告或可安装磁盘清单，可保留手工输入目标盘（也可以先执行硬件采集或自动发现）。',
    };
  }

  if (partialNotice || missingSerial) {
    return {
      status: 'partial',
      disks,
      notice:
        partialNotice ||
        '部分磁盘字段（如序列号）未完整读取，已列出可安装磁盘供选择，也可手工填写目标盘。',
    };
  }

  return {
    status: 'ready',
    disks,
    notice: '',
  };
}

export function isTargetDiskField(
  field: Pick<Schemas['ParamFieldDTO'], 'key' | 'label' | 'type'> & {
    list?: boolean;
  },
): boolean {
  if (field.list) return false;
  if (field.type !== 'word' && field.type !== 'diskpath') return false;
  const key = String(field.key).toLowerCase();
  const name = key.split('/').at(-1) ?? '';
  if (
    name === 'disk' ||
    name === 'system_disk' ||
    name === 'target_disk' ||
    name === 'install_disk' ||
    name === 'boot_disk' ||
    name === 'root_disk' ||
    name === 'os_disk'
  ) {
    return true;
  }
  const label = (field.label ?? '').trim();
  return /系统盘|目标盘|安装盘|目标磁盘|安装磁盘|系统磁盘|启动盘/.test(label);
}

export function formatTargetDiskValue(
  field: Pick<Schemas['ParamFieldDTO'], 'prefill' | 'type'>,
  device: string,
): string {
  const clean = device.trim().replace(/^\/dev\//, '');
  if (!clean) return '';
  if (field.type === 'diskpath' || field.prefill?.trim().startsWith('/dev/')) {
    return `/dev/${clean}`;
  }
  return clean;
}

export function normalizeDiskDevice(raw?: string): string {
  return (raw ?? '').trim().replace(/^\/dev\//, '');
}

export function isPersistentDiskPath(
  field: Pick<Schemas['ParamFieldDTO'], 'type'> & { prefill?: string },
  value?: string,
): boolean {
  if (field.type !== 'diskpath' && !field.prefill?.startsWith('/dev/')) return false;
  return /^\/dev\/(?:disk\/by-(?:id|path)|mapper)\/[^/]+$/.test((value ?? '').trim());
}

export function matchesTargetDiskOption(
  field: Pick<Schemas['ParamFieldDTO'], 'key' | 'label' | 'type'> & {
    list?: boolean;
    required?: boolean;
    prefill?: string;
  },
  value: string | undefined,
  disks: HostDiskOption[],
): boolean {
  if (!isTargetDiskField(field) || !disks.length) return true;
  const normalized = normalizeDiskDevice(value);
  if (!normalized) return field.required === false;
  // 报告只记录内核设备名。持久路径由安装环境解析，具体格式交给服务端校验。
  if (isPersistentDiskPath(field, value)) return true;
  return disks.some((disk) => disk.device === normalized);
}

export function findTargetDiskOption(
  value: string | undefined,
  disks: HostDiskOption[],
): HostDiskOption | undefined {
  const normalized = normalizeDiskDevice(value);
  if (!normalized) return undefined;
  return disks.find((disk) => disk.device === normalized);
}

export function mergeHostSelection<T extends { sn: string }>(
  selected: T[],
  visible: T[],
  checked: T[],
): T[] {
  const visibleSNs = new Set(visible.map((host) => host.sn));
  return [
    ...new Map(
      [
        ...selected.filter((host) => !visibleSNs.has(host.sn)),
        ...checked.filter((host) => visibleSNs.has(host.sn)),
      ].map((host) => [host.sn, host]),
    ).values(),
  ];
}

export function collectEditable(
  forms: Pick<Schemas['WizardParamsHost'], 'fields' | 'sn'>[],
  values: Record<string, Record<string, string>>,
): Record<string, Record<string, string>> {
  return Object.fromEntries(
    forms.map((host) => [
      host.sn,
      Object.fromEntries(
        (host.fields ?? [])
          .filter((field) => !field.locked)
          .map((field) => [
            field.key,
            values[host.sn]?.[field.key] ?? field.prefill,
          ]),
      ),
    ]),
  );
}

interface Selection {
  entered: Record<string, Record<string, string>>;
  isoID: string;
  templateID: string;
  templateVersion: number;
}

export function selectionChanged(
  state: Selection,
  kind: 'hosts' | 'iso',
  value = '',
): Selection {
  return {
    ...state,
    isoID: kind === 'iso' ? value : '',
    templateID: '',
    templateVersion: 0,
    entered: {},
  };
}

export function latestOnly() {
  let revision = 0;
  return {
    invalidate() {
      revision += 1;
    },
    async run<T>(
      request: Promise<T>,
      apply: (result: T) => void,
      fail?: (error: unknown) => void,
    ) {
      const ticket = ++revision;
      try {
        const result = await request;
        if (ticket === revision) apply(result);
      } catch (error) {
        if (ticket === revision) {
          if (fail) fail(error);
          else throw error;
        }
      }
    },
  };
}

export function mergeEvents<T extends Pick<Schemas['EventItem'], 'seq'>>(
  current: T[],
  incoming: T[],
): T[] {
  const events = new Map(current.map((event) => [event.seq, event]));
  for (const event of incoming) events.set(event.seq, event);
  return [...events.values()].sort((a, b) => a.seq - b.seq);
}
