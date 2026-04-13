package tracker

// BuiltinNames returns names of built-in trackers.
func BuiltinNames() []string {
	return []string{"dooray"}
}

// IsBuiltin returns true if the name is a built-in tracker.
func IsBuiltin(name string) bool {
	for _, n := range BuiltinNames() {
		if n == name {
			return true
		}
	}
	return false
}
