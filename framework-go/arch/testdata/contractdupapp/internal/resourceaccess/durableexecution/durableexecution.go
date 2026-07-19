package durableexecution

type durableExecutionAccess struct{}

func (d *durableExecutionAccess) RegisterSchedule(rc Context, id ScheduleID, spec ScheduleSpec) error {
	return nil
}
func (d *durableExecutionAccess) CancelSchedule(rc Context, id ScheduleID) error { return nil }
func (d *durableExecutionAccess) DescribeSchedule(rc Context, id ScheduleID) (ScheduleDescription, error) {
	return ScheduleDescription{ID: id}, nil
}
func (d *durableExecutionAccess) ListSchedules(rc Context) ([]ScheduleID, error) { return nil, nil }
