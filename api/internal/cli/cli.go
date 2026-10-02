// Package cli implements the client commands of the organon binary
// (spec: cli). It talks only to the HTTP API, through package client, and
// never computes dates: date options are passed to the API as given.
package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/FelisCatusKR/organon/api/internal/client"
	"github.com/FelisCatusKR/organon/api/internal/model"
)

// Commands lists the top-level commands this package handles.
var Commands = map[string]bool{"today": true, "overdue": true, "waiting": true, "completed": true, "task": true, "project": true}

// Usage is printed by `organon help`.
const Usage = `client commands (configure with ORGANON_URL / ORGANON_TOKEN or ~/.config/organon/client.json):
  today | overdue | completed [--date YYYY-MM-DD]
  waiting
  task add TITLE [--next] [--priority A|B|C] [--tag T]... [--project ID]
                 [--scheduled "DATE[ HH:MM]"] [--deadline "DATE[ HH:MM]"]
                 [--repeat +1m|++1w|.+3d] [--warn DAYS] [--repeat-to TODO|NEXT] [--body TEXT]
  task list [--state S[,S...]] [--project ID] [--tag T]
  task show ID
  task edit ID [--title T] [--body B] [--priority X|none] [--tags a,b|none]
               [--scheduled D|none] [--deadline D|none] [--repeat R|none] [--warn N]
               (a moved date keeps its repeater and warning unless given)
               [--repeat-to TODO|NEXT|none]
  task start|wait|done|skip|cancel|todo|next ID
  project add TITLE [--body TEXT]
  project list
every command accepts --json (print the API's JSON unchanged); IDs may be
shortened to a unique prefix of at least 4 characters`

// Run executes a client command and returns the process exit code.
func Run(args []string, stdout, stderr io.Writer) int {
	r := &runner{out: stdout, err: stderr}
	if err := r.run(args); err != nil {
		var ae *client.APIError
		if r.json && errors.As(err, &ae) && len(ae.Body) > 0 {
			stdout.Write(append(ae.Body, '\n'))
		} else {
			fmt.Fprintln(stderr, "organon:", err)
		}
		return 1
	}
	return 0
}

type runner struct {
	out, err io.Writer
	json     bool
	c        *client.Client
}

// stringList is a repeatable flag (--tag a --tag b).
type stringList []string

func (s *stringList) String() string     { return strings.Join(*s, ",") }
func (s *stringList) Set(v string) error { *s = append(*s, v); return nil }

// parse parses flags that may appear before, between or after positional
// arguments, and returns the positional ones.
func parse(fs *flag.FlagSet, args []string) ([]string, error) {
	var positional []string
	for len(args) > 0 {
		if err := fs.Parse(args); err != nil {
			return nil, err
		}
		args = fs.Args()
		if len(args) > 0 {
			positional = append(positional, args[0])
			args = args[1:]
		}
	}
	return positional, nil
}

func (r *runner) newFlags(name string) *flag.FlagSet {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(r.err)
	fs.BoolVar(&r.json, "json", false, "print the API's JSON response unchanged")
	return fs
}

func (r *runner) connect() error {
	cfg, err := LoadConfig()
	if err != nil {
		return err
	}
	r.c = client.New(cfg.URL, cfg.Token)
	return nil
}

// emit prints raw JSON with --json, otherwise calls human.
func (r *runner) emit(raw []byte, human func()) {
	if r.json {
		r.out.Write(raw)
		if len(raw) == 0 || raw[len(raw)-1] != '\n' {
			r.out.Write([]byte("\n"))
		}
		return
	}
	human()
}

func (r *runner) run(args []string) error {
	if len(args) == 0 {
		return errors.New(Usage)
	}
	ctx := context.Background()
	switch cmd := args[0]; cmd {
	case "today", "overdue", "completed", "waiting":
		return r.view(ctx, cmd, args[1:])
	case "task":
		if len(args) < 2 {
			return errors.New(Usage)
		}
		return r.task(ctx, args[1], args[2:])
	case "project":
		if len(args) < 2 {
			return errors.New(Usage)
		}
		return r.project(ctx, args[1], args[2:])
	}
	return errors.New(Usage)
}

