// Command server builds its dependencies from the test-subject package: a test substrate
// in the binary.
package main

import "example.com/scenariocomposeapp/internal/scenariocompose"

func main() { _ = scenariocompose.Subject{} }
