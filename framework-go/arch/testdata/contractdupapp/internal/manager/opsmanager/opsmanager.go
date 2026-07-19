package opsmanager

import "context"

// durableExecutionAccess is a LEGIT narrow accepted-interface seam over the
// durable-execution substrate — modeled directly on the real app's
// operationsmanager/billingmanager seam (see recon B.5). It intentionally
// narrows to 1 of the generated durableexecution.DurableExecutionAccess
// contract's 4 operations, AND uses a LOCAL MIRROR scheduleSpec type (not the
// generated ScheduleSpec) plus stdlib context.Context (not the generated
// Context). This must trip NEITHER rule c (unexported, and there is no
// generated contract in this package at all) NOR rule d (it fails both
// name-set equality [1 method vs. the foreign contract's 4] and per-method
// signature equality [context.Context/scheduleSpec vs. Context/ScheduleSpec]
// against durableexecution.DurableExecutionAccess).
type durableExecutionAccess interface {
	RegisterSchedule(ctx context.Context, spec scheduleSpec) error
}

// scheduleSpec is the manager's own local mirror of the generated contract's
// ScheduleSpec — deliberately a DIFFERENT type, which is what defeats
// signature equality.
type scheduleSpec struct{ Cron string }
