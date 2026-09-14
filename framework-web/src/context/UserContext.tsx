/**
 * UserContext provides user authentication information to the application.
 * Probes the session on mount and provides the user via context. The server
 * returns mock user data in local mode.
 *
 * The probe defaults to GET /api/userinfo over fetch. An app that routes every
 * request through one transport seam injects its own `fetchUser` instead (the
 * webgen OpsClient's composition route compositionGetUserinfo), so the probe
 * rides that seam and a preview's fixture transport can answer it
 * (design-renderer-data.md §2′.0 P2). Either way, a rejection whose `status` is
 * 401 is announced (announceUnauthenticated) and then reloads the page so the
 * edge issues the OIDC redirect, unless a listener claimed it: a preview does,
 * because it cannot sign in and a reload would loop over the same fixture.
 */

import { useState, useEffect, type ReactNode } from 'react';
import { Box, CircularProgress, Alert, Button, Typography } from '@mui/material';
import type { UserInfo } from '../types/UserInfo.js';
import { UserContext } from './UserContextDefinition.js';
import { announceUnauthenticated } from './sessionEvents.js';

interface UserProviderProps {
  children: ReactNode;
  /**
   * The session probe: resolves the user, or rejects. A rejection with a numeric
   * `status` of 401 (an ApiError, say) reloads the page, unless the announced
   * 401 was claimed (see sessionEvents.ts). Defaults to GET /api/userinfo.
   */
  fetchUser?: () => Promise<UserInfo>;
}

/** A non-2xx answer to the default probe. */
class UserInfoFetchError extends Error {
  readonly status: number;

  constructor(status: number, message: string) {
    super(message);
    this.status = status;
  }
}

async function fetchUserInfoOverHttp(): Promise<UserInfo> {
  const response = await fetch('/api/userinfo', {
    headers: { 'Accept': 'application/json' },
  });
  if (!response.ok) {
    throw new UserInfoFetchError(
      response.status,
      `Failed to fetch user info: ${response.status.toString()} ${response.statusText}`
    );
  }
  return (await response.json()) as UserInfo;
}

function statusOf(err: unknown): number | undefined {
  if (typeof err !== 'object' || err === null || !('status' in err)) return undefined;
  return typeof err.status === 'number' ? err.status : undefined;
}

export function UserProvider({ children, fetchUser }: UserProviderProps): ReactNode {
  const [user, setUser] = useState<UserInfo | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const probe = fetchUser ?? fetchUserInfoOverHttp;

  const load = async (): Promise<void> => {
    setLoading(true);
    setError(null);

    try {
      setUser(await probe());
    } catch (err) {
      if (statusOf(err) === 401) {
        // Session expired or not authenticated: reload to trigger the Envoy OIDC
        // redirect, unless a listener (a preview) claimed the 401.
        if (announceUnauthenticated()) {
          window.location.reload();
          return;
        }
        setError('Not signed in (401), and this page cannot sign in.');
        return;
      }
      const errorMessage = err instanceof Error ? err.message : 'Unknown error occurred';
      setError(errorMessage);
      console.error('Error fetching user info:', err);
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => {
    void load();
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
              void load();
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
