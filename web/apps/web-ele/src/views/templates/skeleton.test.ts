import type { Schemas } from '#/api/console/generated';

import { describe, expect, it, vi } from 'vitest';

import { loadSkeletons, skeletonContent } from './skeleton';

const source: Schemas['TemplateResponse'] = {
  id: 'builtin-unattend',
  version: 5,
  rev: 1,
  name: 'lober:unattend',
  builtin: true,
  state: 'approved',
  author: '',
  approver: '',
  derivedFrom: null,
  family: 'windows',
  series: ['windows-server', 'windows-desktop'],
  body: '<settings>{{slot:partition}}</settings>',
  problems: null,
  params: null,
  bindings: null,
  assembly: {
    partition: [
      {
        hookID: 'disk',
        hookVersion: 3,
        bindings: {
          disk_index: {
            source: 'fixed',
            value: '0',
            attr: '',
            editable: false,
          },
        },
        required: null,
        timeoutSec: null,
      },
    ],
  },
};

describe('新建装机模板', () => {
  it('读取所有页，每种骨架只取已批准的最新内置版本', async () => {
    const read = vi
      .fn()
      .mockResolvedValueOnce({
        items: [
          { ...source, version: 2 },
          { ...source, builtin: false, id: 'custom' },
        ],
        next: 'p2',
      })
      .mockResolvedValueOnce({
        items: [source, { ...source, version: 6, state: 'draft' }],
        next: '',
      });
    expect(await loadSkeletons(read)).toEqual([source]);
    expect(read.mock.calls[1]?.[0]).toMatchObject({
      cursor: 'p2',
      prefix: 'lober:',
      approved: true,
    });
  });

  it('只复制所选系统的正文和参数，从空 Hook 列表开始', () => {
    const content = skeletonContent(source, 'windows-server');
    expect(content).toEqual({
      family: 'windows',
      series: ['windows-server'],
      body: source.body,
      params: [],
      bindings: {},
      assembly: {},
    });
    expect(Object.keys(source.assembly!)).toEqual(['partition']);
    expect(() => skeletonContent(source, 'rocky')).toThrow();
  });
});
