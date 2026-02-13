/**
 * FilterBar component for task filtering
 *
 * Displays filter input in three modes:
 * - off: renders nothing
 * - typing: active input with vim-style "/" search appearance
 * - locked: persistent filter badge showing active filter
 */

import React from 'react';
import { Box, Text } from 'ink';

export type FilterMode = 'off' | 'typing' | 'locked';

export interface FilterBarProps {
  /** Current filter text */
  filterText: string;
  /** Current filter mode */
  filterMode: FilterMode;
  /** Number of tasks matching the filter */
  matchCount: number;
  /** Total number of tasks */
  totalCount: number;
}

export const FilterBar = React.memo(function FilterBar({
  filterText,
  filterMode,
  matchCount,
  totalCount,
}: FilterBarProps): React.ReactElement | null {
  // Off mode: render nothing
  if (filterMode === 'off') {
    return null;
  }

  // Typing mode: vim-style "/" search with active input appearance
  if (filterMode === 'typing') {
    return (
      <Box paddingX={1}>
        <Text backgroundColor="yellow" color="black" bold>
          {' / '}
        </Text>
        <Text backgroundColor="yellow" color="black">
          {filterText}
        </Text>
        <Text backgroundColor="white" color="black">
          {' '}
        </Text>
        <Text dimColor>
          {' '}({matchCount} match{matchCount !== 1 ? 'es' : ''})
        </Text>
      </Box>
    );
  }

  // Locked mode: filter badge with clear hint
  return (
    <Box paddingX={1} justifyContent="space-between">
      <Box>
        <Text backgroundColor="cyan" color="black" bold>
          {' Filter: '}
        </Text>
        <Text backgroundColor="cyan" color="black">
          {filterText}{' '}
        </Text>
        <Text>
          {' '}({matchCount}/{totalCount})
        </Text>
      </Box>
      <Text dimColor>Esc: clear</Text>
    </Box>
  );
});

export default FilterBar;
