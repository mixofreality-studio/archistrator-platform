package composegen

import "strings"

// formatError carries the unformatted source alongside a gofmt failure.
type formatError struct {
	err error
	src string
}

func (e *formatError) Error() string {
	return "composegen: gofmt: " + e.err.Error() + "\n" + e.src
}

func (e *formatError) Unwrap() error { return e.err }

// writeRABlocks emits the per-binding ResourceAccess construction: a profile
// switch when a binding has multiple arms, a direct construction otherwise.
func writeRABlocks(b *strings.Builder, r *resolved) {
	if len(r.ras) == 0 {
		return
	}
	b.WriteString("\t// ResourceAccess — one binding per component, variant-selected by profile.\n")
	for _, ra := range r.ras {
		if len(ra.arms) == 1 {
			writeSingleArm(b, ra)
			continue
		}
		writeSwitchArms(b, ra)
	}
	b.WriteString("\n")
}

// writeSingleArm emits a binding with exactly one profile arm (no switch).
func writeSingleArm(b *strings.Builder, ra raBinding) {
	arm := ra.arms[0]
	call := arm.ctor + "(" + strings.Join(arm.args, ", ") + ")"
	if arm.returnsError {
		b.WriteString("\t" + ra.varName + ", err := " + call + "\n")
		b.WriteString("\tif err != nil {\n\t\treturn err\n\t}\n")
		return
	}
	b.WriteString("\t" + ra.varName + " := " + call + "\n")
}

// writeSwitchArms emits a binding with multiple profile arms as a profile switch
// over the pre-declared interface var.
func writeSwitchArms(b *strings.Builder, ra raBinding) {
	b.WriteString("\tvar " + ra.varName + " " + ra.alias + "." + ra.iface + "\n")
	b.WriteString("\tswitch profile {\n")
	for _, arm := range ra.arms {
		b.WriteString("\tcase \"" + arm.profile + "\":\n")
		writeArmBody(b, ra, arm)
	}
	if strings.EqualFold(ra.presence, "required") {
		b.WriteString("\tdefault:\n")
		b.WriteString("\t\treturn errors.New(\"" + ra.key + ": no ResourceAccess variant for the active profile\")\n")
	}
	b.WriteString("\t}\n")
}

// writeArmBody emits one switch case's construction.
func writeArmBody(b *strings.Builder, ra raBinding, arm variantArm) {
	call := arm.ctor + "(" + strings.Join(arm.args, ", ") + ")"
	if arm.returnsError {
		b.WriteString("\t\tv, err := " + call + "\n")
		b.WriteString("\t\tif err != nil {\n\t\t\treturn err\n\t\t}\n")
		b.WriteString("\t\t" + ra.varName + " = v\n")
		return
	}
	b.WriteString("\t\t" + ra.varName + " = " + call + "\n")
}

// writeManagers emits each manager's DI construction + its embedded Temporal
// Worker registration.
func writeManagers(b *strings.Builder, r *resolved) {
	if len(r.managers) == 0 {
		return
	}
	b.WriteString("\t// Managers — generated DI constructors + one embedded Worker each.\n")
	for _, mc := range r.managers {
		b.WriteString("\t" + mc.varName + " := " + mc.ctor + "(" + strings.Join(mc.ctorArgs, ", ") + ")\n")
		writeWorker(b, mc)
	}
	b.WriteString("\n")
}

// writeWorker emits one manager's Worker registration + start + deferred stop.
func writeWorker(b *strings.Builder, mc managerComp) {
	w := "w" + upperFirst(mc.varName)
	b.WriteString("\t" + w + " := worker.New(tc, " + mc.alias + ".TaskQueue, worker.Options{})\n")
	b.WriteString("\t" + mc.alias + ".RegisterManagerWorker(" + w + ", " + mc.varName + ")\n")
	b.WriteString("\tif err := " + w + ".Start(); err != nil {\n\t\treturn err\n\t}\n")
	b.WriteString("\tdefer " + w + ".Stop()\n")
	b.WriteString("\tlogger.Info(\"embedded temporal worker started\", \"taskQueue\", " + mc.alias + ".TaskQueue)\n")
}
