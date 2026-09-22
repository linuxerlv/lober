/**
 * JSON Schema → VbenFormSchema 转换层（SPEC §12.7）。
 *
 * `GET /console/v1/templates/{id}/form-schema` 返回 JSON Schema，框架表单吃的是
 * 自己的 schema 数组。服务端输出被限定为一个子集（详见 {@link JsonSchemaProperty}），
 * 超出子集的字段在 hook 注册时就已经被服务端拒绝，所以这里的映射函数可以假定输入
 * 在子集内 —— 但遇到子集外的东西要显式抛错，不能静默丢字段（静默丢字段的后果是
 * 运维填了一个参数、装机时那个参数不存在，极难定位）。
 *
 * 前端校验只是体验，不是防线：这里映射出的规则给即时反馈，服务端仍按 SPEC §5.6
 * 对 form_data 做类型白名单强校验，那些值会进 kickstart 脚本。映射不必追求与服务端
 * 完全等价，但不能比服务端更宽松到让用户以为能提交。
 */

import type { Component } from 'vue';

import type { VbenFormFieldSchema } from '@vben/common-ui';

import type { ComponentPropsMap, ComponentType } from './component';

import { z } from '@vben/common-ui';

import HostSelect from '#/components/host-select.vue';

/**
 * 本文件产出的字段 schema 类型：{@link VbenFormFieldSchema} 按业务组件注册表
 * （{@link ComponentType} / {@link ComponentPropsMap}）narrow 一遍，与
 * `adapter/form.ts` 里 `VbenFormSchema` 的做法一致。之所以不直接用
 * `VbenFormSchema`（那是 `FormFieldSchema | FormGroupSchema` 的联合类型），
 * 是因为这里只产出字段、不产出分组，用联合类型会导致 componentProps / rules /
 * dependencies 这些字段访问不到（TS 认为可能落在分组分支上）。
 */
export type FieldSchema = VbenFormFieldSchema<ComponentType, ComponentPropsMap>;

/** 服务端输出的 JSON Schema 允许的字段类型子集 */
export type JsonSchemaType =
  | 'array'
  | 'boolean'
  | 'integer'
  | 'number'
  | 'string';

/** x-widget 允许的取值子集，决定映射到框架表单的哪个组件 */
export type JsonSchemaWidget =
  | 'host-select'
  | 'input'
  | 'ip'
  | 'number'
  | 'password'
  | 'select'
  | 'switch'
  | 'textarea';

/** x-depends-on：当 field 字段等于 value 时才显示当前字段 */
export interface JsonSchemaDependsOn {
  field: string;
  value: unknown;
}

/**
 * 单个字段的 JSON Schema 定义（服务端子集）。
 *
 * 校验：enum / pattern / minimum / maximum / minLength / maxLength（required 由
 * 父级 `required` 数组表达，不在字段自身上）。
 * 展示：title / description / default。
 * 扩展：x-widget / x-order / x-depends-on。
 */
export interface JsonSchemaProperty {
  type: JsonSchemaType;
  title?: string;
  description?: string;
  default?: unknown;
  enum?: unknown[];
  pattern?: string;
  minimum?: number;
  maximum?: number;
  minLength?: number;
  maxLength?: number;
  /** type 为 array 时，元素的取值集合（仅用于多选场景） */
  items?: { enum?: unknown[]; type?: JsonSchemaType };
  'x-widget'?: JsonSchemaWidget;
  'x-order'?: number;
  'x-depends-on'?: JsonSchemaDependsOn;
}

/** 表单聚合端点 `data` 的整体形状（JSON Schema object 顶层） */
export interface JsonSchemaForm {
  type: 'object';
  properties: Record<string, JsonSchemaProperty>;
  required?: string[];
}

/**
 * 子集外的 schema 抛出这个错误，而不是静默丢字段。
 * 正常情况下不应该发生：§2.2 的 params_schema 注册期校验已经把这类 schema
 * 挡在注册阶段。出现这个错误说明注册期校验和这里的子集定义脱节了，需要立即
 * 定位，不能被用户当成"这个参数不用填"而略过。
 */
export class UnsupportedJsonSchemaError extends Error {
  constructor(message: string) {
    super(`[form-schema] ${message}`);
    this.name = 'UnsupportedJsonSchemaError';
  }
}

const SUPPORTED_TYPES: readonly JsonSchemaType[] = [
  'array',
  'boolean',
  'integer',
  'number',
  'string',
];

