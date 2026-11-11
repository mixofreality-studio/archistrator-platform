package devstack

import (
	"context"
	"fmt"
	"os/exec"
	"sort"
	"strings"
)

// DockerInventory lists the containers and volumes docker holds now, as "container:<name>" and
// "volume:<name>".
func DockerInventory(ctx context.Context) ([]string, error) {
	var out []string
	for _, q := range []struct {
		kind string
		argv []string
	}{
		{"container", []string{"ps", "-a", "--format", "{{.Names}}"}},
		{"volume", []string{"volume", "ls", "-q"}},
	} {
		b, err := exec.CommandContext(ctx, "docker", q.argv...).Output() // #nosec G204 -- fixed argv
		if err != nil {
			return nil, fmt.Errorf("docker %s: %w", strings.Join(q.argv, " "), err)
		}
		for _, name := range strings.Fields(string(b)) {
			out = append(out, q.kind+":"+name)
		}
	}
	sort.Strings(out)
	return out, nil
}

// reaperPrefix names the container testcontainers starts once per process (Ryuk) to remove, after
// the process exits, whatever the process started. It is the substrate's own guard against a leak, so
// its presence is not one, and it cannot be gone before the process that holds it is.
const reaperPrefix = "container:reaper_"

// DockerLeftovers is the names in after that were not in before, except testcontainers' own reaper
// (it removes itself when the process that started it exits). The context is accepted for symmetry
// with DockerInventory; the comparison is pure.
func DockerLeftovers(_ context.Context, before, after []string) []string {
	seen := make(map[string]bool, len(before))
	for _, n := range before {
		seen[n] = true
	}
	var left []string
	for _, n := range after {
		if !seen[n] && !strings.HasPrefix(n, reaperPrefix) {
			left = append(left, n)
		}
	}
	return left
}
