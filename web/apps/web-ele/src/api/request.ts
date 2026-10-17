/**
 * 该文件可自行根据业务逻辑进行调整
 */
import type { RequestClientOptions } from '@vben/request';

import { useAppConfig } from '@vben/hooks';
import { preferences } from '@vben/preferences';
import { errorMessageResponseInterceptor, RequestClient } from '@vben/request';
import { useAccessStore } from '@vben/stores';

import { ElMessage } from 'element-plus';

import { useAuthStore } from '#/store';
import { sessionResponseInterceptor } from './session-interceptor';

import {
  normalizeErrorResponseInterceptor,
  statusCodeResponseInterceptor,
} from './response-interceptor';

const { apiURL } = useAppConfig(import.meta.env, import.meta.env.PROD);

function createRequestClient(baseURL: string, options?: RequestClientOptions) {
  const client = new RequestClient({
    ...options,
    baseURL,
  });

  function formatToken(token: null | string) {
    return token ? `Bearer ${token}` : null;
  }

  // 请求头处理
  client.addRequestInterceptor({
    fulfilled: async (config) => {
      const accessStore = useAccessStore();

      config.headers.Authorization = formatToken(accessStore.accessToken);
      config.headers['Accept-Language'] = preferences.app.locale;
      return config;
    },
  });

  // 处理返回的响应数据格式：HTTP 2xx 判成功并解包 data，非 2xx 按状态码
  // 判失败（替代 vben 上游只读 body.code 的 defaultResponseInterceptor，
  // 原因见 ./response-interceptor.ts 文件头注释）。
  client.addResponseInterceptor(
    sessionResponseInterceptor(
      () => useAccessStore().accessToken,
      () => useAuthStore().clearSession(),
    ),
  );
  client.addResponseInterceptor(statusCodeResponseInterceptor());

  // 非 2xx 的失败响应在此补一份 friendlyMessage/bizCode，供
  // authenticateResponseInterceptor 之后的下游（包括各调用方）使用；
  // 必须紧跟在 statusCodeResponseInterceptor 之后注册，保证后面的拦截器
  // 与调用方看到的都是补齐后的 error。
  client.addResponseInterceptor(normalizeErrorResponseInterceptor());

  // 通用的错误处理,如果没有进入上面的错误处理逻辑，就会进入这里
  client.addResponseInterceptor(
    errorMessageResponseInterceptor((msg: string, error) => {
      // 这里可以根据业务进行定制,你可以拿到 error 内的信息进行定制化处理，根据不同的 code 做不同的提示，而不是直接使用 message.error 提示 msg
      // 当前mock接口返回的错误字段是 error 或者 message
      const responseData = error?.response?.data ?? {};
      const errorMessage = responseData?.error ?? responseData?.message ?? '';
      // 如果没有错误信息，则会根据状态码进行提示
      ElMessage.error(errorMessage || msg);
    }),
  );

  return client;
}

export const requestClient = createRequestClient(apiURL, {
  responseReturn: 'data',
});

export const baseRequestClient = new RequestClient({ baseURL: apiURL });