const SUPPORTED_WIDGETS: readonly JsonSchemaWidget[] = [
  'host-select',
  'input',
  'ip',
  'number',
  'password',
  'select',
  'switch',
  'textarea',
];

/** RFC 5952 简化版 IPv4/IPv6 校验（够用即可，服务端才是最终裁决者） */
const IPV4_PATTERN =
  /^(25[0-5]|2[0-4]\d|1\d{2}|[1-9]?\d)(\.(25[0-5]|2[0-4]\d|1\d{2}|[1-9]?\d)){3}$/;
const IPV6_PATTERN = /^([\dA-Fa-f]{1,4}:){7}[\dA-Fa-f]{1,4}$|^::1$|^::$/;

function assertSupportedType(fieldName: string, type: JsonSchemaType): void {
  if (!SUPPORTED_TYPES.includes(type)) {
    throw new UnsupportedJsonSchemaError(
      `字段 "${fieldName}" 的 type "${type}" 超出服务端子集（允许 ${SUPPORTED_TYPES.join(' | ')}），拒绝渲染`,
    );
  }
}

function assertSupportedWidget(
  fieldName: string,
  widget: string | undefined,
): asserts widget is JsonSchemaWidget | undefined {
  if (
    widget !== undefined &&
    !SUPPORTED_WIDGETS.includes(widget as JsonSchemaWidget)
  ) {
    throw new UnsupportedJsonSchemaError(
      `字段 "${fieldName}" 的 x-widget "${widget}" 超出服务端子集（允许 ${SUPPORTED_WIDGETS.join(' | ')}），拒绝渲染`,
    );
  }
}

/** 推断字段未显式给出 x-widget 时的默认组件：按 JSON Schema type 而不是瞎猜 */
function inferDefaultWidget(type: JsonSchemaType): JsonSchemaWidget {
  switch (type) {
    case 'array': {
      return 'select';
    }
    case 'boolean': {
      return 'switch';
    }
    case 'integer':
    case 'number': {
      return 'number';
    }
    default: {
      return 'input';
    }
  }
}

/** 由 enum（或 array + items.enum）构造 Select/CheckboxGroup 的选项列表 */
function buildOptions(
  prop: JsonSchemaProperty,
): { label: string; value: unknown }[] {
  const values = prop.type === 'array' ? prop.items?.enum : prop.enum;
  return (values ?? []).map((value) => ({ label: String(value), value }));
}

/**
 * 组装单个字段的 Zod 校验规则。
 * required 由调用方传入（父级 required 数组决定），不由字段自身的 enum/pattern 推断。
 */
function buildRules(
  fieldName: string,
  prop: JsonSchemaProperty,
  required: boolean,
) {
  const widget = prop['x-widget'];

  if (prop.type === 'boolean') {
    // switch 没有"未填"的中间态，required 对 boolean 没有额外约束意义
    return z.boolean().optional();
  }

  if (prop.type === 'integer' || prop.type === 'number') {
    let rule = prop.type === 'integer' ? z.number().int() : z.number();
    if (prop.minimum !== undefined) rule = rule.min(prop.minimum);
    if (prop.maximum !== undefined) rule = rule.max(prop.maximum);
    return required ? rule : rule.optional();
  }

  // string（含 array 多选时对每个选中值的约束交给 componentProps.options，
  // 这里只处理标量 string 字段的规则）
  let rule = z.string();
  if (prop.minLength !== undefined) rule = rule.min(prop.minLength);
  if (prop.maxLength !== undefined) rule = rule.max(prop.maxLength);
  if (prop.pattern !== undefined) {
    rule = rule.regex(
      new RegExp(prop.pattern),
      `不符合格式要求：${prop.pattern}`,
    );
  }
  if (widget === 'ip' && prop.pattern === undefined) {
    // 服务端没给 pattern 时，ip 控件仍要有基本形状校验，不能退化成裸 input
    rule = rule.refine(
      (value) => IPV4_PATTERN.test(value) || IPV6_PATTERN.test(value),
      '不是合法的 IP 地址',
    );
  }
  if (!required) {
    return rule.optional();
  }
  return rule.min(1, `${prop.title ?? fieldName} 不能为空`);
}

interface ComponentMapping {
  /**
   * 具体产出只会是两种：{@link ComponentType} 字符串 key（走 adapter/component
   * 已注册的组件），或者 host-select 这个直接传组件对象的特例（Component 类型，
   * 绕开字符串注册表）。用 `Component | ComponentType` 而不是从
   * `FieldSchema['component']` 取值，是因为在联合类型上做属性访问会把
   * `undefined` 也带进来，产出一个本不该有的可选态。
   */
  component: Component | ComponentType;
  componentProps?: Record<string, unknown>;
}

