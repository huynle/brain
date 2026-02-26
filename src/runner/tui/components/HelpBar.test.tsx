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

    unmount();
  });

  it('shows cron action shortcuts clearly in cron view', () => {
    const { lastFrame, unmount } = render(
      <HelpBar focusedPanel="tasks" viewMode="crons" />
    );

    const frame = lastFrame() || '';

    expect(frame).toContain('Enter');
    expect(frame).toContain('Details');
    expect(frame).toContain('n/e');
    expect(frame).toContain('New/Edit');
    expect(frame).toContain('x');
    expect(frame).toContain('Trigger now');
    expect(frame).toContain('p');
    expect(frame).toContain('Pause/Enable');
    expect(frame).toContain('a/u/R');
    expect(frame).toContain('Edit');
    expect(frame).toContain('links');
    expect(frame).toContain('D');
    expect(frame).toContain('Delete');

    unmount();
  });
});
