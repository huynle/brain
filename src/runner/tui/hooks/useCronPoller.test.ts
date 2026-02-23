import { describe, it, expect, beforeEach } from 'bun:test';

describe('useCronPoller - reducer', () => {
  let cronPollerReducer: typeof import('./useCronPoller').cronPollerReducer;

  beforeEach(async () => {
    const mod = await import('./useCronPoller');
    cronPollerReducer = mod.cronPollerReducer;
  });

  const initialState = {
    crons: [],
    isLoading: true,
    isConnected: false,
    error: null,
  };

  it('handles FETCH_START', () => {
    const result = cronPollerReducer({ ...initialState, isLoading: false }, { type: 'FETCH_START' as const });
    expect(result.isLoading).toBe(true);
    expect(result.isConnected).toBe(false);
  });

  it('handles FETCH_SUCCESS', () => {
    const crons = [
      {
        id: 'crn00001',
        path: 'projects/p1/cron/crn00001.md',
        title: 'Nightly',
        schedule: '0 2 * * *',
        status: 'active' as const,
      },
    ];

    const result = cronPollerReducer(initialState, {
      type: 'FETCH_SUCCESS' as const,
      crons,
    });

    expect(result.crons).toEqual(crons);
    expect(result.isLoading).toBe(false);
    expect(result.isConnected).toBe(true);
    expect(result.error).toBeNull();
  });

  it('handles FETCH_ERROR and preserves stale crons', () => {
    const stateWithData = {
      crons: [
        {
          id: 'crn00001',
          path: 'projects/p1/cron/crn00001.md',
          title: 'Nightly',
          schedule: '0 2 * * *',
          status: 'active' as const,
        },
      ],
      isLoading: false,
      isConnected: true,
      error: null,
    };

    const error = new Error('network failed');
    const result = cronPollerReducer(stateWithData, { type: 'FETCH_ERROR' as const, error });

    expect(result.crons).toEqual(stateWithData.crons);
    expect(result.isLoading).toBe(false);
    expect(result.isConnected).toBe(false);
    expect(result.error).toBe(error);
  });
});
