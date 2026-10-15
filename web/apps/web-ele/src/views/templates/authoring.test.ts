import type { Schemas } from '#/api/console/generated';

import { describe, expect, it } from 'vitest';

import {
  authoringRights,
  hookPayload,
  replaceInSlot,
  replaceInstance,
  templatePayload,
} from './authoring';

describe('authoring model', () => {
  it('keeps builtins readonly and prevents self approval with normalized usernames', () => {
    expect(
      authoringRights(
        { builtin: true, state: 'approved', author: 'alice' },
        ['template:write', 'template:approve'],
        'reviewer',
        'template',
      ),
    ).toEqual({
      edit: false,
      review: false,
      submit: false,
      deprecate: false,
      newVersion: false,
      derive: true,
    });
    expect(
      authoringRights(
        { builtin: false, state: 'review', author: ' Alice ' },
        ['hook:approve'],
        'ALICE',
        'hook',
      ).review,
    ).toBe(false);
    expect(
      authoringRights(
        { builtin: false, state: 'review', author: 'alice' },
        ['hook:approve'],
        'reviewer',
        'hook',
      ).review,
    ).toBe(true);
  });
  it('replaces in place and retains only same-name bindings without mutating the source', () => {
    const binding = {
      source: 'fixed',
      value: 'vda',
      attr: '',
      editable: false,
    };
    const original = {
      hookID: 'old',
      hookVersion: 2,
      timeoutSec: 80,
      required: false,
      bindings: { disk: binding, old: binding },
    };
    const result = replaceInstance(original, {
      id: 'new',
      version: 3,
      params: [{ name: 'disk' }, { name: 'fresh' }],
    });
    expect(result).toEqual({
      hookID: 'new',
      hookVersion: 3,
      timeoutSec: 80,
      required: false,
      bindings: { disk: binding },
    });
    result.bindings!.disk!.value = 'sda';
    expect(original.bindings.disk.value).toBe('vda');
  });
  it('submits current revision and only editable content for a hook draft', () => {
    const row = {
      name: 'a',
      description: '',
      timeoutSec: 30,
      required: true,
      params: [],
      impls: [],
      rev: 9,
      builtin: true,
      state: 'approved',
    };
    expect(hookPayload(row)).toEqual({
      name: 'a',
      description: '',
      timeoutSec: 30,
      required: true,
      params: [],
      impls: [],
      rev: 9,
    });
  });
  it('keeps the server assembly intact when saving body edits before refreshing slots', () => {
    const row = {
      name: 't',
      family: 'linux' as Schemas['CatalogFamily'],
      series: ['rocky'] as Schemas['CatalogSeries'][],
      body: '{{slot:new}}',
      params: [],
      bindings: {},
      assembly: {
        old: [
          {
            hookID: 'h',
            hookVersion: 1,
            bindings: {},
            required: null,
            timeoutSec: null,
          },
        ],
      },
      rev: 4,
      author: 'alice',
    };
    expect(templatePayload(row)).toEqual({
      name: 't',
      family: 'linux' as Schemas['CatalogFamily'],
      series: ['rocky'] as Schemas['CatalogSeries'][],
      body: '{{slot:new}}',
      params: [],
      bindings: {},
      assembly: row.assembly,
      rev: 4,
    });
  });
});

it('replaces an incomplete reference in the same position and can append without changing existing order', () => {
  const before: Schemas['InstanceDTO'][] = [
    {
      hookID: 'first',
      hookVersion: 1,
      bindings: {},
      required: null,
      timeoutSec: null,
    },
    {
      hookID: '',
      hookVersion: 1,
      bindings: {
        disk: { source: 'attr', attr: 'disk', value: '', editable: false },
      },
      required: true,
      timeoutSec: 60,
    },
    {
      hookID: 'third',
      hookVersion: 1,
      bindings: {},
      required: null,
      timeoutSec: null,
    },
  ];
  const hook = { id: 'replacement', version: 2, params: [{ name: 'disk' }] };
  const changed = replaceInSlot(before, 1, hook);
  expect(changed.map((instance) => instance.hookID)).toEqual([
    'first',
    'replacement',
    'third',
  ]);
  expect(changed[1]).toEqual({
    ...before[1],
    hookID: 'replacement',
    hookVersion: 2,
  });
  expect(before[1]!.hookID).toBe('');
  const appended = replaceInSlot(changed, 3, hook);
  expect(appended.map((instance) => instance.hookID)).toEqual([
    'first',
    'replacement',
    'third',
    'replacement',
  ]);
  expect(appended[3]).toEqual({
    hookID: 'replacement',
    hookVersion: 2,
    bindings: {},
    required: null,
    timeoutSec: null,
  });
});
