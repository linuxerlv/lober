/* eslint-disable vue/one-component-per-file -- Lightweight table adapter tests actual wizard selections. */
import { createApp, defineComponent, h, nextTick, ref } from 'vue';

import { afterEach, expect, it, vi } from 'vitest';

const io = vi.hoisted(() => ({
  hosts: vi.fn(),
  isos: vi.fn(),
  templates: vi.fn(),
  params: vi.fn(),
}));
vi.mock('vue-router', () => ({ useRouter: () => ({ push: vi.fn() }) }));
vi.mock('@vben/stores', () => ({
  useAccessStore: () => ({ accessCodes: ['task:create'] }),
}));
vi.mock('@vben/common-ui', () => ({
  Page: defineComponent({
    setup: (_, ctx) => () => h('main', ctx.slots.default?.()),
  }),
}));
vi.mock('#/api/console/wizard', () => ({ wizardAPI: io }));
vi.mock('#/adapter/vxe-table', () => ({
  useVbenVxeGrid: (options: { gridEvents: { checkboxChange: () => void } }) => {
    const rows = ref<Array<{ sn: string; selectable: boolean }>>([]);
    const checked = ref<string[]>([]);
    return [
      defineComponent({
        setup: () => () =>
          h(
            'div',
            rows.value.map((row) =>
              h('label', [
                row.sn,
                h('input', {
                  type: 'checkbox',
                  name: row.sn,
                  checked: checked.value.includes(row.sn),
                  onChange: (event: Event) => {
                    checked.value = (event.target as HTMLInputElement).checked
                      ? [...checked.value, row.sn]
                      : checked.value.filter((sn) => sn !== row.sn);
                    options.gridEvents.checkboxChange();
                  },
                }),
              ]),
            ),
          ),
      }),
      {
        setGridOptions: ({ data }: { data: typeof rows.value }) => {
          rows.value = data;
          checked.value = [];
        },
        grid: {
          getCheckboxRecords: () =>
            rows.value.filter((row) => checked.value.includes(row.sn)),
          clearCheckboxRow: async () => {
            checked.value = [];
          },
          setCheckboxRow: async (selected: typeof rows.value) => {
            checked.value = selected.map((row) => row.sn);
          },
        },
      },
    ];
  },
}));
import Wizard from './new.vue';

let stop: (() => void) | undefined;
afterEach(() => {
  stop?.();
  document.body.innerHTML = '';
  vi.resetAllMocks();
});
function click(text: string) {
  const button = Array.from(
    document.querySelectorAll<HTMLButtonElement>('button'),
  ).find((el) => el.textContent?.trim() === text);
  expect(button, text).toBeTruthy();
  button!.click();
}
function mount() {
  const el = document.createElement('div');
  document.body.append(el);
  const app = createApp(Wizard);
  app.mount(el);
  stop = () => app.unmount();
  return el;
}
function chooseHost(sn: string, selected = true) {
  const input = document.querySelector<HTMLInputElement>(
    `input[name="${sn}"]`,
  )!;
  input.checked = selected;
  input.dispatchEvent(new Event('change', { bubbles: true }));
}

it('uses numbered host pages and preserves selected hosts and visible checks across pages', async () => {
  io.hosts.mockImplementation(async ({ page }: { page: number }) => ({
    items: [{ sn: page === 1 ? 'H1' : 'H2', selectable: true }],
    total: 21,
    next: '',
  }));
  io.isos.mockResolvedValue({ items: [], total: 0, next: '' });
  const el = mount();
  await vi.waitFor(() => expect(el.textContent).toContain('H1'));
  expect(io.hosts).toHaveBeenLastCalledWith({
    q: '',
    attr_key: '',
    attr_value: '',
    page: 1,
    page_size: 20,
  });
  chooseHost('H1');
  const query = el.querySelector<HTMLInputElement>(
    'input[placeholder="按 SN 前缀搜索"]',
  )!;
  query.value = 'unsubmitted';
  query.dispatchEvent(new Event('input', { bubbles: true }));
  el.querySelector<HTMLButtonElement>('.el-pagination .btn-next')!.click();
  await vi.waitFor(() => expect(el.textContent).toContain('H2'));
  expect(io.hosts).toHaveBeenLastCalledWith(
    expect.objectContaining({ page: 2, q: '' }),
  );
  chooseHost('H2');
  await vi.waitFor(() => expect(el.textContent).toContain('已选 2 台'));
  el.querySelector<HTMLButtonElement>('.el-pagination .btn-prev')!.click();
  await vi.waitFor(() =>
    expect(
      el.querySelector<HTMLInputElement>('input[name="H1"]')?.checked,
    ).toBe(true),
  );
  chooseHost('H1', false);
  click('下一步');
  await vi.waitFor(() =>
    expect(io.isos).toHaveBeenCalledWith({
      hostSNs: ['H2'],
      page: 1,
      page_size: 20,
    }),
  );
});

