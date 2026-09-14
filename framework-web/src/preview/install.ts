/**
 * Side-effect entry (`@mixofreality-studio/archistrator-platform-framework-web/preview/install`):
 * installs the preview's network and navigation guards. It MUST be the preview
 * entry's first import, so both are in place before any other module evaluates.
 */
import { installPreviewGuards } from './installPreviewGuards.ts';

installPreviewGuards();
