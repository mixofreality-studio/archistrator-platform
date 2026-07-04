import { client } from '../api/client';
import type { Id } from '../contracts/types';
export function useThing(): Id { return client.id; }