func (r *runner) view(ctx context.Context, view string, args []string) error {
	fs := r.newFlags(view)
	date := ""
	if view != "waiting" {
		fs.StringVar(&date, "date", "", "calendar date (default: today in the instance's calendar)")
	}
	if pos, err := parse(fs, args); err != nil {
		return err
	} else if len(pos) > 0 {
		return fmt.Errorf("unexpected argument %q", pos[0])
	}
	if err := r.connect(); err != nil {
		return err
	}
	list, raw, err := r.c.TaskView(ctx, view, date)
	if err != nil {
		return err
	}
	r.emit(raw, func() { printTasks(r.out, list.Items) })
	return nil
}

// planning builds a TimestampInput from "DATE" or "DATE HH:MM" without
// interpreting the date: the API validates it and Org places it.
func planning(value, repeat string, warn int) *model.TimestampInput {
	date, clock, _ := strings.Cut(strings.TrimSpace(value), " ")
	ts := &model.TimestampInput{Date: date}
	if clock = strings.TrimSpace(clock); clock != "" {
		ts.Time = &clock
	}
	if repeat != "" {
		ts.Repeat = &repeat
	}
	if warn > 0 {
		ts.WarningDays = &warn
	}
	return ts
}

var transitions = map[string]string{
	"start": "start", "wait": "wait", "done": "complete", "skip": "skip", "cancel": "cancel", "todo": "todo", "next": "next",
}

func (r *runner) task(ctx context.Context, sub string, args []string) error {
	switch sub {
	case "add":
		return r.taskAdd(ctx, args)
	case "list":
		return r.taskList(ctx, args)
	case "show":
		return r.taskShow(ctx, args)
	case "edit":
		return r.taskEdit(ctx, args)
	}
	if action, ok := transitions[sub]; ok {
		return r.taskTransition(ctx, action, args)
	}
	return fmt.Errorf("unknown task command %q\n%s", sub, Usage)
}

func (r *runner) taskAdd(ctx context.Context, args []string) error {
	fs := r.newFlags("task add")
	var (
		next                                                 bool
		priority, project, scheduled, deadline, repeat, body string
		repeatTo                                             string
		warn                                                 int
		tags                                                 stringList
	)
	fs.BoolVar(&next, "next", false, "create as NEXT instead of TODO")
	fs.StringVar(&priority, "priority", "", "A, B or C")
	fs.Var(&tags, "tag", "tag (repeatable)")
	fs.StringVar(&project, "project", "", "project ID or prefix")
	fs.StringVar(&scheduled, "scheduled", "", `"YYYY-MM-DD" or "YYYY-MM-DD HH:MM"`)
	fs.StringVar(&deadline, "deadline", "", `"YYYY-MM-DD" or "YYYY-MM-DD HH:MM"`)
	fs.StringVar(&repeat, "repeat", "", "repeater for the deadline (or the schedule if there is no deadline)")
	fs.IntVar(&warn, "warn", 0, "show the deadline this many days before it")
	fs.StringVar(&repeatTo, "repeat-to", "", "state after a repeat: TODO or NEXT")
	fs.StringVar(&body, "body", "", "text below the heading")
	pos, err := parse(fs, args)
	if err != nil {
		return err
	}
	if len(pos) != 1 {
		return errors.New(`usage: organon task add "TITLE" [options]`)
	}
	if repeat != "" && scheduled == "" && deadline == "" {
		return errors.New("--repeat needs --deadline or --scheduled")
	}
	if warn != 0 && deadline == "" {
		return errors.New("--warn needs --deadline")
	}
	req := model.CreateTask{Title: pos[0]}
	if next {
		st := model.CreateTaskState("NEXT")
		req.State = &st
	}
	if priority != "" {
		p := model.CreateTaskPriority(priority)
		req.Priority = &p
	}
	if len(tags) > 0 {
		t := []string(tags)
		req.Tags = &t
	}
	if body != "" {
		req.Body = &body
	}
	if repeatTo != "" {
		rt := model.CreateTaskRepeatToState(repeatTo)
		req.RepeatToState = &rt
	}
	if deadline != "" {
		req.Deadline = planning(deadline, repeat, warn)
	}
	if scheduled != "" {
		schedRepeat := ""
		if deadline == "" {
			schedRepeat = repeat
		}
		req.Scheduled = planning(scheduled, schedRepeat, 0)
	}
	if err := r.connect(); err != nil {
		return err
	}
	if project != "" {
		id, err := resolveProject(ctx, r.c, project)
		if err != nil {
			return err
		}
		req.ProjectID = &id
	}
	task, raw, err := r.c.CreateTask(ctx, req)
	if err != nil {
		return r.mutationError(err, "")
	}
	r.emit(raw, func() { printTask(r.out, task) })
	return nil
}

