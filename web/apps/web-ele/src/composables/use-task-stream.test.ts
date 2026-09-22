import { describe, expect, it } from 'vitest';

import {
  computeBackoffDelayMs,
  isTerminalPhase,
  isTerminalTaskState,
  parseSseFrame,
} from './use-task-stream';

describe('computeBackoffDelayMs', () => {
  it('returns the base delay for the first attempt', () => {
    expect(computeBackoffDelayMs(0)).toBe(1000);
  });

  it('doubles the delay for each subsequent attempt', () => {
    expect(computeBackoffDelayMs(1)).toBe(2000);
    expect(computeBackoffDelayMs(2)).toBe(4000);
    expect(computeBackoffDelayMs(3)).toBe(8000);
  });

  it('caps the delay at the configured maximum', () => {
    expect(computeBackoffDelayMs(10, { maxMs: 30_000 })).toBe(30_000);
  });

  it('respects a custom base delay', () => {
    expect(computeBackoffDelayMs(0, { baseMs: 500 })).toBe(500);
    expect(computeBackoffDelayMs(1, { baseMs: 500 })).toBe(1000);
  });

  it('falls back to the base delay for a negative attempt count', () => {
    expect(computeBackoffDelayMs(-1)).toBe(1000);
  });
});

describe('isTerminalTaskState / isTerminalPhase', () => {
  it('treats done and failed as terminal', () => {
    expect(isTerminalTaskState('done')).toBe(true);
    expect(isTerminalTaskState('failed')).toBe(true);
    expect(isTerminalPhase('done')).toBe(true);
    expect(isTerminalPhase('failed')).toBe(true);
  });

  it('treats every other state/phase as non-terminal', () => {
    for (const value of [
      'pending',
      'booting',
      'initializing',
      'rebooting',
      'pre',
      'packages',
      'post',
      'firstboot',
    ]) {
      expect(isTerminalTaskState(value)).toBe(false);
      expect(isTerminalPhase(value)).toBe(false);
    }
  });
});

describe('parseSseFrame', () => {
  it('parses a single-line data frame with id and event', () => {
    const frame = parseSseFrame(
      'id: 110\nevent: task-event\ndata: {"seq":110,"phase":"done"}',
    );
    expect(frame).toEqual({
      data: '{"seq":110,"phase":"done"}',
      event: 'task-event',
      id: '110',
    });
  });

  it('joins multi-line data fields with newlines', () => {
    const frame = parseSseFrame('data: line1\ndata: line2');
    expect(frame).toEqual({
      data: 'line1\nline2',
      event: undefined,
      id: undefined,
    });
  });

  it('ignores comment lines starting with a colon', () => {
    const frame = parseSseFrame(': keep-alive\ndata: {"seq":1}');
    expect(frame).toEqual({
      data: '{"seq":1}',
      event: undefined,
      id: undefined,
    });
  });

  it('returns null when there is no data field', () => {
    expect(parseSseFrame('id: 5\nevent: ping')).toBeNull();
  });

  it('handles values without a leading space after the colon', () => {
    const frame = parseSseFrame('id:42\ndata:{"seq":42}');
    expect(frame).toEqual({ data: '{"seq":42}', event: undefined, id: '42' });
  });
});
