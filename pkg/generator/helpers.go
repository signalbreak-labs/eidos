package generator

// fullAttrPath joins a parent attribute path with a child name. The empty parent
// path yields the child name alone, producing dotted paths such as
// "parent.child" as the generator descends through nested schemas.
func fullAttrPath(parent, name string) string {
	if parent == "" {
		return name
	}
	return parent + "." + name
}