/** x-widget → 框架组件 + 固定 componentProps 的映射表 */
function mapWidgetToComponent(
  fieldName: string,
  prop: JsonSchemaProperty,
  widget: JsonSchemaWidget,
): ComponentMapping {
  switch (widget) {
    case 'host-select': {
      // 直接传组件对象（不是字符串），绕开需要在 adapter/component 注册字符串
      // key 的路径——见 @vben-core/form-ui 的 form-field.vue：非字符串的
      // component 会被直接当作要渲染的组件使用。
      return { component: HostSelect };
    }
    case 'input': {
      return { component: 'Input' };
    }
    case 'ip': {
      return {
        component: 'Input',
        componentProps: { placeholder: '例如 192.168.1.10' },
      };
    }
    case 'number': {
      return { component: 'InputNumber' };
    }
    case 'password': {
      return { component: 'VbenInputPassword' };
    }
    case 'select': {
      return {
        component: 'Select',
        componentProps: {
          options: buildOptions(prop),
          multiple: prop.type === 'array',
        },
      };
    }
    case 'switch': {
      return { component: 'Switch' };
    }
    case 'textarea': {
      return {
        component: 'Input',
        componentProps: { type: 'textarea', rows: 3 },
      };
    }
    default: {
      // 理论上走不到这里：assertSupportedWidget 已经在上游拒绝了未知 widget。
      // 保留这个分支只是为了在新增 JsonSchemaWidget 枚举值却漏改这里时，
      // TypeScript 的 exhaustiveness 检查能报错，而不是运行时静默漏掉。
      const exhaustiveCheck: never = widget;
      throw new UnsupportedJsonSchemaError(
        `字段 "${fieldName}" 的 x-widget "${exhaustiveCheck}" 没有对应的组件映射`,
      );
    }
  }
}

/** 将 x-depends-on 映射为框架表单的 dependencies（TanStack Form 版 API：resolve + triggerFields） */
function buildDependencies(
  dependsOn: JsonSchemaDependsOn,
): FieldSchema['dependencies'] {
  const { field, value } = dependsOn;
  return {
    triggerFields: [field],
    resolve: ({ values }) => ({ show: values[field] === value }),
  };
}

/**
 * 转换单个字段：JsonSchemaProperty → VbenFormSchema。
 * 子集外的 type / x-widget 会抛 {@link UnsupportedJsonSchemaError}。
 */
export function convertJsonSchemaProperty(
  fieldName: string,
  prop: JsonSchemaProperty,
  required: boolean,
): FieldSchema {
  assertSupportedType(fieldName, prop.type);
  assertSupportedWidget(fieldName, prop['x-widget']);

  const widget = prop['x-widget'] ?? inferDefaultWidget(prop.type);
  const { component, componentProps } = mapWidgetToComponent(
    fieldName,
    prop,
    widget,
  );

  const schema: FieldSchema = {
    component,
    fieldName,
    label: prop.title ?? fieldName,
    rules: buildRules(fieldName, prop, required),
    ...(componentProps ? { componentProps } : {}),
    ...(prop.description ? { help: prop.description } : {}),
    ...(prop.default === undefined ? {} : { defaultValue: prop.default }),
  };

  if (prop['x-depends-on']) {
    schema.dependencies = buildDependencies(prop['x-depends-on']);
  }

  return schema;
}

/**
 * 转换整份表单聚合 JSON Schema → VbenFormSchema[]，按 x-order 排序
 * （缺省 x-order 的字段按原始出现顺序，排在有 x-order 的字段之后）。
 *
 * 子集外的字段会让整份转换直接抛错中止，不会返回一份"丢了几个字段"的残缺数组——
 * 建任务表单缺一个字段，运维填完提交时才会在服务端校验发现，此时定位成本远高于
 * 在渲染前就整体失败。
 */
export function convertJsonSchemaForm(schema: JsonSchemaForm): FieldSchema[] {
  const requiredSet = new Set(schema.required);
  const entries = Object.entries(schema.properties);

  const withOrder = entries.map(([fieldName, prop], index) => ({
    fieldName,
    prop,
    order: prop['x-order'] ?? Number.MAX_SAFE_INTEGER,
    index,
  }));

  withOrder.sort((a, b) => a.order - b.order || a.index - b.index);

  return withOrder.map(({ fieldName, prop }) =>
    convertJsonSchemaProperty(fieldName, prop, requiredSet.has(fieldName)),
  );
}
