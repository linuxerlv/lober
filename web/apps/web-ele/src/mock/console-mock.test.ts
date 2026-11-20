import type { IncomingMessage, ServerResponse } from 'node:http';

import { Buffer } from 'node:buffer';
import { EventEmitter } from 'node:events';

import { describe, expect, it } from 'vitest';

import { handleConsoleMockRequest } from './console-mock';

async function callMock(method: string, url: string, body?: unknown, username = 'admin') {
  const req = new EventEmitter() as EventEmitter & IncomingMessage;
  req.method = method;
  req.url = url;
  req.headers = { authorization: `Bearer mock-user-${username}` };

  let statusCode = 200;
  let responseBody = '';
  const headers: Record<string, string> = {};
  const res = {
    get statusCode() {
      return statusCode;
    },
    set statusCode(val: number) {
      statusCode = val;
    },
    setHeader(key: string, val: string) {
      headers[key.toLowerCase()] = val;
    },
    end(chunk?: string) {
      responseBody += chunk ?? '';
    },
  } as unknown as ServerResponse;

  const promise = handleConsoleMockRequest(req, res);
  if (body !== undefined) {
    req.emit('data', Buffer.from(JSON.stringify(body), 'utf8'));
  }
  req.emit('end');
  const handled = await promise;
  return {
    handled,
    statusCode,
    headers,
    json: responseBody ? JSON.parse(responseBody) : null,
  };
}

describe('console mock server', () => {
  it('applies role limits and revision checks to page grants', async () => {
    const path = '/console/v1/roles/viewer/menus';
    const roles = await callMock('GET', '/console/v1/roles/menus');
    const viewer = roles.json.data.roles.find((item: { role: string }) => item.role === 'viewer');
    expect(viewer.availableMenuIds).toContain('overview-workspace');
    expect((await callMock('GET', '/console/v1/roles/menus', undefined, 'chenxun')).statusCode).toBe(403);
    expect((await callMock('PUT', path, { menuIds: [], rev: viewer.rev }, 'chenxun')).statusCode).toBe(403);
    expect((await callMock('PUT', path, { menuIds: ['reports'], rev: viewer.rev })).statusCode).toBe(400);
    const saved = await callMock('PUT', path, { menuIds: ['overview-workspace'], rev: viewer.rev });
    expect(saved.json.data.menuIds).toEqual(['overview-workspace']);
    try {
      expect((await callMock('PUT', path, { menuIds: [], rev: viewer.rev })).statusCode).toBe(409);
      const session = await callMock('GET', '/console/v1/user/info', undefined, 'chenxun');
      expect(session.json.data.menuIds).toEqual(['overview-workspace']);
    } finally {
      await callMock('PUT', path, { menuIds: viewer.menuIds, rev: saved.json.data.rev });
    }
  });
  it('returns session user and supports switching demo roles', async () => {
    const session = await callMock('GET', '/console/v1/session');
    expect(session.handled).toBe(true);
    expect(session.json.code).toBe(0);
    expect(session.json.data.role).toBe('admin');

    const switched = await callMock('POST', '/console/v1/session/switch-role', {
      role: 'operator',
    });
    expect(switched.json.data.user.role).toBe('operator');
  });

  it('serves tasks, hosts, wizard disks, templates, hooks and audit records', async () => {
    const tasksRes = await callMock('GET', '/console/v1/tasks?page=1&page_size=20');
    expect(tasksRes.json.data.items.length).toBeGreaterThan(0);

    const hostsRes = await callMock('GET', '/console/v1/hosts?page=1&page_size=20');
    expect(hostsRes.json.data.items.length).toBeGreaterThan(0);

    const disksRes = await callMock(
      'GET',
      '/console/v1/hosts/SRV-BJ-A01-01/disks',
    );
    expect(disksRes.json.data.status).toBe('ready');
    expect(disksRes.json.data.disks[0].device).toBe('nvme0n1');

    const tplRes = await callMock('GET', '/console/v1/templates?page=1&page_size=20');
    expect(tplRes.json.data.items.length).toBeGreaterThan(0);

    const auditRes = await callMock('GET', '/console/v1/audit?page=1&page_size=20');
    expect(auditRes.json.data.items.length).toBeGreaterThan(0);
  });
});
