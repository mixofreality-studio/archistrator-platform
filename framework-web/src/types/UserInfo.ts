/**
 * Represents an organization entry in the user's organization map.
 */
export interface OrganizationEntry {
  id: string;
  [key: string]: string | string[];
}

/**
 * Map of organization names to their details.
 */
export type OrganizationMap = Record<string, OrganizationEntry>;

export interface UserInfo {
  sub: string;
  email?: string;
  name?: string;
  preferred_username?: string;
  picture?: string;
  email_verified?: boolean;
  organization?: OrganizationMap;
}

/**
 * Extracts the first organization ID from the user's organization map.
 * Returns 'default' if no organizations are present.
 */
export function getFirstOrganizationId(user: UserInfo): string {
  if (user.organization === undefined) {
    return 'default';
  }

  const orgNames = Object.keys(user.organization);
  const firstName = orgNames[0];
  if (firstName === undefined) {
    return 'default';
  }

  const firstOrg = user.organization[firstName];
  return firstOrg?.id ?? 'default';
}
