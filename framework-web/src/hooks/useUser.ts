/**
 * Hook to access the current user from context.
 */

import { useContext } from 'react';
import { UserContext } from '../context/UserContextDefinition.js';
import type { UserContextValue } from '../context/UserContextDefinition.js';

/**
 * Hook to access the current user from context.
 * Must be used within a UserProvider.
 */
export function useUser(): UserContextValue {
  const context = useContext(UserContext);

  if (context === undefined) {
    throw new Error('useUser must be used within a UserProvider');
  }

  return context;
}
