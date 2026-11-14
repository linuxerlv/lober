import { describe, expect, it } from 'vitest';

import { eventDetails, findFailureEvent } from './event-details';

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
});
