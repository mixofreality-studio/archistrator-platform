# greenfield conformance fixture

Provenance: `testdata/greenfield.project.json`, the generator golden fixture (an
order/fulfillment app with a Postgres-backed order-state RA). It was written to pin
generator output, not to build, so the chain (scaffold, gen-null, gen-models, appgen,
gen-tests, go build, go vet) needs these edits to the *fixture data*. Each is a
hand-written dependency of the original golden app that no generator produces:

- `orderManager` contract: the `repo` dep was typed
  `func(orderID string) (repolookup.RepoRef, bool)` with `goImport`
  `example.com/greenfield/server/internal/resourceaccess/repolookup`, a hand-written
  package of the original app. Now `func(orderID string) (string, bool)` with no
  `goImport`.
- `orderStateAccess` contract: `ReceiptForOrder` returned the opaque
  `x-go-type: "OrderReceipt"` (a hand-written type). Now `$ref` to a new
  `OrderReceipt` object in the contract's `$defs` (`OrderID`, `AmountCents`).
- Deployment model: `orderStateAccess` bound the `local` profile to a `memory`
  variant, whose constructor `orderstate.NewMemoryAccess` is hand-written. Now the
  `local` profile binds the `postgres` variant, and the `postgres` infrastructure
  entry lists the `local` profile, so the generated launcher provisions it.

The fixture has no browser surface, so the chain runs no web steps for it.
