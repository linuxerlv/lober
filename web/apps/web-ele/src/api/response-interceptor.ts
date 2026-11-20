import type { RequestResponse, ResponseInterceptorConfig } from '@vben/request';

interface Envelope {
  code?: number;
  data?: unknown;
  message?: string;
}

function normalize<T extends object>(
  error: T,
  status?: number,
  body?: Envelope,
) {
  return Object.assign(error, {
    httpStatus: status,
    bizCode: body?.code,
    friendlyMessage: body?.message,
  });
}

export function statusCodeResponseInterceptor(): ResponseInterceptorConfig {
  return {
    fulfilled: (response: RequestResponse) => {
      const { config, data, status } = response;
      if (
        status < 200 ||
        status >= 300 ||
        (typeof data?.code === 'number' && data.code !== 0)
      ) {
        throw normalize(
          Object.assign(new Error(data?.message || 'Request failed'), {
            response,
          }),
          status,
          data,
        );
      }
      if (config.responseReturn === 'raw') return response;
      if (config.responseReturn === 'body') return data;
      return data?.data;
    },
  };
}

export function normalizeErrorResponseInterceptor(): ResponseInterceptorConfig {
  return {
    rejected: async (error) => {
      const body = error?.response?.data;
      if (body instanceof Blob && body.type.includes('json')) {
        try {
          error.response.data = JSON.parse(await body.text());
        } catch {
          /* Keep unreadable bodies for callers. */
        }
      }
      if (error?.response)
        normalize(error, error.response.status, error.response.data);
      throw error;
    },
  };
}
