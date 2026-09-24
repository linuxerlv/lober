/**
 * app 层自写的响应拦截器，替代 vben 上游的 `defaultResponseInterceptor`。
 *
 * ============================================================================
 * 重要：此文件让本项目从此偏离 vben 的响应判定约定，升级上游 `@vben/request`
 * 时必须重新核对这一点，不要因为看起来像是"临时代码"就删掉。
 * ============================================================================
 *
 * 背景（docs/superpowers/specs/2026-09-24-gin-migration-contract.md §0、§8）：
 * 后端 internal/apierr 一直在输出真实 HTTP 状态码（404/409/500...），从未
 * 固定返回 200。vben 上游的 `defaultResponseInterceptor` 按
 * `{codeField:'code', dataField:'data', successCode:0}` 配置工作，只读
 * `body.code`、完全忽略 HTTP 状态码——这不是后端的缺陷，是前端此前没有
 * 使用后端已经给出的信息。
 *
 * 本拦截器改为：HTTP 2xx 判成功并解包 `data`；非 2xx 按状态码分流为失败，
 * 同时读 `body.code` 做同状态码下的业务细分（例如 409 下 1005/1006/1007/1008
 * 是状态冲突、该主机已有任务、仍被引用、拒绝重装四种不同的业务分支，
 * 对应的 UI 提示文案也不同，见契约 §2 错误码表）。
 *
 * 与 defaultResponseInterceptor 的关键差异：旧实现把"成功"完全定义为
 * `status in [200,400) && body.code === 0`，因此哪怕状态码已经是 404/409，
 * 只要恰好 `body.code === 0`（实际不会发生，但旧逻辑本身没有区分状态码）
 * 也会被判成功；反过来，只要 body.code !== 0 就必须手工 `throw` 一个
 * `Object.assign({}, response, {response})` 来伪造出 axios 拒绝态特有的
 * `error.response` 形状，因为 axios 对 2xx-3xx 默认是 resolve 而不是
 * reject。本拦截器把判成败的依据换成状态码后，这一步伪造大部分场景不再
 * 需要：非 2xx 时 axios 的默认 `validateStatus`（仅 2xx 视为成功）本身就会
 * 让请求进入 rejected 分支，`error.response.data`/`error.response.status`
 * 已经是完整、真实的响应，不需要额外包装。
 *
 * 仍保留的防御性分支：如果后端某个端点意外用 2xx 状态码返回了非零
 * `code`（违反契约，理论上不应发生），本拦截器仍会视为失败并抛出同样
 * 形状的 error，避免这种契约违例被静默当成功处理。
 *
 * 下游消费者（未改动，行为不变）：
 * - `authenticateResponseInterceptor`（token 过期判定）：读
 *   `error.response.status === 401`，本次改动后这个判定更加名副其实——
 *   以前 401 也可能被后端包装成 200（历史遗留，实测已确认不会发生，
 *   但拦截器本身按 401 状态码判定，与后端是否真的用 200 无关）。
 * - `errorMessageResponseInterceptor`：读 `error.response.data.message`
 *   与 `error.response.status`，字段路径不变，行为不变。
 * - `web/apps/web-ele/src/components/audit-timeline.vue` 等直接读
 *   `error.response.data.code` 的调用方：字段路径不变，行为不变。
 */
import type { RequestResponse, ResponseInterceptorConfig } from '@vben/request';

/** 与 internal/apierr.Envelope 对应的响应体形态，字段名/大小写不能改。 */
interface Envelope<T = any> {
  code: number;
  data: T;
  message: string;
}

/** 与 internal/apierr.Code 的值同步，见 internal/apierr/apierr.go。 */
const CODE_OK = 0;

/**
 * 409 状态码下四种业务分支对应的提示文案（契约 §2）。状态码本身只能
 * 表达"状态冲突"这一个粗粒度事实，四个 code 才能区分具体是哪一种冲突，
 * UI 提示必须不同，否则用户不知道该做什么。
 *
 * 调用方可选择直接展示这份文案（`error.friendlyMessage`），也可以继续
 * 沿用旧写法自行读 `error.response.data.code` 分支（如
 * audit-timeline.vue 对 1003 的处理），两种方式并存不冲突。
 */
