package configgen

import "github.com/mixofreality-studio/archistrator-platform/framework-go-app-generator/internal/deploynaming"

// substrateCatalog is the MECHANISM half of the deployment split: it maps a
// declared infrastructure substrate to its ordered set of configuration
// INPUTS. Shared verbatim with composegen via internal/deploynaming — see its
// doc comment for the full rationale.
var substrateCatalog = deploynaming.SubstrateCatalog
