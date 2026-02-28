import React from 'react';
import { describe, it, expect } from 'bun:test';
import { render } from 'ink-testing-library';
import { HelpBar } from './HelpBar';

describe('HelpBar', () => {
  it('shows checkout shortcut hint in task view', () => {
    const { lastFrame, unmount } = render(
      <HelpBar focusedPanel="tasks" viewMode="tasks" />
    );

    const frame = lastFrame() || '';

    expect(frame).toContain('f');
    expect(frame).toContain('Checkout');
    expect(frame).toContain('s');
    expect(frame).toContain('Meta/Feature');

    unmount();
  });

  it('shows simplified shortcuts in schedule view', () => {
    const { lastFrame, unmount } = render(
      <HelpBar focusedPanel="tasks" viewMode="schedules" />
    );

    const frame = lastFrame() || '';

    // Schedule view should show Enter/Details
    expect(frame).toContain('Enter');
    expect(frame).toContain('Details');

    // Schedule view should NOT show old cron action shortcuts
    expect(frame).not.toContain('New/Edit');
    expect(frame).not.toContain('Trigger now');
    expect(frame).not.toContain('Edit links');
    expect(frame).not.toContain('Delete');

    // Schedule view should NOT show task-specific shortcuts
    expect(frame).not.toContain('Filter');
    expect(frame).not.toContain('Select');
    expect(frame).not.toContain('Meta/Feature');

    unmount();
  });
});
