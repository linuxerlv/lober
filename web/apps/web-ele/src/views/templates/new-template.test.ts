import type { Schemas } from '#/api/console/generated';

import { createApp, defineComponent, h, nextTick } from 'vue';
import { expect, it, vi } from 'vitest';

const api = vi.hoisted(() => ({
  getTemplateListApi: vi.fn(),
  getTemplateVersionsApi: vi.fn(),
  createTemplateApi: vi.fn(),
  updateTemplateApi: vi.fn(),
  getTemplateSlotsApi: vi.fn(),
  getSourceDiffApi: vi.fn(),
  getHookCandidatesApi: vi.fn(),
  getPreviewParamsApi: vi.fn(),
  previewTemplateApi: vi.fn(),
  templateLifecycleApi: vi.fn(),
}));
vi.mock('#/api/console/template', () => api);
vi.mock('#/api/console/hook', () => ({ getHookApi: vi.fn() }));
vi.mock('@vben/stores', () => ({
  useAccessStore: () => ({ accessCodes: ['template:write'] }),
  useUserStore: () => ({ userInfo: { username: 'tester' } }),
}));
vi.mock('@vben/common-ui', () => ({
  Page: defineComponent({
    setup:
      (_, { slots }) =>
      () =>
        h('main', [slots.extra?.(), slots.default?.()]),
  }),
}));
vi.mock('#/adapter/vxe-table', () => ({
  useVbenVxeGrid: () => [() => h('div'), { setGridOptions: vi.fn() }],
}));

import { getHookApi } from '#/api/console/hook';
import Templates from './index.vue';

function button(label: string) {
  const found = Array.from(document.querySelectorAll('button')).find(
    (el) => el.textContent?.trim() === label,
  );
  if (!found) throw new Error(`找不到按钮 ${label}`);
  return found;
}
function fill(selector: string, value: string) {
  const input = document.querySelector<HTMLInputElement | HTMLTextAreaElement>(
    selector,
  )!;
  input.value = value;
  input.dispatchEvent(new Event('input', { bubbles: true }));
}

