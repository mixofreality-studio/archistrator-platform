import { Home } from '../routes/Home';
import { client } from '../api/client';
export function boot(): unknown { return [Home(), client]; }
