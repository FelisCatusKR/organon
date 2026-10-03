package cli

import (
	"fmt"
	"io"
	"strings"

	"github.com/FelisCatusKR/organon/api/internal/model"
)

func shortID(id string) string {
	if len(id) > 8 {
		return id[:8]
	}
	return id
}

// stamp renders a planning date exactly as the API returned it.
func stamp(prefix string, ts *model.Timestamp) string {
	if ts == nil {
		return ""
	}
	s := prefix + ts.Date
	if ts.Time != nil {
		s += " " + *ts.Time
	}
	if ts.Repeat != nil {
		s += " " + *ts.Repeat
	}
	if ts.WarningDays != nil {
		s += fmt.Sprintf(" -%dd", *ts.WarningDays)
	}
	return s
}

// taskLine is one row of the human-readable output.
func taskLine(t model.Task) string {
	var dates []string
	if s := stamp("S ", t.Scheduled); s != "" {
		dates = append(dates, s)
	}
	if s := stamp("D ", t.Deadline); s != "" {
		dates = append(dates, s)
	}
	if t.ClosedAt != nil {
		// Instants are shown in the local zone of this machine.
		dates = append(dates, "closed "+t.ClosedAt.Local().Format("2006-01-02 15:04"))
	}
	title := t.Title
	if t.Priority != nil {
		title = "[#" + string(*t.Priority) + "] " + title
	}
	if len(t.Tags) > 0 {
		title += "  :" + strings.Join(t.Tags, ":") + ":"
	}
	if t.Project != nil {
		title += "  (" + t.Project.Title + ")"
	}
	return fmt.Sprintf("%-9s %s  %-28s %s", t.State, shortID(t.ID), strings.Join(dates, ", "), title)
}

func printTasks(w io.Writer, tasks []model.Task) {
	if len(tasks) == 0 {
		fmt.Fprintln(w, "(no tasks)")
		return
	}
	for _, t := range tasks {
		fmt.Fprintln(w, taskLine(t))
	}
}

func printTask(w io.Writer, t model.Task) {
	fmt.Fprintln(w, taskLine(t))
	fmt.Fprintf(w, "  id       %s\n  version  %s\n", t.ID, t.Version)
	if t.RepeatToState != nil {
		fmt.Fprintf(w, "  repeats back to %s\n", *t.RepeatToState)
	}
	if t.Body != "" {
		fmt.Fprintln(w)
		for _, line := range strings.Split(t.Body, "\n") {
			fmt.Fprintln(w, "  "+line)
		}
	}
}

func printProjects(w io.Writer, projects []model.Project) {
	if len(projects) == 0 {
		fmt.Fprintln(w, "(no projects)")
		return
	}
	for _, p := range projects {
		fmt.Fprintf(w, "%s  %3d open  %s\n", shortID(p.ID), p.OpenTasks, p.Title)
	}
}

// nodeLine is one row of a node list.
func nodeLine(n model.NodeSummary) string {
	line := shortID(n.ID) + "  " + n.Title
	if len(n.Tags) > 0 {
		line += "  :" + strings.Join(n.Tags, ":") + ":"
	}
	if len(n.Aliases) > 0 {
		line += "  (aka " + strings.Join(n.Aliases, ", ") + ")"
	}
	return line
}

func printNodes(w io.Writer, nodes []model.NodeSummary) {
	if len(nodes) == 0 {
		fmt.Fprintln(w, "(no nodes)")
		return
	}
	for _, n := range nodes {
		fmt.Fprintln(w, nodeLine(n))
	}
}

func printNode(w io.Writer, n model.Node) {
	fmt.Fprintln(w, nodeLine(model.NodeSummary{ID: n.ID, Title: n.Title, Tags: n.Tags, Aliases: n.Aliases}))
	fmt.Fprintf(w, "  id       %s\n", n.ID)
	if n.Body != "" {
		fmt.Fprintln(w)
		for _, line := range strings.Split(n.Body, "\n") {
			fmt.Fprintln(w, "  "+line)
		}
	}
}

// printRefs prints backlinks or links: kind, short ID, title.
func printRefs(w io.Writer, refs []model.NodeRef) {
	if len(refs) == 0 {
		fmt.Fprintln(w, "(none)")
		return
	}
	for _, r := range refs {
		fmt.Fprintf(w, "%-4s  %s  %s\n", r.Kind, shortID(r.ID), r.Title)
	}
}
