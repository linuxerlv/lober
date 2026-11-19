import { describe, expect, it } from 'vitest';

import {
  eventDetails,
  eventLabel,
  findFailureEvent,
  latestEventReport,
  taskProgressStage,
} from './event-details';

it('finds the current failure event even when later log records arrive', () => {
  const timeout = {
    seq: 22,
    kind: 'machine.timeout',
    fields: { stage: 'install' },
  };
  expect(
    findFailureEvent([
      timeout,
      { seq: 23, kind: 'installer.syslog', fields: {} },
    ])?.event,
  ).toBe(timeout);
});

it('does not report an older failure after retry or an optional script error as the cause', () => {
  expect(
    findFailureEvent([
      { seq: 1, kind: 'machine.failed', fields: { reason: '旧故障' } },
      { seq: 2, kind: 'machine.retry', fields: {} },
      { seq: 3, kind: 'machine.hook-report', fields: { rc: '1' } },
    ]),
  ).toBeUndefined();
});

it('recognizes slot.end and required hook.report dot-event failures', () => {
  const hookFailure = {
    seq: 10,
    kind: 'hook.report',
    fields: { slot: '1', instance: '1', rc: '2', reason: '脚本退出 2' },
  };
  expect(
    findFailureEvent([hookFailure], () => true),
  ).toEqual({
    event: hookFailure,
    summary: '脚本退出 2',
  });
  expect(findFailureEvent([hookFailure], () => false)).toBeUndefined();

  const slotFailure = {
    seq: 11,
    kind: 'slot.end',
    fields: { slot: '1', rc: '1' },
  };
  expect(findFailureEvent([slotFailure])?.event).toBe(slotFailure);
});

describe('task event details', () => {
  it('shows decoded UTF-8 logs alongside phase diagnostics', () => {
    const body = new TextEncoder().encode('安装完成\n');
    const encoded = btoa(String.fromCharCode(...body));
    expect(eventDetails({ stage: 'post', log_base64: encoded })).toBe(
      'stage=post；日志=安装完成\n',
    );
  });

  it('escapes binary control bytes in log text', () => {
    expect(eventDetails({ log_base64: btoa('a\0b\x1b[31m') })).toBe(
      String.raw`日志=a\x00b\x1b[31m`,
    );
  });

  it('handles malformed and absent log data without breaking event rendering', () => {
    expect(eventDetails({ log_base64: '%%%invalid' })).toBe('日志=[无法解码]');
    expect(eventDetails(undefined)).toBe('');
    expect(eventDetails(null)).toBe('');
  });

  it('derives real progress stage and latest report time/content from real task events without fabricating percentages', () => {
    const events = [
      {
        seq: 1,
        at: 1_750_000_010,
        kind: 'machine.boot',
        fields: { arch: 'x86_64', firmware: 'uefi' },
      },
      {
        seq: 2,
        at: 1_750_000_020,
        kind: 'machine.installing',
        fields: {},
      },
      {
        seq: 3,
        at: 1_750_000_030,
        kind: 'slot.start',
        fields: { slot: '1', timing: 'pre' },
      },
      {
        seq: 4,
        at: 1_750_000_040,
        kind: 'hook.report',
        fields: {
          slot: '1',
          timing: 'pre',
          instance: '1',
          rc: '0',
          log_base64: btoa('disk partition ok'),
        },
      },
    ];
    expect(taskProgressStage('installing', events as never)).toContain('装前脚本');
    expect(taskProgressStage('installing', events as never)).not.toMatch(/%/);
    const latest = latestEventReport(events as never);
    expect(latest?.at).toBe(1_750_000_040);
    expect(latest?.title).toBe(eventLabel('hook.report'));
    expect(latest?.content).toContain('disk partition ok');

    // Legacy or empty events remain compatible
    expect(taskProgressStage('pending', [])).toContain('等待引导');
    expect(latestEventReport([])).toBeUndefined();
  });

  it('prioritizes authoritative task state over incompatible or previous-attempt events and bounds summary length', () => {
    const failedHookEvents = [
      {
        seq: 1,
        at: 1_750_000_010,
        kind: 'machine.installing',
        fields: {},
      },
      {
        seq: 2,
        at: 1_750_000_020,
        kind: 'hook.report',
        fields: { slot: '1', timing: 'pre', instance: '1', rc: '1' },
      },
    ];
    // Required hook failure only emits hook.report while task.state becomes failed
    expect(taskProgressStage('failed', failedHookEvents as never)).toContain('失败');
    expect(taskProgressStage('failed', failedHookEvents as never)).not.toMatch(/^安装中/);

    // Retried task in pending state receiving late syslog from previous attempt
    const retriedEvents = [
      ...failedHookEvents,
      {
        seq: 3,
        at: 1_750_000_030,
        kind: 'machine.retry',
        fields: {},
      },
      {
        seq: 4,
        at: 1_750_000_035,
        kind: 'installer.syslog',
        fields: { log_base64: btoa('late old attempt log') },
      },
    ];
    expect(taskProgressStage('pending', retriedEvents as never)).toContain('等待引导');
    // Previous attempt logs must not be presented as current progress report in pending state
    expect(latestEventReport(retriedEvents as never, 'pending')?.kind).toBe('machine.retry');

    // Terminal states done and cancelled must always win
    expect(taskProgressStage('done', failedHookEvents as never)).toBe('已完成');
    expect(taskProgressStage('cancelled', failedHookEvents as never)).toBe('已取消');

    // Summary truncates overly long content and ignores empty fields while full eventDetails stays intact
    const longText = 'A'.repeat(300);
    const longEvents = [
      {
        seq: 5,
        at: 1_750_000_050,
        kind: 'installer.syslog',
        fields: { empty: '   ', log_base64: btoa(longText) },
      },
    ];
    const report = latestEventReport(longEvents as never, 'installing');
    expect(report?.content.length).toBeLessThanOrEqual(161);
    expect(report?.content).toContain('…');
    expect(report?.content).not.toContain('empty=');
    expect(eventDetails(longEvents[0]!.fields)).toContain(longText);
  });
});
