/**
 * UserProfile component displays user avatar and menu with sign out option.
 *
 * Differences from the gtd-specific version:
 * - Navigation callbacks (onCreateOrg, onSelectOrganization) are props instead
 *   of react-router-dom `useNavigate()` calls, removing the router dependency.
 * - `accountUrl` is a prop instead of a hardcoded Keycloak/GTD URL.
 * - UI test-id strings are inlined from the `User` namespace of UIIdentifiers
 *   rather than imported from the gtd constants file.
 */

import { useState, type MouseEvent, type ReactNode } from 'react';
import {
  IconButton,
  Avatar,
  Menu,
  MenuItem,
  ListItemIcon,
  ListItemText,
  Divider,
  Typography,
  Dialog,
  DialogTitle,
  DialogContent,
  List,
  ListItem,
  ListItemButton,
  Box,
} from '@mui/material';
import {
  Logout as LogoutIcon,
  AccountCircle as AccountCircleIcon,
  SwapHoriz as SwapHorizIcon,
  Business as BusinessIcon,
  AddBusiness as AddBusinessIcon,
} from '@mui/icons-material';
import type { UserInfo } from '../types/UserInfo.js';

// Inline test-id strings (mirrors gtd UIIdentifiers.User namespace)
const USER_TEST_IDS = {
  PROFILE_BUTTON: 'user-profile-button',
  PROFILE_MENU: 'user-profile-menu',
  ACCOUNT_BUTTON: 'user-account-button',
  SWITCH_ORG_BUTTON: 'user-switch-org-button',
  CREATE_ORG_BUTTON: 'user-create-org-button',
  ORG_SWITCH_DIALOG: 'user-org-switch-dialog',
  orgSwitchOption: (orgId: string) => `user-org-switch-option-${orgId}`,
  LOGOUT_BUTTON: 'user-logout-button',
} as const;

export interface UserProfileProps {
  user: UserInfo;
  /**
   * URL to the identity-provider account management page.
   * Opened in a new tab when the user clicks their name/email in the menu.
   */
  accountUrl: string;
  /**
   * Called when the user clicks "Create Organization".
   * The host app is responsible for navigation.
   */
  onCreateOrg: () => void;
  /**
   * Called when the user selects an organization to switch to.
   * Receives the organization ID.
   */
  onSelectOrganization: (orgId: string) => void;
}

/**
 * Get user initials from name or email.
 * Returns up to 2 characters for the avatar.
 */
function getUserInitials(user: UserInfo): string {
  if (user.name !== undefined && user.name !== '') {
    const parts = user.name.trim().split(/\s+/);
    const firstPart = parts[0];
    const lastPart = parts[parts.length - 1];
    if (parts.length >= 2 && firstPart !== undefined && lastPart !== undefined) {
      const firstInitial = firstPart[0] ?? '';
      const lastInitial = lastPart[0] ?? '';
      return `${firstInitial}${lastInitial}`.toUpperCase();
    }
    return user.name.substring(0, 2).toUpperCase();
  }

  if (user.email !== undefined && user.email !== '') {
    return user.email.substring(0, 2).toUpperCase();
  }

  return 'U';
}

