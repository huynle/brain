import React from 'react';
import { describe, it, expect } from 'bun:test';
import { render } from 'ink-testing-library';
import { CronDetail } from './CronDetail';
import type { CronDisplay } from '../types';

function createCron(overrides: Partial<CronDisplay> = {}): CronDisplay {
  return {
    id: 'cron-1',
    path: 'projects/test/cron/cron-1.md',
    title: 'Nightly cleanup',
    projectId: 'test',
    schedule: '0 2 * * *',
    status: 'active',
    next_run: '2026-02-24T02:00:00.000Z',
    runs: [
      {
        run_id: 'run-1',
        status: 'completed',
        started: '2026-02-23T02:00:00.000Z',
        completed: '2026-02-23T02:01:00.000Z',
        duration: 60000,
      },
    ],
    ...overrides,
  };
}

describe('CronDetail', () => {
  it('shows placeholder when no cron is selected', () => {
    const { lastFrame } = render(<CronDetail cron={null} />);
    expect(lastFrame()).toContain('Select a cron entry to view run history');
  });

  it('shows cron metadata and run history', () => {
    const { lastFrame } = render(<CronDetail cron={createCron()} isFocused />);
    const frame = lastFrame() || '';

    expect(frame).toContain('Cron Details');
    expect(frame).toContain('Nightly cleanup');
    expect(frame).toContain('Schedule:');
    expect(frame).toContain('Run History');
    expect(frame).toContain('run-1');
    expect(frame).toContain('completed');
  });

  it('shows empty run history text when no runs exist', () => {
    const { lastFrame } = render(
      <CronDetail cron={createCron({ runs: [] })} />
    );
    expect(lastFrame()).toContain('No runs recorded');
  });
});
