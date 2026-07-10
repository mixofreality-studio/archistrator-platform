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
		if ra.switched {
			writeSwitchArms(b, ra)
			continue
		}
		writeSingleArm(b, ra)
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
		writeReadyLog(b, "\t", ra.key, arm.variant)
		return
	}
	b.WriteString("\t" + ra.varName + " := " + call + "\n")
	writeReadyLog(b, "\t", ra.key, arm.variant)
}

// writeReadyLog emits the boot-log parity line for one constructed binding arm
// (run()'s "<componentKey> (<variant>) ready" convention, e.g. "artifactAccess
// (github) ready").
func writeReadyLog(b *strings.Builder, indent, key, variant string) {
	b.WriteString(indent + "logger.Info(\"" + key + " (" + variant + ") ready\")\n")
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
		writeReadyLog(b, "\t\t", ra.key, arm.variant)
		return
	}
	b.WriteString("\t\t" + ra.varName + " = " + call + "\n")
	writeReadyLog(b, "\t\t", ra.key, arm.variant)
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
		if mc.gated {
			writeGatedWorker(b, mc)
			continue
		}
		writeWorker(b, "\t", mc)
	}
	b.WriteString("\n")
}

// writeGatedWorker wraps a manager's Worker registration in its
// Register<Iface>Worker(cfg) gate hook (G6b) — the Worker runs only when the
// composition root says so (optional-dormant deps present / a dry-run stub filled
// them), else a dormancy warning.
func writeGatedWorker(b *strings.Builder, mc managerComp) {
	b.WriteString("\tif hooks.Register" + mc.iface + "Worker(cfg) {\n")
	writeWorker(b, "\t\t", mc)
	b.WriteString("\t} else {\n")
	b.WriteString("\t\tlogger.Warn(\"" + mc.key + " Worker NOT registered — optional-dormant dependencies absent (Register" + mc.iface + "Worker gate returned false)\")\n")
	b.WriteString("\t}\n")
}

// writeWorker emits one manager's Worker registration + start + deferred stop at
// the given indent.
func writeWorker(b *strings.Builder, indent string, mc managerComp) {
	w := "w" + upperFirst(mc.varName)
	b.WriteString(indent + w + " := worker.New(tc, " + mc.alias + ".TaskQueue, worker.Options{})\n")
	b.WriteString(indent + mc.alias + ".RegisterManagerWorker(" + w + ", " + mc.varName + ")\n")
	b.WriteString(indent + "if err := " + w + ".Start(); err != nil {\n" + indent + "\treturn err\n" + indent + "}\n")
	b.WriteString(indent + "defer " + w + ".Stop()\n")
	b.WriteString(indent + "logger.Info(\"embedded temporal worker started\", \"taskQueue\", " + mc.alias + ".TaskQueue)\n")
}
