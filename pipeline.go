package pipeline

import (
	"bufio"
	"fmt"
	"io"
	"math"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"sync"
)

type Program interface {
	Start() error
	SetError(err error) error
	Error() error
	Exit(err error) error
	SetStdin(stdin io.Reader)
	SetStdout(stdout io.Writer)
	SetStderr(stderr io.Writer)
}

// BaseProgram is for convenience to easily create new programs
// that only require a custom StartFn function.
type BaseProgram struct {
	Stdin   io.Reader
	Stdout  io.Writer
	Stderr  io.Writer
	err     error
	StartFn func() error
}

func (b *BaseProgram) SetStdin(stdin io.Reader) {
	b.Stdin = stdin
}

func (b *BaseProgram) SetStdout(stdout io.Writer) {
	b.Stdout = stdout
}

func (b *BaseProgram) SetStderr(stderr io.Writer) {
	b.Stderr = stderr
}

func (b *BaseProgram) Start() error {
	if b.StartFn == nil {
		return nil
	}
	return b.StartFn()
}

func (b *BaseProgram) SetError(err error) error {
	if err == nil {
		return nil
	}
	// if b.Stderr != nil {
	// 	fmt.Fprintln(b.Stderr, err)
	// }
	b.err = err
	return err
}

func (b *BaseProgram) Error() error {
	return b.err
}

func (b *BaseProgram) Exit(err error) error {
	if rc, ok := b.Stdin.(io.ReadCloser); ok {
		rc.Close()
	}
	if rc, ok := b.Stdout.(io.ReadCloser); ok {
		rc.Close()
	}
	return b.SetError(err)
}

func (b *BaseProgram) Fprint(a ...any) error {
	if b.Stdout == nil {
		return nil
	}
	_, err := fmt.Fprint(b.Stdout, a...)
	if err != nil {
		return b.Exit(err)
	}
	return nil
}

func (b *BaseProgram) FprintStderr(a ...any) {
	if b.Stderr == nil {
		return
	}
	fmt.Fprint(b.Stderr, a...)
}

func NewBaseProgram() *BaseProgram {
	return &BaseProgram{
		Stdin:  os.Stdin,
		Stdout: os.Stdout,
		Stderr: os.Stderr,
	}
}

// Pipeline represents a pipeline object with an associated [ReadAutoCloser].
type Pipeline struct {
	Stdin         io.Reader
	Stdout        io.Writer
	Stderr        io.Writer
	lastPipe      *Pipe
	exitStatus    int
	exitOnError   bool
	combineOutput bool

	// because pipe stages are concurrent, protect 'err'
	mu  *sync.Mutex
	err error
}

// NewPipeline creates a new pipe with an empty reader (use [Pipe.WithReader] to
// attach another reader to it).
func NewPipeline() *Pipeline {
	return &Pipeline{
		Stdin:         os.Stdin,
		Stdout:        os.Stdout,
		Stderr:        os.Stderr,
		lastPipe:      &Pipe{},
		exitOnError:   false,
		combineOutput: false,
		mu:            new(sync.Mutex),
	}
}

// Add adds one or more programs to the pipeline.
func (p *Pipeline) Add(programs ...Program) *Pipeline {
	for _, program := range programs {
		p.Pipe(program)
	}
	return p
}

// Bytes returns the contents of the pipe as a []byte, or an error.
func (p *Pipeline) Bytes() ([]byte, error) {
	data, err := io.ReadAll(p)
	if err != nil {
		p.SetError(err)
	}
	return data, p.Error()
}

// Close closes the pipe's associated reader. This is a no-op if the reader is
// not an [io.Closer].
func (p *Pipeline) Close() error {
	return p.lastPipe.Close()
}

