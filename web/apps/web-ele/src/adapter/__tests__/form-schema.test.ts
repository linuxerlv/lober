import type {
  FieldSchema,
  JsonSchemaForm,
  JsonSchemaProperty,
} from '../form-schema';

import { z } from '@vben/common-ui';

import { describe, expect, it } from 'vitest';

import {
  convertJsonSchemaForm,
  convertJsonSchemaProperty,
  UnsupportedJsonSchemaError,
} from '../form-schema';

/** 造一个最小可用的 string 字段，方便各用例覆盖单个变量 */
function stringProp(
  overrides: Partial<JsonSchemaProperty> = {},
): JsonSchemaProperty {
  return { type: 'string', title: '示例字段', ...overrides };
}

/**
 * `rules` 的声明类型是 `FormSchemaRuleType`（框架内置校验规则名 | 业务自定义
 * 规则名 | ZodType 的联合），本文件产出的永远是 ZodType 分支，这里窄化一下
 * 好让测试直接调用 `.safeParse`，不用每个用例都写一遍类型断言。
 */
function asZodType(rules: FieldSchema['rules'] | undefined) {
  if (!(rules instanceof z.ZodType)) {
    throw new TypeError('期望 rules 是 ZodType，但拿到了别的规则名');
  }
  return rules;
}

/** 造一个 resolve 回调需要的最小完整 context（本文件的 resolve 只读 values） */
function stubResolveContext(values: Record<string, unknown>) {
  return {
    actions: {} as never,
    controller: {} as never,
    schema: {} as never,
    values,
  };
}

describe('convertJsonSchemaProperty：八种 x-widget 映射', () => {
  it('input → Input 组件', () => {
    const schema = convertJsonSchemaProperty(
      'name',
      stringProp({ 'x-widget': 'input' }),
      false,
    );
    expect(schema.component).toBe('Input');
  });

  it('textarea → Input 组件 + type=textarea（没有单独注册 Textarea 组件类型）', () => {
    const schema = convertJsonSchemaProperty(
      'note',
      stringProp({ 'x-widget': 'textarea' }),
      false,
    );
    expect(schema.component).toBe('Input');
    expect(schema.componentProps).toMatchObject({ type: 'textarea' });
  });

  it('password → VbenInputPassword 组件（框架内置，不需要业务注册）', () => {
    const schema = convertJsonSchemaProperty(
      'secret',
      stringProp({ 'x-widget': 'password' }),
      false,
    );
    expect(schema.component).toBe('VbenInputPassword');
  });

  it('number → InputNumber 组件', () => {
    const schema = convertJsonSchemaProperty(
      'count',
      { type: 'integer', title: '数量', 'x-widget': 'number' },
      false,
    );
    expect(schema.component).toBe('InputNumber');
  });

  it('switch → Switch 组件', () => {
    const schema = convertJsonSchemaProperty(
      'enabled',
      { type: 'boolean', title: '开关', 'x-widget': 'switch' },
      false,
    );
    expect(schema.component).toBe('Switch');
  });

  it('select → Select 组件，options 来自 enum', () => {
    const schema = convertJsonSchemaProperty(
      'mode',
      stringProp({ 'x-widget': 'select', enum: ['a', 'b'] }),
      false,
    );
    expect(schema.component).toBe('Select');
    expect(schema.componentProps).toMatchObject({
      options: [
        { label: 'a', value: 'a' },
        { label: 'b', value: 'b' },
      ],
      multiple: false,
    });
  });

  it('select + type array → Select 组件，multiple=true，options 来自 items.enum', () => {
    const schema = convertJsonSchemaProperty(
      'tags',
      {
        type: 'array',
        title: '标签',
        'x-widget': 'select',
        items: { type: 'string', enum: ['x', 'y'] },
      },
      false,
    );
    expect(schema.componentProps).toMatchObject({
      options: [
        { label: 'x', value: 'x' },
        { label: 'y', value: 'y' },
      ],
      multiple: true,
    });
  });

  it('ip → Input 组件，且没有 pattern 时仍带 IP 形状校验', () => {
    const schema = convertJsonSchemaProperty(
      'bizIp',
      stringProp({ 'x-widget': 'ip' }),
      true,
    );
    expect(schema.component).toBe('Input');
    expect(asZodType(schema.rules).safeParse('192.168.1.10').success).toBe(
      true,
    );
    expect(asZodType(schema.rules).safeParse('not-an-ip').success).toBe(false);
  });

  it('host-select → 直接传组件对象（不是字符串 key），绕开 adapter/component 注册', () => {
    const schema = convertJsonSchemaProperty(
      'hostId',
      stringProp({ 'x-widget': 'host-select' }),
      true,
    );
    expect(typeof schema.component).not.toBe('string');
  });
});

