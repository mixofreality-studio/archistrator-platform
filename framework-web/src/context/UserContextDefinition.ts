/**
 * User Context Definition
 * Separate file for context to satisfy react-refresh/only-export-components
 */

import { createContext } from 'react';
import type { UserInfo } from '../types/UserInfo.js';

export interface UserContextValue {
  user: UserInfo;
}

export const UserContext = createContext<UserContextValue | undefined>(undefined);