it('keeps chosen ISO and template labels while navigating their pages', async () => {
  io.hosts.mockResolvedValue({
    items: [{ sn: 'H1', selectable: true }],
    total: 1,
    next: '',
  });
  io.isos.mockImplementation(async ({ page }: { page: number }) => ({
    items: [{ id: `iso${page}`, name: `ISO page ${page}`, usable: true }],
    total: 21,
    next: '',
  }));
  io.templates.mockImplementation(async ({ page }: { page: number }) => ({
    items: [
      {
        id: `tpl${page}`,
        version: page,
        name: `Template page ${page}`,
        usable: true,
      },
    ],
    total: 21,
    next: '',
  }));
  io.params.mockResolvedValue({ hosts: [], common: [] });
  const el = mount();
  await vi.waitFor(() => expect(el.textContent).toContain('H1'));
  chooseHost('H1');
  click('下一步');
  await vi.waitFor(() => expect(el.textContent).toContain('ISO page 1'));
  el.querySelector<HTMLInputElement>('input[type="radio"]')!.click();
  await nextTick();
  el.querySelector<HTMLButtonElement>('.el-pagination .btn-next')!.click();
  await vi.waitFor(() => expect(el.textContent).toContain('ISO page 2'));
  click('下一步');
  await vi.waitFor(() => expect(el.textContent).toContain('Template page 1'));
  expect(io.templates).toHaveBeenLastCalledWith({
    isoID: 'iso1',
    page: 1,
    page_size: 20,
  });
  el.querySelector<HTMLInputElement>('input[type="radio"]')!.click();
  await nextTick();
  el.querySelector<HTMLButtonElement>('.el-pagination .btn-next')!.click();
  await vi.waitFor(() => expect(el.textContent).toContain('Template page 2'));
  click('下一步');
  await vi.waitFor(() =>
    expect(io.params).toHaveBeenCalledWith(
      expect.objectContaining({
        isoID: 'iso1',
        templateID: 'tpl1',
        templateVersion: 1,
      }),
    ),
  );
  await vi.waitFor(() => expect(el.textContent).toContain('校验并确认'));
  click('校验并确认');
  await vi.waitFor(() => expect(el.textContent).toContain('确认批次'));
  expect(el.textContent).toContain('ISO page 1');
  expect(el.textContent).toContain('Template page 1');
});

it('retains host rows, selection and page when loading another page fails', async () => {
  io.hosts
    .mockResolvedValueOnce({
      items: [{ sn: 'H1', selectable: true }],
      total: 21,
      next: '',
    })
    .mockRejectedValueOnce(new Error('host page unavailable'));
  const el = mount();
  await vi.waitFor(() => expect(el.textContent).toContain('H1'));
  chooseHost('H1');
  el.querySelector<HTMLButtonElement>('.el-pagination .btn-next')!.click();
  await vi.waitFor(() =>
    expect(el.textContent).toContain('host page unavailable'),
  );
  expect(el.querySelector<HTMLInputElement>('input[name="H1"]')?.checked).toBe(
    true,
  );
  expect(el.querySelector('.el-pager .is-active')?.textContent?.trim()).toBe(
    '1',
  );
  expect(el.textContent).toContain('已选 1 台');
});
