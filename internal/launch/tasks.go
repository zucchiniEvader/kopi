package launch

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// TasksFile is where a work tree keeps its tasks.
const TasksFile = ".vscode/tasks.json"

// Task is a task of tasks.json: a command run in a shell, or a program
// with its arguments, and the tasks to run before it.
type Task struct {
	Label     string
	Shell     bool
	Command   string
	Args      []string
	Cwd       string
	Env       map[string]string
	DependsOn []string
}

// LoadTasks reads the tasks of the work tree at root, by their labels:
// none, without an error, when it has no tasks.json.
func LoadTasks(root string) (map[string]Task, error) {
	data, err := os.ReadFile(filepath.Join(root, TasksFile))
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var file struct {
		Tasks []struct {
			Label   string          `json:"label"`
			Type    string          `json:"type"`
			Command string          `json:"command"`
			Args    json.RawMessage `json:"args"`
			Options struct {
				Cwd string            `json:"cwd"`
				Env map[string]string `json:"env"`
			} `json:"options"`
			DependsOn json.RawMessage `json:"dependsOn"`
		} `json:"tasks"`
	}
	if err := json.Unmarshal(StripJSONC(data), &file); err != nil {
		return nil, fmt.Errorf("%s: %w", TasksFile, err)
	}
	out := map[string]Task{}
	for _, t := range file.Tasks {
		args, err := stringOrList(t.Args)
		if err != nil {
			return nil, fmt.Errorf("%s: args of %q: %w", TasksFile, t.Label, err)
		}
		deps, err := stringOrList(t.DependsOn)
		if err != nil {
			return nil, fmt.Errorf("%s: dependsOn of %q: %w", TasksFile, t.Label, err)
		}
		out[t.Label] = Task{Label: t.Label, Shell: t.Type != "process", Command: t.Command, Args: args,
			Cwd: t.Options.Cwd, Env: t.Options.Env, DependsOn: deps}
	}
	return out, nil
}

// Resolve returns the task with its variables expanded, its folder
// absolute.
func (t Task) Resolve(v Vars) (Task, error) {
	var err error
	exp := func(s string) string {
		out, e := v.Expand(s)
		if e != nil && err == nil {
			err = e
		}
		return out
	}
	r := t
	r.Command = exp(t.Command)
	r.Args = make([]string, len(t.Args))
	for i, a := range t.Args {
		r.Args[i] = exp(a)
	}
	r.Cwd = exp(t.Cwd)
	switch {
	case r.Cwd == "":
		r.Cwd = v.Workspace
	case !filepath.IsAbs(r.Cwd):
		r.Cwd = filepath.Join(v.Workspace, r.Cwd)
	}
	r.Env = map[string]string{}
	for k, val := range t.Env {
		r.Env[k] = exp(val)
	}
	return r, err
}