export function UserProfile({
  user,
  accountUrl,
  onCreateOrg,
  onSelectOrganization,
}: UserProfileProps): ReactNode {
  const [anchorEl, setAnchorEl] = useState<null | HTMLElement>(null);
  const [orgDialogOpen, setOrgDialogOpen] = useState(false);
  const open = Boolean(anchorEl);

  const handleClick = (event: MouseEvent<HTMLElement>): void => {
    setAnchorEl(event.currentTarget);
  };

  const handleClose = (): void => {
    setAnchorEl(null);
  };

  const handleSignOut = (): void => {
    window.location.href = '/logout';
  };

  const handleAccountClick = (): void => {
    window.open(accountUrl, '_blank', 'noopener,noreferrer');
    handleClose();
  };

  const handleSwitchOrgClick = (): void => {
    setOrgDialogOpen(true);
    handleClose();
  };

  const handleCreateOrgClick = (): void => {
    handleClose();
    onCreateOrg();
  };

  const handleOrgDialogClose = (): void => {
    setOrgDialogOpen(false);
  };

  const handleSelectOrganization = (orgId: string): void => {
    setOrgDialogOpen(false);
    onSelectOrganization(orgId);
  };

  const displayName = user.name ?? user.email ?? 'User';
  const displayEmail = user.email;
  const orgEntries = Object.entries(user.organization ?? {});
  const hasMultipleOrgs = orgEntries.length > 1;

  return (
    <>
      <IconButton
        aria-controls={open ? 'user-menu' : undefined}
        aria-expanded={open ? 'true' : undefined}
        aria-haspopup="true"
        aria-label="user profile"
        data-testid={USER_TEST_IDS.PROFILE_BUTTON}
        size="small"
        onClick={handleClick}
      >
        <Avatar alt={displayName} sx={{ width: 32, height: 32 }}>
          {getUserInitials(user)}
        </Avatar>
      </IconButton>

      <Menu
        anchorEl={anchorEl}
        anchorOrigin={{
          vertical: 'bottom',
          horizontal: 'right',
        }}
        data-testid={USER_TEST_IDS.PROFILE_MENU}
        id="user-menu"
        open={open}
        transformOrigin={{
          vertical: 'top',
          horizontal: 'right',
        }}
        onClose={handleClose}
      >
        <MenuItem
          data-testid={USER_TEST_IDS.ACCOUNT_BUTTON}
          onClick={handleAccountClick}
        >
          <ListItemIcon>
            <AccountCircleIcon fontSize="small" />
          </ListItemIcon>
          <ListItemText
            primary={displayName}
            secondary={displayEmail}
            slotProps={{
              primary: { variant: 'body2', fontWeight: 'medium' },
              secondary: { variant: 'caption' },
            }}
          />
        </MenuItem>
        <Divider />
        {hasMultipleOrgs ? <MenuItem
            data-testid={USER_TEST_IDS.SWITCH_ORG_BUTTON}
            onClick={handleSwitchOrgClick}
          >
            <ListItemIcon>
              <SwapHorizIcon fontSize="small" />
            </ListItemIcon>
            <ListItemText>
              <Typography variant="body2">Switch Organization</Typography>
            </ListItemText>
          </MenuItem> : null}
        <MenuItem
          data-testid={USER_TEST_IDS.CREATE_ORG_BUTTON}
          onClick={handleCreateOrgClick}
        >
          <ListItemIcon>
            <AddBusinessIcon fontSize="small" />
          </ListItemIcon>
          <ListItemText>
            <Typography variant="body2">Create Organization</Typography>
          </ListItemText>
        </MenuItem>
        <MenuItem data-testid={USER_TEST_IDS.LOGOUT_BUTTON} onClick={handleSignOut}>
          <ListItemIcon>
            <LogoutIcon fontSize="small" />
          </ListItemIcon>
          <ListItemText>
            <Typography variant="body2">Sign Out</Typography>
          </ListItemText>
        </MenuItem>
      </Menu>

      <Dialog
        data-testid={USER_TEST_IDS.ORG_SWITCH_DIALOG}
        open={orgDialogOpen}
        onClose={handleOrgDialogClose}
      >
        <DialogTitle>Switch Organization</DialogTitle>
        <DialogContent>
          <List sx={{ minWidth: 300 }}>
            {orgEntries.map(([orgName, orgDetails]) => (
              <ListItem disablePadding key={orgDetails.id}>
                <ListItemButton
                  data-testid={USER_TEST_IDS.orgSwitchOption(orgDetails.id)}
                  onClick={() => {
                    handleSelectOrganization(orgDetails.id);
                  }}
                >
                  <ListItemIcon>
                    <BusinessIcon color="primary" />
                  </ListItemIcon>
                  <Box sx={{ flexGrow: 1 }}>
                    <Typography variant="body1">{orgName}</Typography>
                  </Box>
                </ListItemButton>
              </ListItem>
            ))}
          </List>
        </DialogContent>
      </Dialog>
    </>
  );
}