const CONFLICT_MESSAGES: Record<number, string> = {
  1005: '当前状态已被其他操作改变，请刷新页面查看最新状态后再重试',
  1006: '该主机已存在一个未结束的任务，请先处理或取消该任务后再试',
  1007: '该对象仍被其他记录引用，无法执行此操作',
  1008: '当前机器状态不允许重装，请人工核实后再操作',
};

/**
 * 由本拦截器附加在 rejected error 对象上的规范化信息，供调用方按需使用。
 * 不是替换 `error.response.data`，是在其之上提供一层更方便的读法。
 */
interface NormalizedErrorExtras {
  /** 与 error.response.status 相同，冗余一份便于调用方少写一层可选链 */
  httpStatus?: number;
  /** 与 error.response.data.code 相同 */
  bizCode?: number;
  /**
   * 面向用户的中文提示：409 时取 CONFLICT_MESSAGES 对应文案，否则取
   * body.message；两者都没有时留空，由 errorMessageResponseInterceptor
   * 按状态码给通用兜底文案。
   */
  friendlyMessage?: string;
}

/**
 * 判断响应是否应当被当作成功处理。
 *
 * 主判据是 HTTP 状态码（契约 §0：后端一直给真实状态码）；额外校验
 * `body.code === CODE_OK` 作为防御性兜底，防止契约被意外违反时静默
 * 放过（例如某个 handler 忘记在出错路径设置状态码，仍返回了 2xx，
 * 但 body.code 已经如实标记了失败）。
 */
function isSuccessResponse(status: number, body: unknown): boolean {
  if (status < 200 || status >= 300) {
    return false;
  }
  const code = (body as Envelope | undefined)?.code;
  // body 不是合法 Envelope（例如空响应体）时不因此判失败，只在 code
  // 字段确实存在且不等于 CODE_OK 时才判失败——校验条件不能比契约更严格。
  return code === undefined || code === CODE_OK;
}

/**
 * app 层自写的响应拦截器：等价替代
 * `defaultResponseInterceptor({codeField:'code', dataField:'data', successCode:0})`。
 *
 * 用法与旧拦截器一致，直接注册到 `client.addResponseInterceptor`。
 */
export function statusCodeResponseInterceptor(): ResponseInterceptorConfig {
  return {
    fulfilled: (response: RequestResponse) => {
      const { config, data: responseData, status } = response;

      if (config.responseReturn === 'raw') {
        return response;
      }

      if (isSuccessResponse(status, responseData)) {
        if (config.responseReturn === 'body') {
          return responseData;
        }
        return (responseData as Envelope | undefined)?.data;
      }

      // 契约违反的防御性分支（2xx 但 body.code !== 0）：伪造出与 axios
      // 原生 rejected 态一致的 error.response 形状，保证下游
      // （authenticateResponseInterceptor/errorMessageResponseInterceptor/
      // 各调用方）读 error.response.data 时行为一致，不需要区分"axios
      // 原生 reject"与"本拦截器主动 throw"这两种来路。
      throw attachNormalizedExtras(
        Object.assign({}, response, { response }),
        status,
        responseData,
      );
    },
  };
}

/**
 * 在真正的 axios 请求错误（非 2xx，走 axios 原生 rejected 分支）上补一份
 * `friendlyMessage`/`bizCode`/`httpStatus`，方便调用方读取 409 细分文案，
 * 不改变 `error.response` 本身的结构。
 *
 * 网络错误、超时等没有 `error.response` 的场景直接透传，交给下游的
 * `errorMessageResponseInterceptor` 按 err.message 处理，本拦截器不介入。
 */
export function normalizeErrorResponseInterceptor(): ResponseInterceptorConfig {
  return {
    rejected: (error: any) => {
      const response = error?.response;
      if (!response) {
        throw error;
      }
      throw attachNormalizedExtras(error, response.status, response.data);
    },
  };
}

function attachNormalizedExtras<T extends object>(
  error: T,
  status: number | undefined,
  body: unknown,
): T & NormalizedErrorExtras {
  const envelope = body as Envelope | undefined;
  const bizCode = envelope?.code;
  const friendlyMessage =
    status === 409 && bizCode !== undefined
      ? (CONFLICT_MESSAGES[bizCode] ?? envelope?.message)
      : envelope?.message;

  return Object.assign(error, {
    httpStatus: status,
    bizCode,
    friendlyMessage,
  });
}
