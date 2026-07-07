// Package orderstate is a minimal hand-written stand-in for the ResourceAccess
// contract package the generated Temporal wiring imports. It exists only to
// give the compile-proof sample (internal/sample/order) a real package to
// resolve orderstate.OrderStateAccess + its data types against. The method
// signatures mirror the greenfield fixture's orderStateAccess operations
// exactly (fwra.Context first param, matching business params/results), so the
// generated activities type-check against this interface.
package orderstate

import (
	fwra "github.com/mixofreality-studio/archistrator-platform/framework-go/resourceaccess"
)

// OrderID identifies an order.
type OrderID string

// Version is an optimistic-concurrency version stamp.
type Version int

// Order is the persisted order record.
type Order struct {
	ID          OrderID
	AmountCents int
	Status      string
	Version     Version
}

// OrderStateAccess is the order-state store contract. Every method takes the
// ResourceAccess call context first, as the layer requires.
type OrderStateAccess interface {
	ReadOrder(ctx fwra.Context, orderID OrderID) (Order, error)
	PutOrder(ctx fwra.Context, orderID OrderID, expectedVersion Version, order Order) (Version, error)
	CancelOrder(ctx fwra.Context, orderID OrderID, expectedVersion Version, reason string) (Version, error)
	ArchiveOrder(ctx fwra.Context, orderID OrderID, expectedVersion Version, idempotencyKey fwra.IdempotencyKey) (Version, error)
	ChargeOrder(ctx fwra.Context, orderID OrderID, amountCents int, idempotencyKey fwra.IdempotencyKey) (Version, error)
	PurgeOrder(ctx fwra.Context, orderID OrderID) error
}
