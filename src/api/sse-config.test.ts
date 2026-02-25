import { describe, expect, it } from 'bun:test';
import {
  TASK_SSE_HEARTBEAT_MS,
  TASK_SSE_SAFE_IDLE_TIMEOUT_SECONDS,
} from './sse-config';

describe('SSE timing configuration', () => {
  it('keeps server idle timeout safely above task heartbeat', () => {
    const timeoutMs = TASK_SSE_SAFE_IDLE_TIMEOUT_SECONDS * 1000;

    expect(timeoutMs).toBeGreaterThan(TASK_SSE_HEARTBEAT_MS);
    expect(timeoutMs - TASK_SSE_HEARTBEAT_MS).toBeGreaterThanOrEqual(10000);
  });
});
