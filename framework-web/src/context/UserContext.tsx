/**
 * UserContext provides user authentication information to the application.
 * Fetches user info from /api/userinfo on mount and provides it via context.
 * The server returns mock user data in local mode.
 */

import { useState, useEffect, type ReactNode } from 'react';
import { Box, CircularProgress, Alert, Button, Typography } from '@mui/material';
import type { UserInfo } from '../types/UserInfo.js';
import { UserContext } from './UserContextDefinition.js';

interface UserProviderProps {
  children: ReactNode;
}

export function UserProvider({ children }: UserProviderProps): ReactNode {
  const [user, setUser] = useState<UserInfo | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  const fetchUserInfo = async (): Promise<void> => {
    setLoading(true);
    setError(null);

    try {
      const response = await fetch('/api/userinfo', {
        headers: { 'Accept': 'application/json' },
      });

      if (!response.ok) {
        if (response.status === 401) {
          // Session expired or not authenticated — reload to trigger Envoy OIDC redirect
          window.location.reload();
          return;
        }
        throw new Error(
          `Failed to fetch user info: ${response.status.toString()} ${response.statusText}`
        );
      }

      const userData = (await response.json()) as UserInfo;
      setUser(userData);
    } catch (err) {
      const errorMessage = err instanceof Error ? err.message : 'Unknown error occurred';
      setError(errorMessage);
      console.error('Error fetching user info:', err);
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => {
    void fetchUserInfo();
  }, []);

  // Loading state - show centered spinner
  if (loading) {
    return (
      <Box
        sx={{
          display: 'flex',
          justifyContent: 'center',
          alignItems: 'center',
          minHeight: '100vh',
        }}
      >
        <CircularProgress />
      </Box>
    );
  }

  // Error state - show error alert with retry button
  if ((error !== null && error !== '') || user === null) {
    return (
      <Box
        sx={{
          display: 'flex',
          flexDirection: 'column',
          justifyContent: 'center',
          alignItems: 'center',
          minHeight: '100vh',
          gap: 2,
          p: 3,
        }}
      >
        <Alert severity="error" sx={{ maxWidth: 600 }}>
          <Typography gutterBottom variant="h6">
            Failed to Load User Information
          </Typography>
          <Typography sx={{ mb: 2 }} variant="body2">
            {error ?? 'User information not available'}
          </Typography>
          <Button
            variant="contained"
            onClick={() => {
              void fetchUserInfo();
            }}
          >
            Retry
          </Button>
        </Alert>
      </Box>
    );
  }

  // Success - provide user via context
  return <UserContext.Provider value={{ user }}>{children}</UserContext.Provider>;
}
