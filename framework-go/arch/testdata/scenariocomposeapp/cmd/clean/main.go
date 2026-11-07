// Command clean imports neither test substrate.
package main

import "example.com/scenariocomposeapp/internal/manager/okmgr"

func main() { _ = okmgr.OKManager{} }
