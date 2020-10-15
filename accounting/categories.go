package accounting

import "regexp"

// CustomCategory describes a custom category which can be used to identify
// special case groups of transactions.
type CustomCategory struct {
	// Name is the custom name for the category.
	Name string

	// Regexes is a slice of regexes which we will use to match labels. If
	// a label matches any one expression in this set, it is considered
	// part of the category.
	Regexes []*regexp.Regexp
}

// NewCustomCategory creates compiles the set of regexes provided and returning
// a new category. This function assumes that each regex is unique.
func NewCustomCategory(name string, regexes []string) (*CustomCategory, error) {
	category := &CustomCategory{
		Name: name,
	}

	for _, regex := range regexes {
		exp, err := regexp.Compile(regex)
		if err != nil {
			return nil, err
		}

		category.Regexes = append(category.Regexes, exp)
	}

	return category, nil
}

// isMember returns a boolean indicating whether a label belongs in a custom
// category. If the label matches any one of the category's regexes, it is
// considered a member of the category.
func (c CustomCategory) isMember(label string) bool {
	for _, regex := range c.Regexes {
		isMatch := regex.Match([]byte(label))
		if isMatch {
			return true
		}
	}

	return false
}

// getCategory matches a label against a set of custom categories, and returns
// the name of the category it belongs in (if any).
func getCategory(label string, categories []CustomCategory) string {
	for _, category := range categories {
		if category.isMember(label) {
			return category.Name
		}
	}

	return ""
}
