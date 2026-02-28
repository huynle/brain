/**
 * Bottom help bar showing keyboard shortcuts
 */

import React from 'react';
import { Box, Text } from 'ink';

interface HelpBarProps {
  focusedPanel?: 'tasks' | 'details' | 'logs';
  viewMode?: 'tasks' | 'schedules';
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
  const isScheduleView = viewMode === 'schedules';
  const focusLabel = isScheduleView
    ? (focusedPanel === 'tasks' ? 'schedules' : focusedPanel === 'details' ? 'schedule details' : focusedPanel)
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
               <Text bold>{isScheduleView ? 'j/k' : 'd'}</Text> {isScheduleView ? 'Scroll' : 'Dependencies'}
               {'  '}
             </>
           ) : (
             <>
               {!isScheduleView && (
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
                    <Text bold>s</Text> Meta/Feature
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
                    <Text bold>f</Text> Checkout
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
                 {isScheduleView && (
                   <>
                     <Text bold>Enter</Text> Details
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
