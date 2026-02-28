import React from 'react';
import { describe, it, expect } from 'bun:test';
import { render } from 'ink-testing-library';
import { ScheduleDetail } from './ScheduleDetail';
import type { TaskDisplay } from '../types';

function createScheduledTask(overrides: Partial<TaskDisplay> = {}): TaskDisplay {
  return {
    id: 'task-1',
    path: 'projects/test/task/task-1.md',
    title: 'Nightly cleanup',
    status: 'active',
    priority: 'medium',
    tags: ['maintenance'],
    schedule: '0 2 * * *',
    dependencies: [],
    dependents: [],
    dependencyTitles: [],
    dependentTitles: [],
    projectId: 'test',
    ...overrides,
  };
}

describe('ScheduleDetail', () => {
  it('shows placeholder when no task is selected', () => {
    const { lastFrame } = render(<ScheduleDetail task={null} />);
    expect(lastFrame()).toContain('Select a scheduled task to view details');
  });

  it('shows task metadata and schedule info', () => {
    const { lastFrame } = render(
      <ScheduleDetail task={createScheduledTask()} isFocused />
    );
    const frame = lastFrame() || '';

    expect(frame).toContain('Schedule Details');
    expect(frame).toContain('Nightly cleanup');
    expect(frame).toContain('task-1');
    expect(frame).toContain('Schedule:');
    expect(frame).toContain('0 2 * * *');
    expect(frame).toContain('Status:');
    expect(frame).toContain('Priority:');
    expect(frame).toContain('medium');
  });

  it('shows tags when present', () => {
    const { lastFrame } = render(
      <ScheduleDetail task={createScheduledTask({ tags: ['deploy', 'nightly'] })} />
    );
    const frame = lastFrame() || '';
    expect(frame).toContain('Tags:');
    expect(frame).toContain('deploy');
    expect(frame).toContain('nightly');
  });

  it('shows project and path info', () => {
    const { lastFrame } = render(
      <ScheduleDetail
        task={createScheduledTask({
          projectId: 'brain-api',
          path: 'projects/brain-api/task/abc123.md',
        })}
      />
    );

    const frame = lastFrame() || '';
    expect(frame).toContain('Project:');
    expect(frame).toContain('brain-api');
    expect(frame).toContain('Path:');
    expect(frame).toContain('projects/brain-api/task/abc123.md');
  });
});
