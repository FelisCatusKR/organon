package cli

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"github.com/FelisCatusKR/organon/api/internal/client"
)

var (
	fullIDRE   = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)
	prefixIDRE = regexp.MustCompile(`^[0-9a-f-]{4,36}$`)
	allStates  = []string{"TODO", "NEXT", "DOING", "WAITING", "DONE", "CANCELLED"}
)

type candidate struct{ id, label string }

func pick(kind, prefix string, all []candidate) (string, error) {
	prefix = strings.ToLower(prefix)
	if fullIDRE.MatchString(prefix) {
		return prefix, nil
	}
	if !prefixIDRE.MatchString(prefix) {
		return "", fmt.Errorf("%q is not a %s ID or an ID prefix of at least 4 characters", prefix, kind)
	}
	var matches []candidate
	for _, c := range all {
		if strings.HasPrefix(c.id, prefix) {
			matches = append(matches, c)
		}
	}
	switch len(matches) {
	case 0:
		return "", fmt.Errorf("no %s ID starts with %s", kind, prefix)
	case 1:
		return matches[0].id, nil
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%s matches %d %ss; use more characters:", prefix, len(matches), kind)
	for _, m := range matches {
		fmt.Fprintf(&b, "\n  %s  %s", m.id, m.label)
	}
	return "", fmt.Errorf("%s", b.String())
}

// resolveTask accepts a full ID or a unique prefix of at least 4 characters,
// looked up among tasks in every state (so closed tasks can be reopened).
func resolveTask(ctx context.Context, c *client.Client, arg string) (string, error) {
	if fullIDRE.MatchString(strings.ToLower(arg)) {
		return strings.ToLower(arg), nil
	}
	list, _, err := c.ListTasks(ctx, allStates, "", "")
	if err != nil {
		return "", err
	}
	all := make([]candidate, 0, len(list.Items))
	for _, t := range list.Items {
		all = append(all, candidate{t.ID, string(t.State) + " " + t.Title})
	}
	return pick("task", arg, all)
}

func resolveProject(ctx context.Context, c *client.Client, arg string) (string, error) {
	if fullIDRE.MatchString(strings.ToLower(arg)) {
		return strings.ToLower(arg), nil
	}
	list, _, err := c.ListProjects(ctx)
	if err != nil {
		return "", err
	}
	all := make([]candidate, 0, len(list.Items))
	for _, p := range list.Items {
		all = append(all, candidate{p.ID, p.Title})
	}
	return pick("project", arg, all)
}

// resolveNode accepts a full ID or a unique prefix, looked up among the nodes
// search returns (archived nodes need their full ID).
func resolveNode(ctx context.Context, c *client.Client, arg string) (string, error) {
	if fullIDRE.MatchString(strings.ToLower(arg)) {
		return strings.ToLower(arg), nil
	}
	list, _, err := c.SearchNodes(ctx, "", "")
	if err != nil {
		return "", err
	}
	all := make([]candidate, 0, len(list.Items))
	for _, n := range list.Items {
		all = append(all, candidate{n.ID, n.Title})
	}
	return pick("node", arg, all)
}
