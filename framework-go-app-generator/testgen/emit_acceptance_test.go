package testgen

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// The runtime every generated Playwright spec runs on typechecks under strict compiler options, and
// exposes what an acceptance spec calls: a contract call and an MCP tool call, each as a named
// actor. The usage file is what Task 4.4's generated specs write; this test pins the runtime's side
// of that contract. It needs npm (the generated TypeScript cannot be proven to typecheck without it).
func TestRuntime_TypeChecks(t *testing.T) {
	if _, err := exec.LookPath("npm"); err != nil {
		t.Fatal("npm is required: the runtime cannot be proven to typecheck without it")
	}
	dir := t.TempDir()
	write := func(name, content string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write("runtime.ts", tsRuntimeSource)
	write("usage.ts", `import { expect } from '@playwright/test';
import type { TestInfo } from '@playwright/test';
import { mcpCall, probe, type ContractCall, type ToolCall } from './runtime';

export async function usage(testInfo: TestInfo): Promise<unknown> {
  const as: ContractCall = { verb: 'GET', path: '/api/v1/list/openList', args: {}, expect: { errorExpected: false }, as: 'agent-1' };
  await probe(testInfo, as);
  const tool: ToolCall = { tool: 'listOpenList', args: {}, expect: { errorExpected: false }, as: 'agent-1' };
  expect(tool.as).toBe('agent-1');
  return mcpCall(testInfo, tool);
}
`)
	write("package.json", `{"name":"runtime-typecheck","private":true,"type":"module"}`)
	write("tsconfig.json", `{"compilerOptions":{"target":"ES2022","module":"ESNext","moduleResolution":"bundler","lib":["ES2023","DOM"],"types":["node"],"strict":true,"noUncheckedIndexedAccess":true,"noEmit":true,"skipLibCheck":true},"files":["runtime.ts","usage.ts"]}`)

	// The uitests bootstrap's own ranges (webgen/templates/bootstrap/uitests.package.json.tmpl).
	install := exec.Command("npm", "install", "--no-audit", "--no-fund", "--prefer-offline", "--ignore-scripts",
		"@playwright/test@^1.50.0", "@types/node@^25.0.3", "typescript@~5.9.3")
	install.Dir = dir
	if out, err := install.CombinedOutput(); err != nil {
		t.Fatalf("npm install: %v\n%s", err, out)
	}
	tsc := exec.Command(filepath.Join(dir, "node_modules", ".bin", "tsc"), "-p", "tsconfig.json")
	tsc.Dir = dir
	if out, err := tsc.CombinedOutput(); err != nil {
		t.Fatalf("tsc: %v\n%s", err, out)
	}
}
