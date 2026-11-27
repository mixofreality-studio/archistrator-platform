package methodcheck

import (
	"bytes"
	"encoding/json"
	"fmt"
)

// ---- ResourceSeed ----

// ResourceSeedKind discriminates ResourceSeed variants.
type ResourceSeedKind string

// The ResourceSeed variants: the substrates a scenario host can load a fixture into.
const (
	SeedKindPostgres ResourceSeedKind = "postgres"
	SeedKindGitHub   ResourceSeedKind = "github"
	SeedKindGitRepo  ResourceSeedKind = "gitRepo"
)

// Defaults of a GitRepoSeed, applied by DecodeResourceSeed and nowhere else.
const (
	DefaultGitRepoBranch  = "main"
	DefaultGitRepoMessage = "arrange"
)

// ResourceSeedShapes names the three admitted fixture shapes, for findings and errors.
const ResourceSeedShapes = `{"kind":"postgres","model":{"sql":"..."}}, ` +
	`{"kind":"github","model":{"owner","name","description","topics","private","files":{path:content}}} or ` +
	`{"kind":"gitRepo","model":{"branch","message","files":{path:content}}}`

// ResourceSeed is the typed fixture of a seedResource arrange call: exactly one of the
// substrate seeds. Methodcheck (TP-ARRANGE), testgen and the scenario host all decode a
// fixture through DecodeResourceSeed, so what the plan admits is what the host can load.
//
//sumtype:decl
type ResourceSeed interface {
	SeedKind() ResourceSeedKind
	isResourceSeed()
}

// PostgresSeed is SQL executed on the host's database (kind "postgres"). The statements run
// over the tables the Resource's ResourceAccess creates.
type PostgresSeed struct {
	SQL string `json:"sql"`
}

// GitHubSeed is a repository, and its files, in the GitHub double's catalog (kind "github").
type GitHubSeed struct {
	Owner       string            `json:"owner"`
	Name        string            `json:"name"`
	Description string            `json:"description"`
	Topics      []string          `json:"topics"`
	Private     bool              `json:"private"`
	Files       map[string]string `json:"files"`
}

// GitRepoSeed is one commit adding Files on top of the local repo's Branch (kind "gitRepo").
type GitRepoSeed struct {
	Branch  string            `json:"branch"`
	Message string            `json:"message"`
	Files   map[string]string `json:"files"`
}

// SeedKind implements ResourceSeed.
func (PostgresSeed) SeedKind() ResourceSeedKind { return SeedKindPostgres }

// SeedKind implements ResourceSeed.
func (GitHubSeed) SeedKind() ResourceSeedKind { return SeedKindGitHub }

// SeedKind implements ResourceSeed.
func (GitRepoSeed) SeedKind() ResourceSeedKind { return SeedKindGitRepo }

func (PostgresSeed) isResourceSeed() {}
func (GitHubSeed) isResourceSeed()   {}
func (GitRepoSeed) isResourceSeed()  {}

type (
	postgresSeedWire PostgresSeed
	gitHubSeedWire   GitHubSeed
	gitRepoSeedWire  GitRepoSeed
)

// MarshalJSON writes the envelope.
func (v PostgresSeed) MarshalJSON() ([]byte, error) {
	return marshalEnvelope(v.SeedKind(), postgresSeedWire(v))
}

// MarshalJSON writes the envelope.
func (v GitHubSeed) MarshalJSON() ([]byte, error) {
	return marshalEnvelope(v.SeedKind(), gitHubSeedWire(v))
}

// MarshalJSON writes the envelope.
func (v GitRepoSeed) MarshalJSON() ([]byte, error) {
	return marshalEnvelope(v.SeedKind(), gitRepoSeedWire(v))
}

// DecodeResourceSeed decodes a seedResource fixture, the {"kind","model"} envelope of one
// seed variant. It is strict: an unknown kind or field, trailing data, an empty sql, a
// github seed without owner and name, and a gitRepo seed without files are errors. A
// gitRepo seed's empty branch and message take their defaults here.
func DecodeResourceSeed(fixture json.RawMessage) (ResourceSeed, error) {
	dec := json.NewDecoder(bytes.NewReader(fixture))
	var value json.RawMessage
	if err := dec.Decode(&value); err != nil {
		return nil, fmt.Errorf("resource seed: fixture is not a JSON value: %w", err)
	}
	if dec.More() {
		return nil, fmt.Errorf("resource seed: trailing data after the JSON value")
	}
	kind, model, err := openEnvelope(value)
	if err != nil {
		return nil, fmt.Errorf("resource seed: %w", err)
	}
	var seed ResourceSeed
	switch ResourceSeedKind(kind) {
	case SeedKindPostgres:
		seed, err = decodePostgresSeed(model)
	case SeedKindGitHub:
		seed, err = decodeGitHubSeed(model)
	case SeedKindGitRepo:
		seed, err = decodeGitRepoSeed(model)
	default:
		return nil, fmt.Errorf("resource seed: unknown kind %q (want %s, %s or %s)", kind, SeedKindPostgres, SeedKindGitHub, SeedKindGitRepo)
	}
	if err != nil {
		return nil, fmt.Errorf("resource seed %s: %w", kind, err)
	}
	return seed, nil
}

func decodePostgresSeed(model json.RawMessage) (ResourceSeed, error) {
	var v postgresSeedWire
	if err := decodeSeedModel(model, &v); err != nil {
		return nil, err
	}
	if v.SQL == "" {
		return nil, fmt.Errorf("sql is empty")
	}
	return PostgresSeed(v), nil
}

func decodeGitHubSeed(model json.RawMessage) (ResourceSeed, error) {
	var v gitHubSeedWire
	if err := decodeSeedModel(model, &v); err != nil {
		return nil, err
	}
	if v.Owner == "" || v.Name == "" {
		return nil, fmt.Errorf("owner and name are required")
	}
	return GitHubSeed(v), nil
}

// decodeGitRepoSeed is the one place a gitRepo seed's branch and message take their defaults.
func decodeGitRepoSeed(model json.RawMessage) (ResourceSeed, error) {
	var v gitRepoSeedWire
	if err := decodeSeedModel(model, &v); err != nil {
		return nil, err
	}
	if len(v.Files) == 0 {
		return nil, fmt.Errorf("files is empty")
	}
	if v.Branch == "" {
		v.Branch = DefaultGitRepoBranch
	}
	if v.Message == "" {
		v.Message = DefaultGitRepoMessage
	}
	return GitRepoSeed(v), nil
}

// decodeSeedModel decodes a seed model strictly: unknown fields and trailing data are errors.
func decodeSeedModel(model json.RawMessage, v any) error {
	dec := json.NewDecoder(bytes.NewReader(model))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		return err
	}
	if dec.More() {
		return fmt.Errorf("trailing data after the model")
	}
	return nil
}
