import { describe, expect, it } from 'bun:test';
import { taskSseReducer } from './useTaskSse';

const initialState = {
  tasks: [],
  stats: {
    total: 0,
    ready: 0,
    waiting: 0,
    blocked: 0,
    inProgress: 0,
    completed: 0,
  },
  isLoading: true,
  isConnected: false,
  error: null,
};

describe('useTaskSse reducer', () => {
  it('suppresses transient transport error before first healthy snapshot', () => {
    const result = taskSseReducer(initialState, {
      type: 'CONNECTION_ERROR',
      error: new Error('SSE connection error'),
    });

    expect(result).toEqual(initialState);
  });

  it('marks disconnected without surfacing transient transport errors after connected', () => {
    const connectedState = {
      ...initialState,
      isLoading: false,
      isConnected: true,
      tasks: [{ id: 't1' } as any],
    };

    const result = taskSseReducer(connectedState, {
      type: 'CONNECTION_ERROR',
      error: new Error('SSE connection error'),
    });

    expect(result.isConnected).toBe(false);
    expect(result.error).toBeNull();
    expect(result.tasks).toEqual(connectedState.tasks);
  });

  it('surfaces non-transient connection errors', () => {
    const result = taskSseReducer(initialState, {
      type: 'CONNECTION_ERROR',
      error: new Error('SSE stream reported an error'),
    });

    expect(result.isConnected).toBe(false);
    expect(result.error?.message).toBe('SSE stream reported an error');
  });
});
