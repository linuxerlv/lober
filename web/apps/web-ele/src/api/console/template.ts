import { requestClient } from '#/api/request';

/**
 * 模板端点（docs/contracts/http-console.md「5. 模板」节，端点 17-20）。
 *
 * 字段名与 model.Template 对应。
 */

/** model.DiskPolicy 取值域（authoring.go），五个取值 */
export type DiskPolicy =
  | 'by-hook'
  | 'by-model'
  | 'first'
  | 'largest'
  | 'raid1-ssd';

/** model.OnFailurePolicy 取值域（authoring.go），四个取值 */
export type OnFailurePolicy = 'halt' | 'keep-network' | 'reboot' | 'shell';

/** model.TemplateState 取值域 */
export type TemplateState = 'active' | 'deprecated' | 'draft';

export interface TemplateHookRef {
  hookDigest: string;
  hookId: string;
  order: number;
  paramsDefaults: Record<string, unknown>;
  required: boolean;
  stage: string;
}

/** 模板条目，与 model.Template 对应 */
export interface TemplateItem {
  answerTemplate: string;
  artifactSetId: string;
  diskPolicy: DiskPolicy;
  hookRefs: TemplateHookRef[];
  id: string;
  kernelArgs: string;
  name: string;
  onFailure: OnFailurePolicy;
  requiredFormFields: string[];
  state: TemplateState;
  version: number;
}

/**
 * POST /templates 与 PUT /templates/{id} 共用入参。
 * hookRefs 的 hookDigest 由服务端按 hookId 查出当前 active 版本自动填入，
 * 前端可不传，传了服务端也以查出的值为准（pin 的是内容而非引用）。
 */
export interface TemplateUpsertParams {
  answerTemplate: string;
  artifactSetId: string;
  diskPolicy: DiskPolicy;
  hookRefs: Array<
    Omit<TemplateHookRef, 'hookDigest'> & { hookDigest?: string }
  >;
  kernelArgs: string;
  name: string;
  onFailure: OnFailurePolicy;
  requiredFormFields: string[];
}

/**
 * POST /templates/{id}/preview 入参。formData 用于假数据渲染，
 * hostId 可选，不传则用服务端内置的假 Host。
 */
export interface TemplatePreviewParams {
  formData: Record<string, unknown>;
  hostId?: string;
}

/** POST /templates/{id}/preview 返回 */
export interface TemplatePreviewResult {
  answerFile: string;
  bootMenu: string;
  warnings: string[];
}

/**
 * GET /templates —— 契约缺口（如实记录，不是本文件发明的端点）：
 * docs/contracts/http-console.md 冻结的 28 个端点里没有模板列表端点（只有
 * POST 建版本 / PUT 建新版本 / GET form-schema / POST preview 四个按 id
 * 操作的端点）。但 SPEC §12.6 的 /templates 列表页与建任务页（src/views/
 * tasks/new.vue）的"选模板"步骤都需要能列出模板，new.vue 已经假定本函数
 * 存在并按 hosts/artifact-sets 同构的列表端点形态调用。这里照此实现，若
 * 后端未实现 GET /templates 会在运行时报错——需要 Integrate 阶段与契约
 * 所有者确认是否补一个端点编号（类比本轮已有的 reject 端点先例），或改为
 * 从别处（如 preview/form-schema 响应）取模板元数据。
 */
export async function getTemplateListApi() {
  return requestClient.get<TemplateItem[]>('/templates');
}

/** POST /templates，返回 version=1、state=draft 的新 TemplateItem */
export async function createTemplateApi(data: TemplateUpsertParams) {
  return requestClient.post<TemplateItem>('/templates', data);
}

/**
 * PUT /templates/{id}
 * 建新版本，新记录 version + 1，state 重置为 draft。
 */
export async function updateTemplateApi(
  id: string,
  data: TemplateUpsertParams,
) {
  return requestClient.put<TemplateItem>(`/templates/${id}`, data);
}

/**
 * GET /templates/{id}/form-schema
 * data 本身就是可直接喂给 src/adapter/form-schema.ts 的 JSON Schema 对象，
 * 不再嵌一层（SPEC §12.7）。
 */
export async function getTemplateFormSchemaApi(id: string) {
  return requestClient.get<Record<string, unknown>>(
    `/templates/${id}/form-schema`,
  );
}

/**
 * POST /templates/{id}/preview
 * 把「建任务 → 上电 → 等数分钟 → 失败 → 翻日志」的反馈周期压到秒级（SPEC §8.2），
 * 渲染失败返回 CodeInternal，message 带具体渲染错误。
 */
export async function previewTemplateApi(
  id: string,
  data: TemplatePreviewParams,
) {
  return requestClient.post<TemplatePreviewResult>(
    `/templates/${id}/preview`,
    data,
  );
}