describe('convertJsonSchemaProperty：required 与 pattern 转成校验规则', () => {
  it('required=true 时空字符串校验失败', () => {
    const schema = convertJsonSchemaProperty('name', stringProp(), true);
    expect(asZodType(schema.rules).safeParse('').success).toBe(false);
    expect(asZodType(schema.rules).safeParse('张三').success).toBe(true);
  });

  it('required=false 时允许 undefined', () => {
    const schema = convertJsonSchemaProperty('nickname', stringProp(), false);
    expect(asZodType(schema.rules).safeParse(undefined).success).toBe(true);
  });

  it('pattern 转成 regex 校验，不匹配时失败', () => {
    const schema = convertJsonSchemaProperty(
      'code',
      stringProp({ pattern: '^[A-Z]{3}\\d{3}$' }),
      true,
    );
    expect(asZodType(schema.rules).safeParse('ABC123').success).toBe(true);
    expect(asZodType(schema.rules).safeParse('abc123').success).toBe(false);
  });

  it('minLength / maxLength 生效', () => {
    const schema = convertJsonSchemaProperty(
      'short',
      stringProp({ minLength: 2, maxLength: 4 }),
      true,
    );
    expect(asZodType(schema.rules).safeParse('a').success).toBe(false);
    expect(asZodType(schema.rules).safeParse('ab').success).toBe(true);
    expect(asZodType(schema.rules).safeParse('abcde').success).toBe(false);
  });

  it('number 类型的 minimum / maximum 生效', () => {
    const schema = convertJsonSchemaProperty(
      'port',
      { type: 'integer', title: '端口', minimum: 1, maximum: 65_535 },
      true,
    );
    expect(asZodType(schema.rules).safeParse(0).success).toBe(false);
    expect(asZodType(schema.rules).safeParse(8080).success).toBe(true);
    expect(asZodType(schema.rules).safeParse(70_000).success).toBe(false);
  });
});

describe('convertJsonSchemaProperty：x-depends-on 转成 dependencies', () => {
  it('生成 triggerFields 与 resolve，resolve 按目标字段值决定 show', () => {
    const schema = convertJsonSchemaProperty(
      'vlanId',
      stringProp({ 'x-depends-on': { field: 'networkMode', value: 'vlan' } }),
      false,
    );
    expect(schema.dependencies?.triggerFields).toEqual(['networkMode']);

    const resolve = schema.dependencies?.resolve;
    expect(resolve).toBeTypeOf('function');
    if (!resolve) throw new Error('resolve 未定义');

    const shown = resolve(stubResolveContext({ networkMode: 'vlan' }));
    const hidden = resolve(stubResolveContext({ networkMode: 'flat' }));

    expect(shown).toEqual({ show: true });
    expect(hidden).toEqual({ show: false });
  });

  it('没有 x-depends-on 时不生成 dependencies', () => {
    const schema = convertJsonSchemaProperty('plain', stringProp(), false);
    expect(schema.dependencies).toBeUndefined();
  });
});

describe('convertJsonSchemaProperty：子集外的输入显式拒绝', () => {
  it('子集外的 type 抛错，不是静默丢字段', () => {
    expect(() =>
      convertJsonSchemaProperty(
        'weird',
        {
          type: 'null' as unknown as JsonSchemaProperty['type'],
          title: '异常字段',
        },
        false,
      ),
    ).toThrow(UnsupportedJsonSchemaError);
  });

  it('子集外的 x-widget 抛错', () => {
    expect(() =>
      convertJsonSchemaProperty(
        'weird',
        stringProp({
          'x-widget': 'datepicker' as unknown as JsonSchemaProperty['x-widget'],
        }),
        false,
      ),
    ).toThrow(UnsupportedJsonSchemaError);
  });

  it('错误信息带字段名，方便定位是哪个 hook 的 params_schema 出的问题', () => {
    // 不用 try/catch 里条件断言（lint: no-conditional-expect）：
    // 用 expect().toThrow() 先确认必然抛错，再单独调用一次取到 error 对象校验消息内容。
    const call = () =>
      convertJsonSchemaProperty(
        'mysteryField',
        stringProp({
          'x-widget': 'nope' as unknown as JsonSchemaProperty['x-widget'],
        }),
        false,
      );
    expect(call).toThrow(UnsupportedJsonSchemaError);
    expect(call).toThrow(/mysteryField/);
  });
});

describe('convertJsonSchemaForm：整份表单聚合', () => {
  const form: JsonSchemaForm = {
    type: 'object',
    required: ['sn'],
    properties: {
      note: stringProp({ 'x-widget': 'textarea', 'x-order': 2 }),
      sn: stringProp({ 'x-widget': 'host-select', 'x-order': 1 }),
      unordered: stringProp(),
    },
  };

  it('按 x-order 排序，缺省 x-order 的字段排在最后', () => {
    const schemas = convertJsonSchemaForm(form);
    expect(schemas.map((s) => s.fieldName)).toEqual([
      'sn',
      'note',
      'unordered',
    ]);
  });

  it('required 数组决定每个字段的必填校验，而不是字段自身猜测', () => {
    const schemas = convertJsonSchemaForm(form);
    const sn = schemas.find((s) => s.fieldName === 'sn');
    const note = schemas.find((s) => s.fieldName === 'note');
    expect(asZodType(sn?.rules).safeParse('').success).toBe(false);
    expect(asZodType(note?.rules).safeParse(undefined).success).toBe(true);
  });

  it('聚合中任意一个字段子集外，整体转换直接抛错而不是返回残缺数组', () => {
    const badForm: JsonSchemaForm = {
      type: 'object',
      properties: {
        ok: stringProp(),
        bad: stringProp({
          'x-widget':
            'unknown-widget' as unknown as JsonSchemaProperty['x-widget'],
        }),
      },
    };
    expect(() => convertJsonSchemaForm(badForm)).toThrow(
      UnsupportedJsonSchemaError,
    );
  });
});
