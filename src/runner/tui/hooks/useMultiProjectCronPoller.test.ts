import { describe, it, expect, beforeEach } from 'bun:test';

describe('useMultiProjectCronPoller - reducer', () => {
  let multiProjectCronReducer: typeof import('./useMultiProjectCronPoller').multiProjectCronReducer;

  beforeEach(async () => {
    const mod = await import('./useMultiProjectCronPoller');
    multiProjectCronReducer = mod.multiProjectCronReducer;
  });

  const initialState = {
    cronsByProject: new Map<string, any[]>(),
    connectionByProject: new Map<string, boolean>(),
    errorsByProject: new Map<string, Error>(),
    isLoading: true,
  };

  it('handles FETCH_START', () => {
    const result = multiProjectCronReducer({ ...initialState, isLoading: false }, { type: 'FETCH_START' as const });
    expect(result.isLoading).toBe(true);
  });

  it('handles FETCH_SUCCESS', () => {
    const cronsByProject = new Map([
      ['p1', [{ id: 'c1', path: 'projects/p1/cron/c1.md', title: 'Nightly', schedule: '0 2 * * *', status: 'active' as const }]],
    ]);
    const connectionByProject = new Map([['p1', true]]);
    const errorsByProject = new Map<string, Error>();

    const result = multiProjectCronReducer(initialState, {
      type: 'FETCH_SUCCESS' as const,
      cronsByProject,
      connectionByProject,
      errorsByProject,
    });

    expect(result.cronsByProject).toBe(cronsByProject);
    expect(result.connectionByProject).toBe(connectionByProject);
    expect(result.errorsByProject).toBe(errorsByProject);
    expect(result.isLoading).toBe(false);
  });
});

describe('useMultiProjectCronPoller - helpers', () => {
  let mergeAllCrons: typeof import('./useMultiProjectCronPoller').mergeAllCrons;
  let checkAnyConnected: typeof import('./useMultiProjectCronPoller').checkAnyConnected;
  let getFirstError: typeof import('./useMultiProjectCronPoller').getFirstError;

  beforeEach(async () => {
    const mod = await import('./useMultiProjectCronPoller');
    mergeAllCrons = mod.mergeAllCrons;
    checkAnyConnected = mod.checkAnyConnected;
    getFirstError = mod.getFirstError;
  });

  it('merges all cron entries across projects', () => {
    const cronsByProject = new Map([
      ['p1', [{ id: 'c1', path: 'projects/p1/cron/c1.md', title: 'Nightly', schedule: '0 2 * * *', status: 'active' as const }]],
      ['p2', [{ id: 'c2', path: 'projects/p2/cron/c2.md', title: 'Hourly', schedule: '0 * * * *', status: 'pending' as const }]],
    ]);

    const result = mergeAllCrons(cronsByProject as any);
    expect(result).toHaveLength(2);
  });

  it('reports connected when one project is connected', () => {
    const connectionByProject = new Map([
      ['p1', false],
      ['p2', true],
    ]);

    expect(checkAnyConnected(connectionByProject)).toBe(true);
  });

  it('returns null first error for empty map', () => {
    expect(getFirstError(new Map())).toBeNull();
  });
});
