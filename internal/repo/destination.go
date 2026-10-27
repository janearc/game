package repo

import "strings"

// Elsewhere is whether git would read this url or path as another
// machine. File urls are here. So are paths with a slash before any colon.
//
// Any other form with a colon is elsewhere: a scheme, a transport helper
// (name::address) or host:path. So is any form this does not know. The
// wrong answer is then the safe one.
func Elsewhere(url string) bool {
	if strings.HasPrefix(url, "file://") {
		return false
	}
	host, _, colon := strings.Cut(url, ":")
	return colon && !strings.Contains(host, "/")
}

// Leaves is whether a push of dist goes to another machine. dist is what
// git is given to push to: a remote's name, a url or a path.
//
// Git's own account of where it goes is used. That counts a remote's push
// url and the rewrites in git's config. When git cannot say it is yes.
func (tree Repo) Leaves(dist string) bool {
	target, err := tree.pushTarget(dist)
	return err != nil || Elsewhere(target)
}

// pushTarget is where git sends a push of dist. Git sends a remote's
// name to the url it pushes to. Anything else is rewritten as git
// rewrites it. PushInsteadOf comes first, then insteadOf.
func (tree Repo) pushTarget(dist string) (string, error) {
	named, err := tree.Git("remote", "get-url", "--push", dist)
	if err == nil {
		return named, nil
	}
	listing := []string{"config", "--null", "--list"}
	config, err := tree.output("", nil, listing)
	if err != nil {
		return "", err
	}
	pushed := rulesIn(config, ".pushinsteadof")
	if target, done := rewritten(dist, pushed); done {
		return target, nil
	}
	instead := rulesIn(config, ".insteadof")
	target, _ := rewritten(dist, instead)
	return target, nil
}

// rule is one url.BASE.insteadOf line of git's config: any url that
// begins with instead begins with base.
type rule struct {
	base, instead string
}

// rulesIn is the rules of one kind in config. Config is the output of
// git config --null --list. Kind is the end of the key in lower case:
// .insteadof or .pushinsteadof.
func rulesIn(config, kind string) []rule {
	var rules []rule
	for _, entry := range strings.Split(config, "\x00") {
		key, value, _ := strings.Cut(entry, "\n")
		inner, begins := strings.CutPrefix(key, "url.")
		base, ends := strings.CutSuffix(inner, kind)
		if begins && ends {
			rules = append(rules, rule{base: base, instead: value})
		}
	}
	return rules
}

// rewritten is url with the longest matching rule applied, and whether
// any rule matched.
func rewritten(url string, rules []rule) (string, bool) {
	var best rule
	matched := false
	for _, one := range rules {
		longer := !matched || len(one.instead) > len(best.instead)
		if strings.HasPrefix(url, one.instead) && longer {
			best, matched = one, true
		}
	}
	return best.base + strings.TrimPrefix(url, best.instead), matched
}
