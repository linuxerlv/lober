import { describe, expect, it, vi } from 'vitest';

import { collectEditable, latestOnly, mergeEvents, selectionChanged } from './wizard-state';

describe('wizard preserves server authority', () => {
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
        'SN-1': { 'tpl/password': 'a b', 'partition/1/disk': '/dev/sdb', fake: 'x' },
        'SN-2': { 'tpl/password': 'injected' },
      }),
    ).toEqual({ 'SN-1': { 'tpl/password': 'a b' } });
  });

  it('clears all downstream selection and values when ISO changes', () => {
    expect(
      selectionChanged(
        { isoID: 'old', templateID: 'tpl', templateVersion: 2, entered: { host: { x: '1' } } },
        'iso',
        'new',
      ),
    ).toEqual({ isoID: 'new', templateID: '', templateVersion: 0, entered: {} });
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
    expect(mergeEvents(old as never, replay as never).map((event) => event.seq)).toEqual([1, 2, 3]);
  });
});
