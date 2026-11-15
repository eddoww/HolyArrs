package mediafiles

// SpecsCompile says whether an imported trait's tests can all be read, so
// the trait can match at all (a trait whose tests cannot be read never does).
func SpecsCompile(specs []OwnSpec) bool {
	_, ok := compileSpecs(specs)
	return ok
}
