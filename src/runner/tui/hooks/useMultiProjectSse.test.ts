import { describe, expect, it } from 'bun:test';
import { multiProjectSseReducer } from './useMultiProjectSse';

const emptyState = {
  tasksByProject: new Map(),
  statsByProject: new Map(),
  connectionByProject: new Map(),
  errorsByProject: new Map(),
  isLoading: true,
};

describe('useMultiProjectSse reducer', () => {
  it('suppresses transient transport errors before a project is connected', () => {
    const result = multiProjectSseReducer(emptyState, {
      type: 'PROJECT_CONNECTION_ERROR',
      projectId: 'p1',
      error: new Error('SSE connection error'),
    });

    expect(result).toEqual(emptyState);
  });

  it('marks project disconnected without error churn on transient reconnects', () => {
    const connected = {
      ...emptyState,
      isLoading: false,
      connectionByProject: new Map([['p1', true]]),
    };

    const result = multiProjectSseReducer(connected, {
      type: 'PROJECT_CONNECTION_ERROR',
      projectId: 'p1',
      error: new Error('SSE connection error'),
    });

    expect(result.connectionByProject.get('p1')).toBe(false);
    expect(result.errorsByProject.has('p1')).toBe(false);
  });

  it('surfaces non-transient project connection errors', () => {
    const result = multiProjectSseReducer(emptyState, {
      type: 'PROJECT_CONNECTION_ERROR',
      projectId: 'p1',
      error: new Error('SSE stream reported an error'),
    });

    expect(result.errorsByProject.get('p1')?.message).toBe('SSE stream reported an error');
  });
});
