import { useThing } from '../hooks/useThing';
import type { Id } from '../contracts/types';
export function Card(): Id { return useThing(); }
