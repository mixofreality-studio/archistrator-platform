package methodcheck

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

func TestResourceSeed_DecodesEachVariant(t *testing.T) {
	cases := map[string]struct {
		raw  string
		want ResourceSeed
	}{
		"postgres": {`{"kind":"postgres","model":{"sql":"SELECT 1"}}`, PostgresSeed{SQL: "SELECT 1"}},
		"github": {`{"kind":"github","model":{"owner":"o","name":"n","description":"d","topics":["t"],"private":true,"files":{"a":"b"}}}`,
			GitHubSeed{Owner: "o", Name: "n", Description: "d", Topics: []string{"t"}, Private: true, Files: map[string]string{"a": "b"}}},
		"gitRepo defaults": {`{"kind":"gitRepo","model":{"files":{"a":"b"}}}`,
			GitRepoSeed{Branch: DefaultGitRepoBranch, Message: DefaultGitRepoMessage, Files: map[string]string{"a": "b"}}},
		"gitRepo explicit": {`{"kind":"gitRepo","model":{"branch":"dev","message":"m","files":{"a":"b"}}}`,
			GitRepoSeed{Branch: "dev", Message: "m", Files: map[string]string{"a": "b"}}},
	}
	for name, c := range cases {
		got, err := DecodeResourceSeed(json.RawMessage(c.raw))
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s: got %#v, want %#v", name, got, c.want)
		}
	}
}

func TestResourceSeed_RefusesWhatIsNotASeed(t *testing.T) {
	cases := map[string]string{
		"domain json":           `{"owner":"o","todos":[]}`,
		"unknown kind":          `{"kind":"redis","model":{}}`,
		"unknown field":         `{"kind":"postgres","model":{"sql":"x","rows":2}}`,
		"unknown envelope key":  `{"kind":"postgres","model":{"sql":"x"},"extra":1}`,
		"trailing data":         `{"kind":"postgres","model":{"sql":"x"}} {}`,
		"trailing model data":   `{"kind":"postgres","model":{"sql":"x"} {}}`,
		"empty sql":             `{"kind":"postgres","model":{"sql":""}}`,
		"github without owner":  `{"kind":"github","model":{"name":"n"}}`,
		"github without name":   `{"kind":"github","model":{"owner":"o"}}`,
		"gitRepo without files": `{"kind":"gitRepo","model":{"branch":"main"}}`,
		"empty":                 ``,
		"not json":              `nope`,
		"truncated json":        `{"kind":"postgres","model":`,
		"json array":            `[1]`,
	}
	for name, raw := range cases {
		if s, err := DecodeResourceSeed(json.RawMessage(raw)); err == nil {
			t.Errorf("%s: admitted as %#v", name, s)
		}
	}
}

func TestResourceSeed_MarshalRoundTrips(t *testing.T) {
	for _, s := range []ResourceSeed{
		PostgresSeed{SQL: "SELECT 1"},
		GitHubSeed{Owner: "o", Name: "n", Files: map[string]string{"a": "b"}},
		GitRepoSeed{Branch: "main", Message: "m", Files: map[string]string{"a": "b"}},
	} {
		raw, err := json.Marshal(s)
		if err != nil {
			t.Fatal(err)
		}
		got, err := DecodeResourceSeed(raw)
		if err != nil {
			t.Fatalf("%s: %v", raw, err)
		}
		if !strings.Contains(string(raw), `"kind":"`+string(s.SeedKind())+`"`) || !reflect.DeepEqual(got, s) {
			t.Errorf("round trip of %#v via %s gave %#v", s, raw, got)
		}
	}
}
