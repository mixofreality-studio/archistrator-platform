package durableexecution

// DurableExecutionAccess is the generated ResourceAccess port (contract
// surface) fronting the durable-execution substrate — modeled on the real
// app's 4-op contract.
type DurableExecutionAccess interface {
	RegisterSchedule(rc Context, id ScheduleID, spec ScheduleSpec) error
	CancelSchedule(rc Context, id ScheduleID) error
	DescribeSchedule(rc Context, id ScheduleID) (ScheduleDescription, error)
	ListSchedules(rc Context) ([]ScheduleID, error)
}

// Context is a generated contract value type standing in for the framework's
// request-scoped context type.
type Context struct{}

// ScheduleID is a generated contract value type.
type ScheduleID string

// ScheduleSpec is a generated contract value type.
type ScheduleSpec struct{ Cron string }

// ScheduleDescription is a generated contract value type.
type ScheduleDescription struct{ ID ScheduleID }

// NewDurableExecutionAccess constructs the access (generated constructor).
func NewDurableExecutionAccess() DurableExecutionAccess { return &durableExecutionAccess{} }
