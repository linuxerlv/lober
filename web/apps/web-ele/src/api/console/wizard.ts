import type { Schemas } from './generated';

import { requestClient } from '#/api/request';

export const wizardAPI = {
  hosts: (params: Record<string, number | string> = {}) =>
    requestClient.get<Schemas['WizardHostPage']>('/wizard/hosts', { params }),
  isos: (data: Schemas['WizardISORequest']) =>
    requestClient.post<Schemas['WizardISOResult']>('/wizard/isos', data),
  templates: (data: Schemas['WizardTemplateRequest']) =>
    requestClient.post<Schemas['WizardTemplateResult']>('/wizard/templates', data),
  params: (data: Schemas['WizardParamsRequest']) =>
    requestClient.post<Schemas['WizardParamsResult']>('/wizard/params', data),
  submit: (data: Schemas['WizardSubmitRequest']) =>
    requestClient.post<Schemas['WizardSubmitResult']>('/wizard/tasks', data),
  preview: (id: string, version: number, data: Schemas['PreviewRequest']) =>
    requestClient.post<Schemas['PreviewResponse']>(
      `/templates/${encodeURIComponent(id)}/versions/${version}/preview`,
      data,
    ),
};