it('选系统、加 Hook、保存参数，再显示后端返回的文件', async () => {
  const skeleton = {
    id: 'builtin-kickstart',
    version: 6,
    rev: 1,
    name: 'lober:kickstart',
    builtin: true,
    state: 'approved',
    author: '',
    approver: '',
    family: 'linux',
    series: ['rhel', 'rocky'],
    body: '# backend\n{{slot:packages}}',
    params: [],
    bindings: {},
    derivedFrom: null,
    problems: [],
    assembly: {
      packages: [
        {
          hookID: 'packages',
          hookVersion: 4,
          required: null,
          timeoutSec: null,
          bindings: {
            packages: {
              source: 'fixed',
              value: 'curl',
              editable: true,
              attr: '',
            },
          },
        },
      ],
    },
  } satisfies Schemas['TemplateResponse'];
  const hook = {
    id: 'packages',
    version: 4,
    name: '软件包',
    params: [
      {
        name: 'packages',
        label: '软件包名',
        list: true,
        type: 'word',
      },
    ],
  } as Schemas['HookResponse'];
  vi.mocked(getHookApi).mockRejectedValue(new Error('没有 Hook 详情读取权限'));
  api.getTemplateListApi.mockImplementation(async ({ approved }) => ({
    items: approved ? [skeleton] : [],
    total: 0,
    next: '',
  }));
  const draft = {
    ...skeleton,
    id: 'draft-id',
    version: 1,
    builtin: false,
    state: 'draft',
  };
  api.createTemplateApi.mockImplementation(async (data) => ({
    ...draft,
    ...data,
  }));
  api.updateTemplateApi.mockImplementation(async (_id, _version, data) => ({
    ...draft,
    ...data,
    rev: data.rev + 1,
  }));
  api.getTemplateVersionsApi.mockResolvedValue({ items: [] });
  api.getTemplateSlotsApi.mockResolvedValue({
    slots: [{ name: 'packages', exec: false }],
  });
  api.getSourceDiffApi.mockResolvedValue({});
  api.getHookCandidatesApi.mockResolvedValue({
    items: [{ usable: true, hook }],
    total: 1,
  });
  api.getPreviewParamsApi.mockResolvedValue({ fields: [], sample: ['主机'] });
  api.previewTemplateApi.mockResolvedValue({
    files: [{ name: 'ks.cfg', kind: 'answer', body: '%packages\ntmux\n%end' }],
  });

  const root = document.createElement('div');
  document.body.append(root);
  const app = createApp(Templates);
  app.mount(root);
  try {
    button('新建模板').click();
    await vi.waitFor(() =>
      expect(
        document.querySelector('input[placeholder^="例如 Rocky"]'),
      ).toBeTruthy(),
    );
    fill('input[placeholder^="例如 Rocky"]', 'Rocky-Base');
    button('下一步').click();
    await nextTick();
    expect(api.createTemplateApi).not.toHaveBeenCalled();
    const systemLabel = Array.from(document.querySelectorAll('label')).find(
      (el) => el.textContent?.trim().startsWith('操作系统'),
    );
    systemLabel!.querySelector<HTMLElement>('.el-select__wrapper')!.click();
    await vi.waitFor(() => {
      const option = Array.from(
        document.querySelectorAll<HTMLElement>('.el-select-dropdown__item'),
      ).find((el) => el.textContent?.trim() === 'rocky');
      expect(option).toBeTruthy();
      option!.click();
    });
    await nextTick();
    button('下一步').click();
    await vi.waitFor(() =>
      expect(api.createTemplateApi).toHaveBeenCalledWith(
        expect.objectContaining({
          name: 'Rocky-Base',
          series: ['rocky'],
          body: skeleton.body,
          assembly: {},
        }),
      ),
    );
    await vi.waitFor(() => expect(button('添加 hook').disabled).toBe(false));
    button('添加 hook').click();
    await vi.waitFor(() => expect(button('选用').disabled).toBe(false));
    button('选用').click();
    await nextTick();
    button('保存草稿').click();
    await vi.waitFor(() =>
      expect(getHookApi).toHaveBeenCalledWith('packages', 4),
    );
    await vi.waitFor(() => expect(button('下一步').disabled).toBe(false));
    button('下一步').click();
    await vi.waitFor(() =>
      expect(
        document.querySelector<HTMLTextAreaElement>(
          'textarea[placeholder="每行填一项"]',
        )?.value,
      ).toBe('curl'),
    );
    fill('textarea[placeholder="每行填一项"]', 'tmux');
    await nextTick();
    api.updateTemplateApi.mockRejectedValueOnce(new Error('保存冲突'));
    button('预览装机文件').click();
    await vi.waitFor(() =>
      expect(document.body.textContent).toContain('保存冲突'),
    );
    await vi.waitFor(() => expect(button('预览装机文件').disabled).toBe(false));
    expect(api.getPreviewParamsApi).not.toHaveBeenCalled();
    button('预览装机文件').click();
    await vi.waitFor(
      () =>
        expect(api.getPreviewParamsApi).toHaveBeenCalledWith(
          'draft-id',
          1,
          expect.objectContaining({ hostSN: '', isoID: '' }),
        ),
      { timeout: 5000 },
    );
    await vi.waitFor(() => expect(button('生成预览').disabled).toBe(false), {
      timeout: 5000,
    });
    expect(api.updateTemplateApi).toHaveBeenLastCalledWith(
      'draft-id',
      1,
      expect.objectContaining({
        assembly: {
          packages: [
            expect.objectContaining({
              hookID: 'packages',
              bindings: {
                packages: expect.objectContaining({
                  source: 'fixed',
                  value: 'tmux',
                }),
              },
            }),
          ],
        },
      }),
    );
    button('生成预览').click();
    await vi.waitFor(() =>
      expect(document.body.textContent).toContain('%packages\ntmux\n%end'),
    );
    expect(api.previewTemplateApi).toHaveBeenCalledWith(
      'draft-id',
      1,
      expect.objectContaining({ entered: {} }),
    );
  } finally {
    app.unmount();
    root.remove();
  }
}, 20_000);
