import type { Schemas } from '#/api/console/generated';

import { computed, reactive, ref } from 'vue';

import { NumberedPage } from '#/composables/numbered-page';

export function mergeHostSelection<T extends { sn: string }>(
  selected: T[],
  visible: T[],
  checked: T[],
): T[] {
  const visibleSNs = new Set(visible.map((host) => host.sn));
  return [
    ...new Map(
      [
        ...selected.filter((host) => !visibleSNs.has(host.sn)),
        ...checked.filter((host) => visibleSNs.has(host.sn)),
      ].map((host) => [host.sn, host]),
    ).values(),
  ];
}

export function collectEditable(
  forms: Pick<Schemas['WizardParamsHost'], 'fields' | 'sn'>[],
  values: Record<string, Record<string, string>>,
): Record<string, Record<string, string>> {
  return Object.fromEntries(
    forms.map((host) => [
      host.sn,
      Object.fromEntries(
        (host.fields ?? [])
          .filter((field) => !field.locked)
          .map((field) => [
            field.key,
            values[host.sn]?.[field.key] ?? field.prefill,
          ]),
      ),
    ]),
  );
}

// 每次换主机、镜像或模板都从这里清掉不再适用的选择、参数和预览。
export function useWizardSelection(invalidate: () => void) {
  const selectedHosts = ref<Schemas['WizardHostItem'][]>([]);
  const selectedISO = ref<Schemas['WizardISOItem']>();
  const selectedTemplate = ref<Schemas['WizardTemplateItem']>();
  const isoPage = reactive(new NumberedPage<Schemas['WizardISOItem']>());
  const templatePage = reactive(
    new NumberedPage<Schemas['WizardTemplateItem']>(),
  );
  const forms = ref<Schemas['WizardParamsHost'][]>([]);
  const entered = ref<Record<string, Record<string, string>>>({});
  const commonProblems = ref<Schemas['ProblemJSON'][]>([]);
  const paramsProblems = computed(() => [
    ...commonProblems.value,
    ...forms.value.flatMap((host) => host.problems ?? []),
  ]);
  const activeHost = ref('');
  const batchKey = ref('');
  const batchValue = ref('');
  const preview = ref<Schemas['PreviewResponse']>();
  const previewOpen = ref(false);

  function selectTemplate(tpl?: Schemas['WizardTemplateItem']) {
    invalidate();
    selectedTemplate.value = tpl;
    forms.value = [];
    entered.value = {};
    commonProblems.value = [];
    activeHost.value = '';
    batchKey.value = '';
    batchValue.value = '';
    preview.value = undefined;
    previewOpen.value = false;
  }

  function selectISO(iso?: Schemas['WizardISOItem']) {
    selectTemplate();
    selectedISO.value = iso;
    templatePage.reset();
    templatePage.items = [];
    templatePage.total = 0;
  }

  function selectHosts(hosts: Schemas['WizardHostItem'][]) {
    selectISO();
    selectedHosts.value = hosts;
    isoPage.reset();
    isoPage.items = [];
    isoPage.total = 0;
  }

  function selection() {
    return {
      isoID: selectedISO.value?.id ?? '',
      templateID: selectedTemplate.value?.id ?? '',
      templateVersion: selectedTemplate.value?.version ?? 0,
    };
  }

  return {
    selectedHosts,
    selectedISO,
    selectedTemplate,
    isoPage,
    templatePage,
    forms,
    entered,
    commonProblems,
    paramsProblems,
    activeHost,
    batchKey,
    batchValue,
    preview,
    previewOpen,
    selectHosts,
    selectISO,
    selectTemplate,
    selection,
  };
}

export function latestOnly() {
  let revision = 0;
  return {
    invalidate() {
      revision += 1;
    },
    async run<T>(
      request: Promise<T>,
      apply: (result: T) => void,
      fail?: (error: unknown) => void,
    ) {
      const ticket = ++revision;
      try {
        const result = await request;
        if (ticket === revision) apply(result);
      } catch (error) {
        if (ticket === revision) {
          if (fail) fail(error);
          else throw error;
        }
      }
    },
  };
}

export function mergeEvents<T extends Pick<Schemas['EventItem'], 'seq'>>(
  current: T[],
  incoming: T[],
): T[] {
  const events = new Map(current.map((event) => [event.seq, event]));
  for (const event of incoming) events.set(event.seq, event);
  return [...events.values()].sort((a, b) => a.seq - b.seq);
}