func (r *runner) taskList(ctx context.Context, args []string) error {
	fs := r.newFlags("task list")
	var state, project, tag string
	fs.StringVar(&state, "state", "", "comma-separated states (default: TODO,NEXT,DOING,WAITING)")
	fs.StringVar(&project, "project", "", "project ID or prefix")
	fs.StringVar(&tag, "tag", "", "tag")
	if pos, err := parse(fs, args); err != nil {
		return err
	} else if len(pos) > 0 {
		return fmt.Errorf("unexpected argument %q", pos[0])
	}
	if err := r.connect(); err != nil {
		return err
	}
	var states []string
	if state != "" {
		states = strings.Split(strings.ToUpper(state), ",")
	}
	if project != "" {
		id, err := resolveProject(ctx, r.c, project)
		if err != nil {
			return err
		}
		project = id
	}
	list, raw, err := r.c.ListTasks(ctx, states, project, tag)
	if err != nil {
		return err
	}
	r.emit(raw, func() { printTasks(r.out, list.Items) })
	return nil
}

func (r *runner) oneID(name string, args []string) (string, error) {
	fs := r.newFlags(name)
	pos, err := parse(fs, args)
	if err != nil {
		return "", err
	}
	if len(pos) != 1 {
		return "", fmt.Errorf("usage: organon %s ID", name)
	}
	return pos[0], r.connect()
}

func (r *runner) taskShow(ctx context.Context, args []string) error {
	arg, err := r.oneID("task show", args)
	if err != nil {
		return err
	}
	id, err := resolveTask(ctx, r.c, arg)
	if err != nil {
		return err
	}
	task, raw, err := r.c.GetTask(ctx, id)
	if err != nil {
		return err
	}
	r.emit(raw, func() { printTask(r.out, task) })
	return nil
}

// mutationError explains an unknown outcome instead of inviting a blind retry:
// re-running a completion that already went through would move a repeating
// task twice (design: Risks).
func (r *runner) mutationError(err error, id string) error {
	if !client.OutcomeUnknown(err) {
		return err
	}
	check := "organon task list"
	if id != "" {
		check = "organon task show " + shortID(id)
	}
	return fmt.Errorf("%v\nthe change may or may not have been applied; check with `%s` before trying again", err, check)
}

func (r *runner) taskTransition(ctx context.Context, action string, args []string) error {
	arg, err := r.oneID("task "+action, args)
	if err != nil {
		return err
	}
	id, err := resolveTask(ctx, r.c, arg)
	if err != nil {
		return err
	}
	// Read right before acting, and send exactly what was read.
	current, _, err := r.c.GetTask(ctx, id)
	if err != nil {
		return err
	}
	result, raw, err := r.c.Transition(ctx, id, action, string(current.State), current.Version)
	if err != nil {
		return r.mutationError(err, id)
	}
	r.emit(raw, func() {
		fmt.Fprintln(r.out, taskLine(result.Task))
		for _, w := range result.Warnings {
			if w == "doing_limit_exceeded" {
				fmt.Fprintln(r.err, "warning: more tasks are DOING than your doing_limit")
			} else {
				fmt.Fprintln(r.err, "warning:", w)
			}
		}
	})
	return nil
}

