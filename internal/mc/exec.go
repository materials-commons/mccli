package mc

import (
	"os/exec"
)

type ProjectExec struct {
	Command     string
	Args        []string
	ProjectRoot string
	cmd         *exec.Cmd
}

func NewProjectExec(projectRoot string, command string, args ...string) *ProjectExec {
	return &ProjectExec{
		Command:     command,
		Args:        args,
		ProjectRoot: projectRoot,
	}
}

func (e *ProjectExec) Run() error {
	e.cmd = exec.Command(e.Command, e.Args...)
	e.cmd.Dir = e.ProjectRoot
	return e.cmd.Run()
}
