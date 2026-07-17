package widgetaccess

// WidgetAccess is the contract this component fronts.
type WidgetAccess interface {
	GetWidget(id string) (string, error)
}