// Error returns any error present on the pipe, or nil otherwise.
func (p *Pipeline) Error() error {
	if p.mu == nil { // uninitialised pipe
		return nil
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.err
}

var exitStatusPattern = regexp.MustCompile(`exit status (\d+)$`)

// ExitStatus returns the integer exit status of a previous command (for
// example run by [Pipe.Exec]). This will be zero unless the pipe's error
// status is set and the error matches the pattern “exit status %d”.
func (p *Pipeline) ExitStatus() int {
	if p.Error() == nil {
		return 0
	}
	err := p.Error()
	if exitError, ok := err.(*ExitError); ok {
		return exitError.ExitCode()
	}
	if exitError, ok := err.(*exec.ExitError); ok {
		return exitError.ExitCode()
	}
	match := exitStatusPattern.FindStringSubmatch(err.Error())
	if len(match) < 2 {
		return 0
	}
	status, err := strconv.Atoi(match[1])
	if err != nil {
		// This seems unlikely, but...
		return 0
	}
	return status
}

// Int64 returns the pipe's contents as an int64, together with any error.
func (p *Pipeline) Int64() (int64, error) {
	data, err := p.Bytes()
	if err != nil {
		p.SetError(err)
	}
	strData := strings.TrimSpace(string(data))
	result, convErr := strconv.ParseInt(strData, 10, 64)
	if convErr != nil {
		return 0, convErr
	}
	return result, p.Error()
}

// Int returns the pipe's contents as an int, together with any error.
func (p *Pipeline) Int() (int, error) {
	result64, err := p.Int64()
	return int(result64), err
}

// IsClosed returns if the pipeline's last reader is closed
func (p *Pipeline) IsClosed() bool {
	if p.lastPipe == nil {
		return true
	}
	return p.lastPipe.IsClosed()
}

// Pipe (Filter) adds a new program to the pipeline and ties the output stream [io.Reader]
// of the previous program into the input stream [io.Reader] of the next program.
// It then reads the output stream [io.Writer] of the next program to be used
// for further processing within the pipeline.
//
// Pipe runs concurrently, so its goroutine will not exit until the pipe has
// been fully read. Use [Pipe.Wait] to wait for all concurrent pipes to
// complete.
func (p *Pipeline) Pipe(program Program) *Pipeline {
	if p.exitOnError && p.Error() != nil {
		return p
	}
	if program.Error() != nil {
		p.SetError(program.Error())
		if p.exitOnError {
			return p
		}
	}
	previousPipe := p.lastPipe
	nextPipe := NewPipe() // Pipe simply provides a piped writer and reader
	program.SetStdin(previousPipe)
	program.SetStdout(nextPipe)
	if p.combineOutput {
		program.SetStderr(nextPipe)
	} else {
		program.SetStderr(p.Stderr)
	}
	p.lastPipe = nextPipe
	go func() {
		defer nextPipe.Close()
		err := program.Start()
		if err != nil {
			p.SetError(err)
		}
	}()
	return p
}

// Read reads up to len(b) bytes from the pipe into b. It returns the number of
// bytes read and any error encountered. At end of file, or on a nil pipe, Read
// returns 0, [io.EOF].
func (p *Pipeline) Read(b []byte) (int, error) {
	if p == nil {
		return 0, io.EOF
	}
	return p.lastPipe.Read(b)
}

// Run adds one or more programs to the pipeline and/or runs the pipeline
// with all programs added to it.
func (p *Pipeline) Run(programs ...Program) (int64, error) {
	p.Add(programs...)
	written, err := io.Copy(p.Stdout, p)
	if err != nil {
		p.SetError(err)
	}
	return written, p.Error()
}

// Scanner (FilterScan) sends the contents of the pipe to the function filter, a line at
// a time, and produces the result. filter takes each line as a string and an
// [io.Writer] to write its output to. See [Pipe.Filter] for concurrency
// handling.
func (p *Pipeline) Scanner(filter func(string, io.Writer)) *Pipeline {
	return p.Pipe(Scanner(filter))
}

// SetCombinedOutput configures the pipeline to combine stderr with stdout
func (p *Pipeline) SetCombinedOutput(v bool) *Pipeline {
	p.combineOutput = v
	return p
}

// SetError sets the error err on the pipe.
func (p *Pipeline) SetError(err error) *Pipeline {
	if p.mu == nil { // uninitialised pipe
		return p
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.err = err
	return p
}

// SetExitOnError configures the pipeline to exit when an error has occured
func (p *Pipeline) SetExitOnError(v bool) *Pipeline {
	p.exitOnError = v
	return p
}

// Slice returns the pipe's contents as a slice of strings, one element per
// line, or an error.
//
// An empty pipe will produce an empty slice. A pipe containing a single empty
// line (that is, a single \n character) will produce a slice containing the
// empty string as its single element.
func (p *Pipeline) Slice() ([]string, error) {
	result := []string{}
	p.Scanner(func(line string, w io.Writer) {
		result = append(result, line)
	}).Wait()
	return result, p.Error()
}

// String returns the pipe's contents as a string, together with any error.
func (p *Pipeline) String() (string, error) {
	data, err := p.Bytes()
	if err != nil {
		p.SetError(err)
	}
	return string(data), p.Error()
}

// Wait reads the pipe to completion and discards the result. This is mostly
// useful for waiting until concurrent filters have completed (see
// [Pipe.Filter]).
func (p *Pipeline) Wait() *Pipeline {
	_, err := io.Copy(io.Discard, p)
	if err != nil {
		p.SetError(err)
	}
	return p
}

// WithError sets the error err on the pipe.
func (p *Pipeline) WithError(err error) *Pipeline {
	p.SetError(err)
	return p
}

// WithReader sets the pipe's input reader to r. Once r has been completely
// read, it will be closed if necessary.
func (p *Pipeline) WithReader(r io.Reader) *Pipeline {
	p.lastPipe = NewReadOnlyPipe(r)
	return p
}

// WithStderr redirects the standard error output for commands run via
// [Pipe.Exec] or [Pipe.ExecForEach] to the writer w, instead of going to the
// pipe as it normally would.
func (p *Pipeline) WithStderr(w io.Writer) *Pipeline {
	p.Stderr = w
	return p
}

// WithStdout sets the pipe's standard output to the writer w, instead of the
// default [os.Stdout].
func (p *Pipeline) WithStdout(w io.Writer) *Pipeline {
	p.Stdout = w
	return p
}

// Pipe provides a pipe that streams out what was streamed into it.
type Pipe struct {
	mu       sync.Mutex
	writer   io.WriteCloser
	reader   io.Reader
	isClosed bool
}

// NewPipe initializes a new Pipe.
func NewPipe() *Pipe {
	pr, pw := io.Pipe()
	return &Pipe{
		writer: pw,
		reader: pr,
	}
}

// NewReaderPipe initializes a read-only pipe with the provided stream.
func NewReadOnlyPipe(reader io.Reader) *Pipe {
	return &Pipe{
		reader: reader,
	}
}

// Close closes the pipe output, useful for signaling no more writes.
func (p *Pipe) Close() error {
	var err error
	if p.writer != nil {
		err = p.writer.Close()
	} else if p.reader != nil {
		if rc, ok := p.reader.(io.ReadCloser); ok {
			err = rc.Close()
		}
	}
	p.isClosed = true
	return err
}

// IsClosed returns if the pipe is closed.
func (p *Pipe) IsClosed() bool {
	return p.isClosed
}

// Read implements the io.Reader interface.
func (p *Pipe) Read(b []byte) (int, error) {
	if p.reader == nil {
		return 0, io.EOF
	}
	if p.isClosed {
		return 0, io.EOF
	}
	n, err := p.reader.Read(b)
	if err == io.EOF {
		p.Close()
	}
	return n, err
}

// Write implements the io.Writer interface.
func (p *Pipe) Write(b []byte) (int, error) {
	if p.writer == nil {
		return 0, fmt.Errorf("pipe not configured with a writer")
	}
	return p.writer.Write(b)
}

type ExitError struct {
	Code    int
	Message string
}

func (e *ExitError) Error() string {
	if e.Message != "" {
		return fmt.Sprintf("Process exited with status %d: %s", e.Code, e.Message)
	}
	return fmt.Sprintf("Process exited with status %d", e.Code)
}

func (e *ExitError) String() string {
	return e.Error()
}

func (e *ExitError) Exited() bool {
	return true
}

func (e *ExitError) ExitCode() int {
	return e.Code
}

func newScanner(r io.Reader) *bufio.Scanner {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 4096), math.MaxInt)
	return scanner
}

// Scanner is the scanner program that applies the specified filter line by line.
func Scanner(filter func(string, io.Writer)) Program {
	p := NewBaseProgram()
	p.StartFn = func() error {
		scanner := newScanner(p.Stdin)
		for scanner.Scan() {
			filter(scanner.Text(), p.Stdout)
		}
		return scanner.Err()
	}
	return p
}
