package shell

import (
	"bufio"
	"fmt"
	"io"
	"math"
	"os/exec"
	"strings"
	"text/template"

	"github.com/bartdeboer/pipeline"
	"mvdan.cc/sh/v3/shell"
)

func newScanner(r io.Reader) *bufio.Scanner {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 4096), math.MaxInt)
	return scanner
}

// Exec runs cmdLine as an external command, sending it the contents of the
// pipe as input, and produces the command's standard output (see below for
// error output). The effect of this is to filter the contents of the pipe
// through the external command.
//
// # Error handling
//
// If the command had a non-zero exit status, the pipe's error status will also
// be set to the string “exit status X”, where X is the integer exit status.
// Even in the event of a non-zero exit status, the command's output will still
// be available in the pipe. This is often helpful for debugging. However,
// because [Pipe.String] is a no-op if the pipe's error status is set, if you
// want output you will need to reset the error status before calling
// [Pipe.String].
//
// If the command writes to its standard error stream, this will also go to the
// pipe, along with its standard output. However, the standard error text can
// instead be redirected to a supplied writer, using [Pipe.WithStderr].
func Exec(cmdLine string) pipeline.ProcessE {
	return func(r io.Reader, w io.Writer, e io.Writer) error {
		args, err := shell.Fields(cmdLine, nil)
		if err != nil {
			return err
		}
		cmd := exec.Command(args[0], args[1:]...)
		cmd.Stdin = r
		cmd.Stdout = w
		cmd.Stderr = e
		if err = cmd.Start(); err != nil {
			return &pipeline.ExitError{
				Code:    1,
				Message: err.Error(),
			}
		}
		return cmd.Wait()
	}
}

// ExecForEach renders cmdLine as a Go template for each line of input, running
// the resulting command, and produces the combined output of all these
// commands in sequence. See [Pipe.Exec] for error handling details.
//
// This is mostly useful for substituting data into commands using Go template
// syntax. For example:
//
//	ListFiles("*").ExecForEach("touch {{.}}").Wait()
func ExecForEach(cmdLine string) pipeline.ProcessE {
	tpl, err := template.New("").Parse(cmdLine)
	return func(r io.Reader, w io.Writer, e io.Writer) error {
		if err != nil {
			return err
		}
		scanner := newScanner(r)
		for scanner.Scan() {
			cmdLine := new(strings.Builder)
			err := tpl.Execute(cmdLine, scanner.Text())
			if err != nil {
				return err
			}
			args, err := shell.Fields(cmdLine.String(), nil)
			if err != nil {
				return err
			}
			cmd := exec.Command(args[0], args[1:]...)
			cmd.Stdout = w
			cmd.Stderr = e
			err = cmd.Start()
			if err != nil {
				fmt.Fprintln(cmd.Stderr, err)
				continue
			}
			err = cmd.Wait()
			if err != nil {
				fmt.Fprintln(cmd.Stderr, err)
				continue
			}
		}
		return scanner.Err()
	}
}
