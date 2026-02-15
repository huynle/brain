/**
 * SessionSelectPopup - A popup for selecting which session to open
 *
 * Displays when a task has multiple session_ids and user presses the
 * session open shortcut. Lists sessions with j/k navigation.
 *
 * ┌─── Select Session ───────────────────┐
 * │                                      │
 * │  > ses_abc123def456                  │
 * │    ses_xyz789ghi012                  │
 * │    ses_mnp345qrs678                  │
 * │                                      │
 * │  j/k: Navigate  Enter: Open  Esc: Cancel
 * └──────────────────────────────────────┘
 */

import React from 'react';
import { Box, Text } from 'ink';

export interface SessionSelectPopupProps {
  /** List of session IDs to select from */
  sessionIds: string[];
  /** Currently selected index (0 = first/latest session) */
  selectedIndex: number;
}

/** Maximum length for session ID display */
const MAX_SESSION_ID_LENGTH = 30;

/** Maximum number of sessions to display before truncating */
const MAX_VISIBLE_SESSIONS = 8;

/**
 * Truncate a session ID if it's too long
 */
const truncateSessionId = (sessionId: string): string => {
  if (sessionId.length <= MAX_SESSION_ID_LENGTH) return sessionId;
  return sessionId.slice(0, MAX_SESSION_ID_LENGTH - 3) + '...';
};

export function SessionSelectPopup({
  sessionIds,
  selectedIndex,
}: SessionSelectPopupProps): React.ReactElement {
  const sessionCount = sessionIds.length;
  const visibleSessions = sessionIds.slice(0, MAX_VISIBLE_SESSIONS);
  const hiddenCount = sessionCount - visibleSessions.length;

  return (
    <Box
      flexDirection="column"
      borderStyle="round"
      borderColor="cyan"
      paddingX={2}
      paddingY={1}
    >
      {/* Header */}
      <Box marginBottom={1}>
        <Text bold color="cyan">Select Session</Text>
        <Text dimColor> ({sessionCount} session{sessionCount !== 1 ? 's' : ''})</Text>
      </Box>

      {/* Session list */}
      <Box flexDirection="column" marginBottom={1}>
        {visibleSessions.map((sessionId, index) => {
          const isSelected = index === selectedIndex;
          return (
            <Box key={sessionId}>
              <Text color={isSelected ? 'cyan' : undefined}>
                {isSelected ? '> ' : '  '}
              </Text>
              <Text
                bold={isSelected}
                color={isSelected ? 'cyan' : undefined}
              >
                {truncateSessionId(sessionId)}
              </Text>
              {index === 0 && (
                <Text dimColor> (latest)</Text>
              )}
            </Box>
          );
        })}
        {hiddenCount > 0 && (
          <Box>
            <Text dimColor>  ... and {hiddenCount} more</Text>
          </Box>
        )}
      </Box>

      {/* Footer with shortcuts */}
      <Box borderStyle="single" borderTop borderBottom={false} borderLeft={false} borderRight={false} borderColor="gray">
        <Text dimColor>j/k: Navigate  Enter: Open  Esc: Cancel</Text>
      </Box>
    </Box>
  );
}

export default SessionSelectPopup;