func (r *runner) taskEdit(ctx context.Context, args []string) error {
	fs := r.newFlags("task edit")
	values := map[string]*string{}
	for _, name := range []string{"title", "body", "priority", "tags", "scheduled", "deadline", "repeat", "warn", "repeat-to"} {
		v := new(string)
		values[name] = v
		fs.Func(name, "new value (see `organon help`)", func(s string) error { *v = s; return nil })
	}
	set := map[string]bool{}
	pos, err := parse(fs, args)
	if err != nil {
		return err
	}
	fs.Visit(func(f *flag.Flag) { set[f.Name] = true })
	if len(pos) != 1 {
		return errors.New("usage: organon task edit ID [options]")
	}
	fields := map[string]any{}
	if set["title"] {
		fields["title"] = *values["title"]
	}
	if set["body"] {
		fields["body"] = *values["body"]
	}
	for flagName, field := range map[string]string{"priority": "priority", "repeat-to": "repeat_to_state"} {
		if set[flagName] {
			if v := *values[flagName]; v == "none" {
				fields[field] = nil
			} else {
				fields[field] = v
			}
		}
	}
	if set["tags"] {
		if v := *values["tags"]; v == "none" || v == "" {
			fields["tags"] = []string{}
		} else {
			fields["tags"] = strings.Split(v, ",")
		}
	}
	warn := 0
	if set["warn"] {
		if warn, err = strconv.Atoi(*values["warn"]); err != nil || warn < 0 {
			return errors.New("--warn must be a positive number of days")
		}
	}
	repeat := *values["repeat"]
	if (set["repeat"] || set["warn"]) && !set["deadline"] && !set["scheduled"] {
		return errors.New("--repeat and --warn go with --deadline or --scheduled")
	}
	if len(fields) == 0 && !set["deadline"] && !set["scheduled"] {
		return errors.New("nothing to change")
	}
	if err := r.connect(); err != nil {
		return err
	}
	id, err := resolveTask(ctx, r.c, pos[0])
	if err != nil {
		return err
	}
	current, _, err := r.c.GetTask(ctx, id)
	if err != nil {
		return err
	}
	// The API replaces a whole date. Moving a date should not silently end a
	// series, so a repeater or warning that is not mentioned is carried over
	// from the task as just read (not computed); "--repeat none" drops it.
	for _, kind := range []string{"deadline", "scheduled"} {
		if !set[kind] {
			continue
		}
		v := *values[kind]
		if v == "none" {
			fields[kind] = nil
			continue
		}
		old := current.Scheduled
		if kind == "deadline" {
			old = current.Deadline
		}
		rep := ""
		switch {
		case set["repeat"] && (kind == "deadline" || !set["deadline"]):
			if repeat != "none" {
				rep = repeat
			}
		case old != nil && old.Repeat != nil:
			rep = *old.Repeat
		}
		w := 0
		if kind == "deadline" {
			switch {
			case set["warn"]:
				w = warn
			case old != nil && old.WarningDays != nil:
				w = *old.WarningDays
			}
		}
		fields[kind] = planning(v, rep, w)
	}
	task, raw, err := r.c.UpdateTask(ctx, id, current.Version, fields)
	if err != nil {
		return r.mutationError(err, id)
	}
	r.emit(raw, func() { printTask(r.out, task) })
	return nil
}

func (r *runner) project(ctx context.Context, sub string, args []string) error {
	switch sub {
	case "list":
		fs := r.newFlags("project list")
		if pos, err := parse(fs, args); err != nil {
			return err
		} else if len(pos) > 0 {
			return fmt.Errorf("unexpected argument %q", pos[0])
		}
		if err := r.connect(); err != nil {
			return err
		}
		list, raw, err := r.c.ListProjects(ctx)
		if err != nil {
			return err
		}
		r.emit(raw, func() { printProjects(r.out, list.Items) })
		return nil
	case "add":
		fs := r.newFlags("project add")
		var body string
		fs.StringVar(&body, "body", "", "text below the heading")
		pos, err := parse(fs, args)
		if err != nil {
			return err
		}
		if len(pos) != 1 {
			return errors.New(`usage: organon project add "TITLE"`)
		}
		if err := r.connect(); err != nil {
			return err
		}
		req := model.CreateProject{Title: pos[0]}
		if body != "" {
			req.Body = &body
		}
		p, raw, err := r.c.CreateProject(ctx, req)
		if err != nil {
			return r.mutationError(err, "")
		}
		r.emit(raw, func() { printProjects(r.out, []model.Project{p}) })
		return nil
	}
	return fmt.Errorf("unknown project command %q\n%s", sub, Usage)
}
