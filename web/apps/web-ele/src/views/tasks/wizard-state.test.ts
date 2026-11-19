import { describe, expect, it, vi } from 'vitest';

import {
  diskLabel,
  matchesTargetDisk,
  targetDiskSummary,
} from './wizard-disks';

import {
  collectEditable,
  latestOnly,
  mergeEvents,
  mergeHostSelection,
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
});

describe('目标盘取值方式', () => {
  const disks = [
    { device: 'sda', bytes: '960000000000', model: 'SSD', serial: '' },
  ];
  const host = {
    sn: 'host',
    fields: [
      {
        key: 'partition/1/disk',
        label: '改过的标签',
        locked: false,
        prefill: 'sda',
        list: false,
        options: [],
        type: 'word',
        required: true,
      },
    ],
    targetDisk: { key: 'partition/1/disk', mode: 'device', value: 'sda' },
    problems: [],
  };
  it('respects the installer prefix and never associates a persistent path with an arbitrary disk', () => {
    expect(matchesTargetDisk(host, 'sda', disks)).toBe(true);
    expect(matchesTargetDisk(host, '/dev/sda', disks)).toBe(false);
    expect(matchesTargetDisk(host, '/dev/disk/by-id/example', disks)).toBe(
      false,
    );
    const pathHost = {
      ...host,
      targetDisk: {
        ...host.targetDisk,
        mode: 'device-or-path',
        value: '/dev/disk/by-id/example',
      },
    };
    expect(matchesTargetDisk(pathHost, '/dev/disk/by-id/example', disks)).toBe(
      true,
    );
    expect(targetDiskSummary(pathHost, disks)).toBe(
      '目标盘：/dev/disk/by-id/example（安装时解析此路径）',
    );
  });
  it('formats byte counts and keeps capacity text from older reports', () => {
    expect(diskLabel(disks[0]!)).toBe('sda · 960 GB · SSD');
    expect(
      diskLabel({ ...disks[0]!, bytes: '', capacityText: '896 GiB' }),
    ).toBe('sda · 896 GiB · SSD');
  });
});

it('checks installer path syntax without inventory and compares trimmed disk names', () => {
  const field = {
    key: 'partition/1/disk', label: '磁盘', type: 'word', prefill: '',
    locked: false, list: false, required: true, options: [],
  };
  const host = { sn: 'A', fields: [field], problems: [], targetDisk: { key: field.key, mode: 'device', value: '' } };
  const disks = [{ device: 'vdb', bytes: '1000', model: '', serial: '' }];
  expect(matchesTargetDisk(host, '/dev/vdb', [])).toBe(false);
  expect(matchesTargetDisk(host, 'vdc', [])).toBe(true);
  expect(matchesTargetDisk(host, ' vdb ', disks)).toBe(true);
  expect(matchesTargetDisk(host, 'disk/by-id/wwn-123', [])).toBe(true);
  expect(matchesTargetDisk(host, '', [])).toBe(false);
  field.required = false;
  expect(matchesTargetDisk(host, '', [])).toBe(true);
  expect(matchesTargetDisk(host, '', disks)).toBe(true);
});
