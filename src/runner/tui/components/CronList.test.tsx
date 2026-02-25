import React from 'react';
import { describe, it, expect } from 'bun:test';
import { render } from 'ink-testing-library';
import { CronList } from './CronList';
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
    runs: [],
    ...overrides,
  };
}

describe('CronList', () => {
  it('shows empty state when no cron entries exist', () => {
    const { lastFrame } = render(
      <CronList crons={[]} selectedId={null} />
    );
    expect(lastFrame()).toContain('No cron entries found');
  });

  it('renders cron entries and highlights selected row', () => {
    const crons = [
      createCron({ id: 'cron-1', title: 'Nightly cleanup' }),
      createCron({ id: 'cron-2', title: 'Hourly sync', schedule: '0 * * * *' }),
    ];
    const { lastFrame } = render(
      <CronList crons={crons} selectedId="cron-2" isFocused />
    );

    const frame = lastFrame() || '';
    expect(frame).toContain('Crons');
    expect(frame).toContain('Nightly cleanup');
    expect(frame).toContain('Hourly sync');
    expect(frame).toContain('>');
  });

  it('shows project prefix in multi-project aggregate mode', () => {
    const { lastFrame } = render(
      <CronList
        crons={[createCron({ projectId: 'brain-api' })]}
        selectedId="cron-1"
        showProjectPrefix
      />
    );

    expect(lastFrame()).toContain('[brain-api]');
  });

  it('shows bounded scheduling badges and run bounds info', () => {
    const { lastFrame } = render(
      <CronList
        crons={[
          createCron({
            max_runs: 1,
            attempts_used: 1,
            remaining_runs: 0,
            completed_reason: 'max_runs_reached',
            starts_at: '2026-02-23T00:00:00.000Z',
            window_starts_at_utc: '2026-02-23T00:00:00.000Z',
            window_expires_at_utc: '2026-02-24T00:00:00.000Z',
          }),
        ]}
        selectedId="cron-1"
      />
    );

    const frame = lastFrame() || '';
    expect(frame).toContain('one-shot');
    expect(frame).toContain('window');
    expect(frame).toContain('done:max_runs_reached');
    expect(frame).toContain('used/max: 1/1');
    expect(frame).toContain('left: 0');
  });
});
