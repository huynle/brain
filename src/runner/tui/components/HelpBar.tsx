/**
 * Bottom help bar showing keyboard shortcuts
 */

import React from 'react';
import { Box, Text } from 'ink';

interface HelpBarProps {
  focusedPanel?: 'tasks' | 'details' | 'logs';
  viewMode?: 'tasks' | 'crons';
  /** Whether in multi-project mode (shows tab shortcuts) */
  isMultiProject?: boolean;
  /** Whether filter is currently active (locked in) */
  isFilterActive?: boolean;
  /** Whether tasks are currently selected (shows delete shortcut) */
  hasSelectedTasks?: boolean;
  /** Whether selected task has sessions (shows 'o Session' shortcut) */
  hasTaskSessions?: boolean;
  /** Whether text wrapping is enabled (false = truncate mode) */
  textWrap?: boolean;
}

export const HelpBar = React.memo(function HelpBar({ focusedPanel, viewMode = 'tasks', isMultiProject, isFilterActive, hasSelectedTasks, hasTaskSessions, textWrap }: HelpBarProps): React.ReactElement {
  // Show different hints based on which panel is focused
  const isLogsFocused = focusedPanel === 'logs';
  const isDetailsFocused = focusedPanel === 'details';
  const isCronView = viewMode === 'crons';
  const focusLabel = isCronView
    ? (focusedPanel === 'tasks' ? 'crons' : focusedPanel === 'details' ? 'cron details' : focusedPanel)
    : focusedPanel;
  
  return (
    <Box paddingX={1} justifyContent="space-between">
      <Box>
        <Text dimColor>
          {isMultiProject && (
            <>
              <Text bold>h/l/[/]/1-9</Text> Tabs
              {'  '}
            </>
          )}
          <Text bold>j/k</Text> {isLogsFocused || isDetailsFocused ? 'Scroll' : 'Navigate'}
          {'  '}
          <Text bold>g/G</Text> Top/Bottom
          {'  '}
           {isLogsFocused ? (
             <>
               <Text bold>f</Text> Filter
               {'  '}
             </>
           ) : isDetailsFocused ? (
             <>
               <Text bold>{isCronView ? 'j/k' : 'd'}</Text> {isCronView ? 'Scroll' : 'Dependencies'}
               {'  '}
             </>
           ) : (
             <>
               {!isCronView && (
                 <>
                   <Text bold>/</Text>{' '}
                   {isFilterActive ? (
                     <Text color="cyan">Filter*</Text>
                   ) : (
                     'Filter'
                   )}
                   {'  '}
                   <Text bold>Space</Text> Select
                   {'  '}
                   <Text bold>s</Text> Metadata
                   {'  '}
                   <Text bold>e</Text> Edit
                   {'  '}
                   {hasTaskSessions && (
                     <>
                       <Text bold>o</Text> Session
                       {'  '}
                       <Text bold>O</Text> Tmux
                       {'  '}
                     </>
                   )}
                   <Text bold>y</Text> Yank
                   {'  '}
                   <Text bold>x</Text> Execute
                   {'  '}
                   <Text bold>X</Text> Cancel
                   {'  '}
                   {hasSelectedTasks && (
                     <>
                       <Text bold color="red">⌫</Text> <Text color="red">Delete</Text>
                       {'  '}
                     </>
                   )}
                 </>
               )}
                {isCronView && (
                  <>
                    <Text bold>Enter</Text> Details
                    {'  '}
                 <Text bold>n/e</Text> New/Edit
                 {'  '}
                  <Text bold>x</Text> Trigger now
                  {'  '}
                  <Text bold>p</Text> Pause/Enable
                  {'  '}
                  <Text bold>a/u/R</Text> Edit links
                  {'  '}
                  <Text bold color="red">D</Text> <Text color="red">Delete</Text>
                 {'  '}
               </>
             )}
             </>
           )}
          {isMultiProject ? (
            <>
              <Text bold>p/P</Text> Pause (project/all)
              {'  '}
            </>
          ) : (
            <>
              <Text bold>p</Text> Pause (project)
              {'  '}
            </>
          )}
          <Text bold>w</Text> {textWrap ? 'Wrap' : 'Trunc'}
          {'  '}
           <Text bold>S</Text> Settings
           {'  '}
           <Text bold>C</Text> View
           {'  '}
           <Text bold>Tab</Text> Panel
           {'  '}
           <Text bold>L</Text> Logs
          {'  '}
          <Text bold>T</Text> Detail
          {'  '}
          <Text bold>r</Text> Refresh
          {'  '}
          <Text bold>Ctrl-C</Text> Quit
        </Text>
      </Box>
      {focusedPanel && (
        <Box>
          <Text dimColor>
            Focus: <Text color="cyan">{focusLabel}</Text>
          </Text>
        </Box>
      )}
    </Box>
  );
});

export default HelpBar;
