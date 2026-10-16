<script setup lang="ts">
import type {
  Host,
  ISO,
  Authoring,
  WizardField,
} from '#/api/console/operations';
import { onMounted, reactive, ref } from 'vue';
import { useRouter } from 'vue-router';
import { ElAlert, ElButton, ElCard, ElInput, ElMessageBox } from 'element-plus';
import { operations } from '#/api/console/operations';
import AccessButton from '#/components/access-button.vue';
import { useConsoleAccess } from '#/composables/use-console-access';
import { errorMessage } from '#/composables/use-console-list';
const router = useRouter(),
  { can, run } = useConsoleAccess();
const hosts = ref<
    (Host & { selectable: boolean; problems: { message: string }[] })[]
  >([]),
  isos = ref<(ISO & { usable: boolean })[]>([]),
  templates = ref<(Authoring & { usable: boolean })[]>([]);
const hostSN = ref(''),
  isoID = ref(''),
  templateKey = ref(''),
  query = ref(''),
  error = ref(''),
  busy = ref(false),
  fields = ref<WizardField[]>([]),
  ready = ref(false);
const entered = reactive<Record<string, string>>({});
function selection() {
  const template = templates.value.find(
    (t) => `${t.id}@${t.version}` === templateKey.value,
  );
  if (!template) throw new Error('请选择模板');
  return {
    isoID: isoID.value,
    templateID: template.id,
    templateVersion: template.version,
  };
}
async function listHosts() {
  if (!can('task:create')) return;
  try {
    hosts.value = (
      await operations.wizardHosts({ q: query.value, limit: 100 })
    ).items;
  } catch (e) {
    error.value = errorMessage(e);
  }
}
async function chooseHost() {
  isoID.value = '';
  templateKey.value = '';
  fields.value = [];
  ready.value = false;
  try {
    isos.value = (await operations.wizardISOs([hostSN.value])).items;
  } catch (e) {
    error.value = errorMessage(e);
  }
}
async function chooseISO() {
  templateKey.value = '';
  fields.value = [];
  ready.value = false;
  try {
    templates.value = (await operations.wizardTemplates(isoID.value)).items;
  } catch (e) {
    error.value = errorMessage(e);
  }
}
async function chooseTemplate() {
  ready.value = false;
  try {
    const result = await operations.wizardParams(selection(), [hostSN.value]);
    const host = result.hosts[0];
    const problems = [...result.common, ...(host?.problems || [])];
    fields.value = host?.fields || [];
    for (const key of Object.keys(entered)) delete entered[key];
    for (const field of fields.value)
      if (!field.locked) entered[field.key] = field.prefill;
    error.value = problems.map((p) => p.message).join('\n');
    ready.value = problems.length === 0;
  } catch (e) {
    error.value = errorMessage(e);
  }
}
async function submit() {
  if (busy.value || !ready.value || !can('task:create')) return;
  try {
    await ElMessageBox.confirm(
      `确认对主机 ${hostSN.value} 创建装机任务？请核实目标机器，装机可能清空磁盘。`,
      '确认建任务',
      { type: 'warning' },
    );
  } catch {
    return;
  }
  busy.value = true;
  try {
    const result = await run('task:create', () =>
      operations.createTasks({
        ...selection(),
        hosts: [{ sn: hostSN.value, entered: { ...entered } }],
      }),
    );
    if (result?.tasks[0]) await router.push('/tasks/' + result.tasks[0].id);
  } catch (e) {
    error.value = errorMessage(e);
  } finally {
    busy.value = false;
  }
}
onMounted(listHosts);
</script>
<template>
  <main class="p-6">
    <header class="mb-5 flex items-center gap-3">
      <h1 class="mr-auto text-2xl font-semibold">建任务</h1>
      <ElButton @click="router.push('/tasks')">返回列表</ElButton>
    </header>
    <ElAlert
      v-if="error"
      class="mb-4"
      :title="error"
      type="error"
      :closable="false"
    /><ElCard class="max-w-3xl"
      ><form @submit.prevent="submit" class="grid gap-5">
        <label
          >1. 选择主机
          <div class="my-2 flex gap-2">
            <ElInput v-model="query" placeholder="按主机 SN 查询" /><ElButton
              @click="listHosts"
              >查找主机</ElButton
            >
          </div>
          <select
            v-model="hostSN"
            class="w-full rounded border bg-transparent p-2"
            @change="chooseHost"
          >
            <option value="" disabled>请选择已登记主机</option>
            <option
              v-for="host in hosts"
              :key="host.sn"
              :value="host.sn"
              :disabled="!host.selectable"
            >
              {{ host.sn }} · {{ host.arch }} · {{ host.firmware
              }}{{
                host.selectable
                  ? ''
                  : '（' + host.problems.map((p) => p.message).join('；') + '）'
              }}
            </option>
          </select></label
        ><label
          >2. 选择 ISO<select
            v-model="isoID"
            :disabled="!hostSN"
            class="mt-2 w-full rounded border bg-transparent p-2"
            @change="chooseISO"
          >
            <option value="" disabled>请选择可用 ISO</option>
            <option
              v-for="iso in isos"
              :key="iso.id"
              :value="iso.id"
              :disabled="!iso.usable"
            >
              {{ iso.name }} · {{ iso.series }} · {{ iso.version
              }}{{ iso.usable ? '' : '（不适用）' }}
            </option>
          </select></label
        ><label
          >3. 选择已审批模板<select
            v-model="templateKey"
            :disabled="!isoID"
            class="mt-2 w-full rounded border bg-transparent p-2"
            @change="chooseTemplate"
          >
            <option value="" disabled>请选择模板</option>
            <option
              v-for="template in templates"
              :key="template.id + '@' + template.version"
              :value="template.id + '@' + template.version"
              :disabled="!template.usable"
            >
              {{ template.name }} · v{{ template.version
              }}{{ template.usable ? '' : '（不适用）' }}
            </option>
          </select></label
        >
        <section v-if="ready">
          <h2 class="mb-3 font-medium">4. 装机参数</h2>
          <label v-for="field in fields" :key="field.key" class="mb-3 block"
            >{{ field.label }}{{ field.required ? ' *' : ''
            }}<ElInput
              v-if="field.locked"
              :model-value="field.prefill"
              disabled /><select
              v-else-if="field.options.length"
              v-model="entered[field.key]"
              class="mt-2 w-full rounded border bg-transparent p-2"
            >
              <option
                v-for="option in field.options"
                :key="option.value"
                :value="option.value"
              >
                {{ option.label }}
              </option></select
            ><ElInput
              v-else
              v-model="entered[field.key]"
              :type="field.type === 'secret' ? 'password' : 'text'"
              :required="field.required"
              :placeholder="field.list ? '按字段要求输入列表' : ''"
          /></label>
        </section>
        <div>
          <AccessButton
            permission="task:create"
            native-type="submit"
            type="primary"
            :disabled="!ready"
            :loading="busy"
            >确认并创建任务</AccessButton
          >
        </div>
      </form></ElCard
    >
  </main>
</template>
