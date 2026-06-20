# designphase testdata fixture

A Method project at the pure design phase: a go.mod and an empty `internal/` tree, no
Go code. Used by methodcheck's TestCheck_DesignOnlyNoCodeSkipsLayerAndAlignment to
prove Check skips the arch layer rules + alignment when the package set is empty.
