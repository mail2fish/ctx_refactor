package lib

import "regexp"

var ignoreFunList = []*regexp.Regexp{regexp.MustCompile("len\\("),
	regexp.MustCompile("append\\("),
	regexp.MustCompile("SpanFromContext\\("),
	regexp.MustCompile("SetTag\\("),
	regexp.MustCompile("SchemeWithSpan\\("),
	regexp.MustCompile("func *\\( *\\)"),
	regexp.MustCompile("String *\\( *\\)"),
	regexp.MustCompile("TrimSpace *\\("),
	regexp.MustCompile("ctx\\.GetValue\\("),

	regexp.MustCompile("contextvals\\.GetUserId\\("),

	// regexp.MustCompile(""),
	// regexp.MustCompile(""),
}

func ignoreFun(name string) bool {
	for _, r := range ignoreFunList {
		if r.MatchString(name) {
			return true
		}
	}
	return false
}
