import { describe, expect, it, vi } from 'vitest';

import {
  collectEditable,
  extractHostDiskReport,
  formatTargetDiskValue,
  isTargetDiskField,
  latestOnly,
  matchesTargetDiskOption,
  mergeEvents,
  mergeHostSelection,
  selectionChanged,
} from './wizard-state';

describe('wizard preserves server authority', () => {
  it('keeps off-page hosts and applies checked and unchecked rows only to the visible page', () => {
    const first = { sn: 'first' };
    const second = { sn: 'second' };
    const third = { sn: 'third' };
    expect(
      mergeHostSelection([first, second], [second, third], [third]),
    ).toEqual([first, third]);
    expect(mergeHostSelection([first, third], [first], [first, first])).toEqual(
      [third, first],
    );
  });
  it('submits only unlocked fields returned by the server for selected hosts', () => {
    const forms = [
      {
        sn: 'SN-1',
        fields: [
          { key: 'tpl/password', locked: false, prefill: '' },
          { key: 'partition/1/disk', locked: true, prefill: '/dev/sda' },
        ],
      },
    ];
    expect(
      collectEditable(forms as never, {
        'SN-1': {
          'tpl/password': 'a b',
          'partition/1/disk': '/dev/sdb',
          fake: 'x',
        },
        'SN-2': { 'tpl/password': 'injected' },
      }),
    ).toEqual({ 'SN-1': { 'tpl/password': 'a b' } });
  });

  it('clears all downstream selection and values when ISO changes', () => {
    expect(
      selectionChanged(
        {
          isoID: 'old',
          templateID: 'tpl',
          templateVersion: 2,
          entered: { host: { x: '1' } },
        },
        'iso',
        'new',
      ),
    ).toEqual({
      isoID: 'new',
      templateID: '',
      templateVersion: 0,
      entered: {},
    });
  });

  it('ignores stale responses even if the older request finishes last', async () => {
    const gate = latestOnly();
    const apply = vi.fn();
    let finishOld!: (value: string) => void;
    const old = gate.run(
      new Promise<string>((resolve) => {
        finishOld = resolve;
      }),
      apply,
    );
    await gate.run(Promise.resolve('new'), apply);
    finishOld('old');
    await old;
    expect(apply.mock.calls).toEqual([['new']]);
  });

  it('invalidates pending work when a selection changes or component leaves', async () => {
    const gate = latestOnly();
    const apply = vi.fn();
    let finish!: (value: string) => void;
    const work = gate.run(
      new Promise<string>((resolve) => {
        finish = resolve;
      }),
      apply,
    );
    gate.invalidate();
    finish('discard');
    await work;
    expect(apply).not.toHaveBeenCalled();
  });

  it('merges replayed committed events by server sequence in order', () => {
    const old = [
      { seq: 1, kind: 'first' },
      { seq: 2, kind: 'second' },
    ];
    const replay = [
      { seq: 2, kind: 'second' },
      { seq: 3, kind: 'last' },
    ];
    expect(
      mergeEvents(old as never, replay as never).map((event) => event.seq),
    ).toEqual([1, 2, 3]);
  });

  it('extracts installable disks with name, capacity and serial while excluding partitions, optical drives and invalid disks', () => {
    const report = extractHostDiskReport(
      {
        sn: 'HOST-1',
        attrs: {
          inv_disks:
            'sda:480000000000:Intel SSD\nsda1:1000000000:Part\nsr0:4000000000:DVD-ROM\nloop0:1000000:loop\nnvme0n1:960000000000:Samsung NVMe\nbad:0:Zero',
        },
      },
      {
        mac: '52:54:00:12:34:56',
        registered_sn: 'HOST-1',
        status: 'received',
        report: {
          sections: [
            {
              key: 'storage',
              title: '磁盘和阵列',
              status: 'read',
              items: [
                {
                  name: '存储控制器 0000:00:1f.2',
                  fields: { 设备: 'AHCI' },
                },
                {
                  name: 'sda',
                  fields: {
                    类型: '磁盘',
                    '容量（字节）': '480000000000',
                    型号: 'Intel SSD',
                    序列号: 'SN-INTEL-01',
                  },
                },
                {
                  name: 'sda1',
                  fields: {
                    类型: '分区',
                    '容量（字节）': '1000000000',
                  },
                },
                {
                  name: 'sr0',
                  fields: {
                    类型: '光驱',
                    '容量（字节）': '4000000000',
                  },
                },
                {
                  name: 'nvme0n1',
                  fields: {
                    类型: '系统可见磁盘',
                    '容量（字节）': '960000000000',
                    型号: 'Samsung NVMe',
                    序列号: 'SN-NVME-99',
                  },
                },
                {
                  name: 'nvme0n1p1',
                  fields: {
                    类型: '磁盘',
                    '容量（字节）': '512000000',
                  },
                },
              ],
            },
          ],
        },
      } as never,
    );

    expect(report.status).toBe('ready');
    expect(report.disks).toEqual([
      {
        device: 'sda',
        bytes: '480000000000',
        capacity: '480 GB',
        model: 'Intel SSD',
        serial: 'SN-INTEL-01',
        label: 'sda · 480 GB · 序列号 SN-INTEL-01',
      },
      {
        device: 'nvme0n1',
        bytes: '960000000000',
        capacity: '960 GB',
        model: 'Samsung NVMe',
        serial: 'SN-NVME-99',
        label: 'nvme0n1 · 960 GB · 序列号 SN-NVME-99',
      },
    ]);
  });

  it('flags default disk mismatch without silently selecting another disk, and supports manual fallback on missing or partial reports', () => {
    const multiDiskReport = extractHostDiskReport({
      sn: 'HOST-2',
      attrs: {
        inv_disks: 'nvme0n1:480000000000:NVMe A\nnvme1n1:960000000000:NVMe B',
      },
    });
    const diskField = {
      key: 'partition/1/disk',
      label: '系统盘',
      type: 'word',
      locked: false,
      prefill: 'sda',
    };
    expect(isTargetDiskField(diskField)).toBe(true);
    expect(
      matchesTargetDiskOption(diskField, 'sda', multiDiskReport.disks),
    ).toBe(false);
    expect(
      matchesTargetDiskOption(diskField, 'nvme0n1', multiDiskReport.disks),
    ).toBe(true);
    expect(
      formatTargetDiskValue(
        { ...diskField, type: 'diskpath' },
        'nvme0n1',
      ),
    ).toBe('/dev/nvme0n1');

    const noReport = extractHostDiskReport({ sn: 'HOST-OLD', attrs: {} });
    expect(noReport.status).toBe('none');
    expect(noReport.disks).toEqual([]);
    expect(noReport.notice).toContain('手工');

    const partialReport = extractHostDiskReport(
      { sn: 'HOST-PARTIAL', attrs: {} },
      {
        mac: '52:54:00:12:34:99',
        registered_sn: 'HOST-PARTIAL',
        status: 'received',
        report: {
          sections: [
            {
              key: 'storage',
              title: '磁盘和阵列',
              status: 'partial',
              reason: '部分磁盘序列号未读取',
              items: [
                {
                  name: 'vda',
                  fields: {
                    类型: '磁盘',
                    '容量（字节）': '64000000000',
                  },
                },
              ],
            },
          ],
        },
      } as never,
    );
    expect(partialReport.status).toBe('partial');
    expect(partialReport.disks).toHaveLength(1);
    expect(partialReport.disks[0]?.device).toBe('vda');
    expect(partialReport.notice).toContain('部分磁盘序列号未读取');
  });

  it('preserves historical template semantics for scalar target disk fields and ignores list or unrelated diskpath fields', () => {
    // Scalar install disk fields are recognized
    expect(
      isTargetDiskField({
        key: 'partition/1/disk',
        label: '系统盘',
        type: 'word',
        list: false,
      }),
    ).toBe(true);
    expect(
      isTargetDiskField({
        key: 'partition/1/root',
        label: '安装目标盘',
        type: 'diskpath',
        list: false,
      }),
    ).toBe(true);

    // List parameters must not get scalar disk picker or be treated as target disk
    expect(
      isTargetDiskField({
        key: 'storage/1/disk',
        label: '系统盘列表',
        type: 'word',
        list: true,
      }),
    ).toBe(false);
    expect(
      isTargetDiskField({
        key: 'storage/1/disks',
        label: '数据盘',
        type: 'diskpath',
        list: true,
      }),
    ).toBe(false);

    // Unrelated diskpath fields (e.g. data disk, journal disk, log disk) must not be misidentified as target install disk
    expect(
      isTargetDiskField({
        key: 'ceph/1/journal_dev',
        label: '日志设备路径',
        type: 'diskpath',
        list: false,
      }),
    ).toBe(false);
    expect(
      isTargetDiskField({
        key: 'data/1/data_disk',
        label: '数据盘路径',
        type: 'diskpath',
        list: false,
      }),
    ).toBe(false);

    // formatTargetDiskValue only uses field.type === 'diskpath' or template prefill starting with /dev/, never user-typed currentValue
    expect(
      formatTargetDiskValue(
        { type: 'word', prefill: 'sda' },
        '/dev/vda',
      ),
    ).toBe('vda');
    expect(
      formatTargetDiskValue(
        { type: 'word', prefill: '/dev/sda' },
        '/dev/vda',
      ),
    ).toBe('/dev/vda');
    expect(
      formatTargetDiskValue(
        { type: 'diskpath', prefill: '' },
        'vda',
      ),
    ).toBe('/dev/vda');
  });

  it('prioritizes newer inventory over older discovery report and ignores unverified discovery records', () => {
    // Newer inv_collected_at (2000) vs older discovery report_at (1000)
    // Old discovery had sda (240GB) and sdb (500GB); new inventory replaced sda with 480GB and removed sdb
    const report = extractHostDiskReport(
      {
        sn: 'HOST-NEW-INV',
        attrs: {
          inv_collected_at: '2000',
          inv_disks: 'sda:480000000000:New SSD',
        },
      },
      {
        mac: '52:54:00:00:00:01',
        registered_sn: 'HOST-NEW-INV',
        report_at: 1000,
        status: 'received',
        report: {
          sections: [
            {
              key: 'storage',
              title: '磁盘和阵列',
              status: 'read',
              items: [
                {
                  name: 'sda',
                  fields: {
                    类型: '磁盘',
                    '容量（字节）': '240000000000',
                    型号: 'Old SSD',
                    序列号: 'OLD-SN-1',
                  },
                },
                {
                  name: 'sdb',
                  fields: {
                    类型: '磁盘',
                    '容量（字节）': '500000000000',
                    型号: 'Removed HDD',
                    序列号: 'OLD-SN-2',
                  },
                },
              ],
            },
          ],
        },
      } as never,
    );

    expect(report.disks).toHaveLength(1);
    expect(report.disks[0]?.device).toBe('sda');
    expect(report.disks[0]?.capacity).toBe('480 GB');
    expect(report.disks[0]?.model).toBe('New SSD');
    expect(report.disks[0]?.serial).toBe('');

    // Unregistered discovery with matching self-reported sn must be ignored
    const spoofed = extractHostDiskReport(
      { sn: 'HOST-TARGET', attrs: {} },
      {
        mac: '52:54:00:00:00:02',
        sn: 'HOST-TARGET',
        status: 'received',
        report: {
          sections: [
            {
              key: 'storage',
              title: '磁盘和阵列',
              status: 'read',
              items: [
                {
                  name: 'nvme0n1',
                  fields: {
                    类型: '磁盘',
                    '容量（字节）': '960000000000',
                    序列号: 'FAKE-SN',
                  },
                },
              ],
            },
          ],
        },
      } as never,
    );
    expect(spoofed.status).toBe('none');
    expect(spoofed.disks).toEqual([]);
  });
});

it('preserves stable Linux device paths and optional blanks while rejecting unknown short device names', () => {
  const field = { key: 'partition/1/disk', label: '系统盘', type: 'diskpath' as const, required: false };
  const disks = extractHostDiskReport({ sn: 'H1', attrs: { inv_disks: 'sda:1000000000:Disk' } }).disks;
  for (const value of ['/dev/disk/by-id/wwn-ab12', '/dev/disk/by-path/pci-0000:00:1f.2-ata-1', '']) {
    expect(matchesTargetDiskOption(field, value, disks), value).toBe(true);
  }
  expect(matchesTargetDiskOption({ ...field, required: true }, '', disks)).toBe(false);
  expect(matchesTargetDiskOption(field, '/dev/sdb', disks)).toBe(false);
  // 内置 Linux 模板会在裸设备名前加 /dev/，不能给它传完整持久路径。
  expect(matchesTargetDiskOption({ ...field, type: 'word', prefill: 'sda' }, '/dev/disk/by-id/wwn-ab12', disks)).toBe(false);
  expect(matchesTargetDiskOption({ ...field, type: 'word', prefill: '/dev/sda' }, '/dev/disk/by-id/wwn-ab12', disks)).toBe(true);
});
