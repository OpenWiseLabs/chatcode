package executor

import (
	"regexp"
	"strings"
)

func extractSessionIDByRegex(chunk string, re *regexp.Regexp, group int) string {
	if chunk == "" || re == nil {
		return ""
	}
	matches := re.FindAllStringSubmatch(chunk, -1)
	if len(matches) == 0 {
		return ""
	}
	last := matches[len(matches)-1]
	if len(last) <= group {
		return ""
	}
	return strings.TrimSpace(last[group])
}
