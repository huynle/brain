/**
 * FilterBar Component Tests
 *
 * Tests the filter bar display in three modes: off, typing, locked
 */

import React from 'react';
import { describe, it, expect } from 'bun:test';
import { render } from 'ink-testing-library';
import { FilterBar, type FilterMode } from './FilterBar';

describe('FilterBar', () => {
  const defaultProps = {
    filterText: 'auth',
    filterMode: 'off' as FilterMode,
    matchCount: 3,
    totalCount: 10,
  };

  describe('off mode', () => {
    it('renders nothing when filterMode is off', () => {
      const { lastFrame } = render(
        <FilterBar {...defaultProps} filterMode="off" />
      );
      // Should be empty or just whitespace
      expect(lastFrame()?.trim()).toBe('');
    });

    it('renders nothing even with filter text present', () => {
      const { lastFrame } = render(
        <FilterBar
          filterText="some filter"
          filterMode="off"
          matchCount={5}
          totalCount={20}
        />
      );
      expect(lastFrame()?.trim()).toBe('');
    });
  });

  describe('typing mode', () => {
    it('shows "/" search indicator', () => {
      const { lastFrame } = render(
        <FilterBar {...defaultProps} filterMode="typing" />
      );
      expect(lastFrame()).toContain('/');
    });

    it('displays the current filter text', () => {
      const { lastFrame } = render(
        <FilterBar {...defaultProps} filterMode="typing" filterText="task" />
      );
      expect(lastFrame()).toContain('task');
    });

    it('shows match count', () => {
      const { lastFrame } = render(
        <FilterBar {...defaultProps} filterMode="typing" matchCount={5} />
      );
      expect(lastFrame()).toContain('5 matches');
    });

    it('uses singular "match" for count of 1', () => {
      const { lastFrame } = render(
        <FilterBar {...defaultProps} filterMode="typing" matchCount={1} />
      );
      expect(lastFrame()).toContain('1 match');
      expect(lastFrame()).not.toContain('1 matches');
    });

    it('handles empty filter text', () => {
      const { lastFrame } = render(
        <FilterBar
          filterText=""
          filterMode="typing"
          matchCount={10}
          totalCount={10}
        />
      );
      expect(lastFrame()).toContain('/');
      expect(lastFrame()).toContain('10 matches');
    });

    it('handles zero matches', () => {
      const { lastFrame } = render(
        <FilterBar {...defaultProps} filterMode="typing" matchCount={0} />
      );
      expect(lastFrame()).toContain('0 matches');
    });
  });

  describe('locked mode', () => {
    it('shows "Filter:" label', () => {
      const { lastFrame } = render(
        <FilterBar {...defaultProps} filterMode="locked" />
      );
      expect(lastFrame()).toContain('Filter:');
    });

    it('displays the filter text', () => {
      const { lastFrame } = render(
        <FilterBar {...defaultProps} filterMode="locked" filterText="api" />
      );
      expect(lastFrame()).toContain('api');
    });

    it('shows match count and total count', () => {
      const { lastFrame } = render(
        <FilterBar
          filterText="test"
          filterMode="locked"
          matchCount={3}
          totalCount={10}
        />
      );
      expect(lastFrame()).toContain('3/10');
    });

    it('shows "Esc: clear" hint', () => {
      const { lastFrame } = render(
        <FilterBar {...defaultProps} filterMode="locked" />
      );
      expect(lastFrame()).toContain('Esc: clear');
    });

    it('handles zero matches in locked mode', () => {
      const { lastFrame } = render(
        <FilterBar
          filterText="nonexistent"
          filterMode="locked"
          matchCount={0}
          totalCount={10}
        />
      );
      expect(lastFrame()).toContain('0/10');
      expect(lastFrame()).toContain('Filter:');
    });
  });

  describe('edge cases', () => {
    it('handles special characters in filter text', () => {
      const { lastFrame } = render(
        <FilterBar
          filterText="test-*.tsx"
          filterMode="typing"
          matchCount={2}
          totalCount={10}
        />
      );
      expect(lastFrame()).toContain('test-*.tsx');
    });

    it('handles long filter text', () => {
      const longText = 'a'.repeat(50);
      const { lastFrame } = render(
        <FilterBar
          filterText={longText}
          filterMode="typing"
          matchCount={1}
          totalCount={100}
        />
      );
      expect(lastFrame()).toContain(longText);
    });

    it('handles large match counts', () => {
      const { lastFrame } = render(
        <FilterBar
          filterText="task"
          filterMode="locked"
          matchCount={999}
          totalCount={1000}
        />
      );
      expect(lastFrame()).toContain('999/1000');
    });
  });
});
