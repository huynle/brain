/**
 * Log viewer component for displaying streaming logs
 * Features: scroll support, color-coded levels, context display, performance optimization
 */

import React, { useMemo, useState, useEffect } from 'react';
import { Box, Text } from 'ink';
import type { LogEntry } from '../types';

interface LogViewerProps {
  logs: LogEntry[];
  maxLines?: number;
  showTimestamp?: boolean;
  showLevel?: boolean;
  /** When true, display [projectId] prefix for multi-project mode */
  showProjectPrefix?: boolean;
  /** Whether this panel is focused (enables scroll indicators) */
  isFocused?: boolean;
  /** Scroll offset from bottom (0 = latest logs, positive = scrolled up) */
  scrollOffset?: number;
}

const LEVEL_COLORS: Record<string, string> = {
  debug: 'gray',
  info: 'white',
  warn: 'yellow',
  error: 'red',
};

const LEVEL_LABELS: Record<string, string> = {
  debug: 'DEBUG',
  info: 'INFO',
  warn: 'WARN',
  error: 'ERROR',
};

// Maximum message length before truncation
const MAX_MESSAGE_LENGTH = 80;

/**
 * Format timestamp to HH:MM:SS
 */
function formatTime(date: Date): string {
  const pad = (n: number): string => n.toString().padStart(2, '0');
  return `${pad(date.getHours())}:${pad(date.getMinutes())}:${pad(date.getSeconds())}`;
}

/**
 * Format context as key="value" pairs
 */
function formatContext(context?: Record<string, unknown>): string {
  if (!context || Object.keys(context).length === 0) {
    return '';
  }

  return Object.entries(context)
    .map(([key, value]) => {
      const stringValue = typeof value === 'string' ? value : JSON.stringify(value);
      return `${key}="${stringValue}"`;
    })
    .join(' ');
}

/**
 * Truncate message with ellipsis if too long
 */
function truncateMessage(message: string, maxLength: number): string {
  if (message.length <= maxLength) {
    return message;
  }
  return message.slice(0, maxLength - 3) + '...';
}

/**
 * Single log line component (memoized to prevent unnecessary re-renders)
 */
export const LogLine = React.memo(function LogLine({
  log,
  showTimestamp,
  showLevel,
  showProjectPrefix,
}: {
  log: LogEntry;
  showTimestamp: boolean;
  showLevel: boolean;
  showProjectPrefix: boolean;
}): React.ReactElement {
  const levelColor = LEVEL_COLORS[log.level] || 'white';
  const levelLabel = LEVEL_LABELS[log.level] || log.level.toUpperCase();
  const contextStr = formatContext(log.context);
  
  // Project prefix for multi-project mode
  const projectPrefix = showProjectPrefix && log.projectId ? `[${log.projectId}] ` : '';
  
  // Calculate available space for message (account for project prefix)
  let availableLength = MAX_MESSAGE_LENGTH;
  if (projectPrefix) {
    availableLength = Math.max(30, availableLength - projectPrefix.length);
  }
  if (contextStr) {
    availableLength = Math.max(30, availableLength - contextStr.length - 1);
  }
  
  const truncatedMessage = truncateMessage(log.message, availableLength);

  return (
    <Box>
      {showTimestamp && (
        <Text dimColor>{formatTime(log.timestamp)} </Text>
      )}
      {showLevel && (
        <Text color={levelColor} bold={log.level === 'error'}>
          {levelLabel.padEnd(5)}{' '}
        </Text>
      )}
      {projectPrefix && (
        <Text color="cyan" dimColor>{projectPrefix}</Text>
      )}
      <Text color={log.level === 'debug' ? 'gray' : undefined}>
        {truncatedMessage}
      </Text>
      {contextStr && (
        <Text dimColor> {contextStr}</Text>
      )}
    </Box>
  );
});

export const LogViewer = React.memo(function LogViewer({
  logs,
  maxLines = 50,
  showTimestamp = true,
  showLevel = true,
  showProjectPrefix = false,
  isFocused = false,
  scrollOffset = 0,
}: LogViewerProps): React.ReactElement {
  // Calculate scroll indicators first (needed to adjust visible log count)
  // canScrollUp: there are logs above what we're showing
  // canScrollDown: scrollOffset > 0 means we've scrolled up from bottom
  const canScrollUp = scrollOffset < logs.length - maxLines;
  const canScrollDown = scrollOffset > 0;

  // Calculate visible logs with scroll support
  // When scroll indicators are shown, they take up lines inside the box
  // so we need to reduce the number of log entries displayed
  const visibleLogs = useMemo(() => {
    const totalLogs = logs.length;
    
    if (totalLogs === 0) return [];
    
    // Adjust maxLines to account for scroll indicator lines
    // Each indicator (up/down) takes 1 line when visible
    let adjustedMaxLines = maxLines;
    if (canScrollUp) adjustedMaxLines -= 1;
    if (canScrollDown) adjustedMaxLines -= 1;
    adjustedMaxLines = Math.max(1, adjustedMaxLines); // Always show at least 1 line
    
    // Calculate the end index based on scroll offset
    // scrollOffset=0 means showing the latest logs (bottom)
    // scrollOffset>0 means scrolled up by that many lines
    const endIndex = Math.max(0, totalLogs - scrollOffset);
    const startIndex = Math.max(0, endIndex - adjustedMaxLines);
    
    return logs.slice(startIndex, endIndex);
  }, [logs, maxLines, scrollOffset, canScrollUp, canScrollDown]);
  const isScrolled = scrollOffset > 0;

  // Header with scroll indicator
  const headerText = isScrolled 
    ? `Logs (${scrollOffset} lines below)`
    : 'Logs';

  return (
    <Box
      flexDirection="column"
      borderStyle="single"
      borderColor={isFocused ? 'cyan' : 'gray'}
      paddingX={1}
      width="100%"
      flexGrow={1}
    >
      {/* Header with scroll indicator */}
      <Box justifyContent="space-between">
        <Text bold dimColor={!isFocused} color={isFocused ? 'cyan' : undefined}>
          {headerText}
        </Text>
        {isFocused && (canScrollUp || canScrollDown) && (
          <Text dimColor>
            {canScrollUp ? '↑' : ' '}
            {canScrollDown ? '↓' : ' '}
          </Text>
        )}
      </Box>
      
      {/* Scroll up indicator */}
      {canScrollUp && (
        <Text dimColor color="gray">{'─'.repeat(20)} more above ↑</Text>
      )}
      
      {visibleLogs.length === 0 ? (
        <Text dimColor>No logs yet</Text>
      ) : (
        visibleLogs.map((log, index) => (
          <LogLine
            key={`log-${log.timestamp.getTime()}-${index}`}
            log={log}
            showTimestamp={showTimestamp}
            showLevel={showLevel}
            showProjectPrefix={showProjectPrefix}
          />
        ))
      )}
      
      {/* Scroll down indicator */}
      {canScrollDown && (
        <Text dimColor color="gray">{'─'.repeat(20)} more below ↓</Text>
      )}
    </Box>
  );
});

export default LogViewer;
