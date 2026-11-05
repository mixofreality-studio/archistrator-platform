package configgen

import (
	"fmt"
	"go/scanner"
	"go/token"
	"regexp"
	"slices"
	"strconv"

	"github.com/mixofreality-studio/archistrator-platform/framework-go-projectmodel"
)

// Input is one environment variable the generated Config reads, as config.gen.go names it.
// It is the one source of every <PREFIX>_* name: a launcher or a gate that needs an env name
// reads it here rather than deriving it again.
type Input struct {
	// Env is the variable's name.
	Env string
	// Field is the Config field it is read into.
	Field string
	// InfraKey, Substrate and CatalogInput identify an infrastructure input ("" for a setting).
	InfraKey     string
	Substrate    string
	CatalogInput string
	// Setting is the declared setting's name ("" for an infrastructure input).
	Setting string
	// Default is the value LoadConfig falls back to when the variable is unset.
	Default string
	// Profiles is the profiles an infrastructure input's decl is provisioned for.
	Profiles []string
	// Required reports an input of a required-class decl with no default: MissingFor lists it
	// under every profile in Profiles.
	Required bool
}

// RequiredFor reports whether the generated config requires the input under profile.
func (in Input) RequiredFor(profile string) bool {
	return in.Required && slices.Contains(in.Profiles, profile)
}

// Inputs is every environment variable the Config generated for m and cfg reads, the
// framework's local dev principal last.
func Inputs(m *projectmodel.Model, cfg Config) ([]Input, error) {
	infra, settings, err := collect(m, cfg)
	if err != nil {
		return nil, err
	}
	out := make([]Input, 0, len(infra)+len(settings)+1)
	for _, f := range infra {
		out = append(out, Input{
			Env: f.Env, Field: f.GoName, InfraKey: f.InfraKey, Substrate: f.Substrate, CatalogInput: f.Input,
			Default: f.Default, Profiles: f.Profiles, Required: f.RequiredClass && f.Default == "",
		})
	}
	for _, f := range settings {
		out = append(out, Input{Env: f.Env, Field: f.GoName, Setting: f.Name, Default: f.Default})
	}
	out = append(out, Input{Env: DevPrincipalEnv(cfg.EnvPrefix), Field: devPrincipalField})
	return out, nil
}

// DevPrincipalEnv is the variable the framework's local-profile dev principal is read from.
func DevPrincipalEnv(prefix string) string { return prefix + "_DEV_PRINCIPAL" }

// UndeclaredEnvReads is the gate on the hand half of a composition root (FIX-049): every
// string literal in srcs (file name -> Go source) spelled like a <prefix>_* variable must be one
// the generated Config declares. A hand hook that reads an env var of its own invention is
// configuration no launcher, operator or gate can see; the value belongs in the deployment
// model, read through the Config. It returns one "file: NAME" per offence, sorted.
func UndeclaredEnvReads(inputs []Input, prefix string, srcs map[string][]byte) ([]string, error) {
	declared := make(map[string]bool, len(inputs))
	for _, in := range inputs {
		declared[in.Env] = true
	}
	name := regexp.MustCompile(`^` + regexp.QuoteMeta(prefix) + `_[A-Z0-9_]+$`)
	var out []string
	for file, src := range srcs {
		lits, err := stringLiterals(file, src)
		if err != nil {
			return nil, err
		}
		for _, lit := range lits {
			if name.MatchString(lit) && !declared[lit] {
				out = append(out, file+": "+lit)
			}
		}
	}
	slices.Sort(out)
	return slices.Compact(out), nil
}

// stringLiterals is every string literal in a Go source file, comments excluded.
func stringLiterals(file string, src []byte) ([]string, error) {
	fset := token.NewFileSet()
	var errs scanner.ErrorList
	var s scanner.Scanner
	s.Init(fset.AddFile(file, -1, len(src)), src, func(pos token.Position, msg string) { errs.Add(pos, msg) }, 0)
	var out []string
	for {
		_, tok, lit := s.Scan()
		if tok == token.EOF {
			break
		}
		if tok != token.STRING {
			continue
		}
		v, err := strconv.Unquote(lit)
		if err != nil {
			return nil, fmt.Errorf("configgen: %s: string literal %s: %w", file, lit, err)
		}
		out = append(out, v)
	}
	if len(errs) > 0 {
		return nil, fmt.Errorf("configgen: %w", errs.Err())
	}
	return out, nil
}
