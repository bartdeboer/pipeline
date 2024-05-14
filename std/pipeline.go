package std

import (
	"io"
	"net/http"
	"regexp"
	"strings"

	"github.com/bartdeboer/pipeline"
)

// Pipeline[T any] provides a convenient embeddable implementation
// of the std programs that may be used for custom implementations
//
// Example:
//
// type CustomPipeline struct {
// 	Pipeline[*CustomPipeline]
// }
//
// func NewCustomPipeline() *CustomPipeline {
// 	p := &CustomPipeline{}
// 	p.Pipeline = NewPipeline(p)
// 	return p
// }

type Pipeline[T any] struct {
	*pipeline.Pipeline
	self T
}

func NewPipeline[T any](t T) Pipeline[T] {
	p := Pipeline[T]{
		pipeline.NewPipeline(),
		t,
	}
	return p
}

// AppendFile reads the input and appends it to the file path, creating it if necessary,
// and outputs the number of bytes successfully written
func (p *Pipeline[T]) AppendFile(path string) T {
	p.PipeE(AppendFile(path))
	return p.self
}

// Basename reads each line as a file path and outputs each path with any leading directory components removed
func (p *Pipeline[T]) Basename() T {
	p.PipeE(Basename())
	return p.self
}

// Column reads each line and outputs column col, where columns are whitespace delimited and the first column is column 1
func (p *Pipeline[T]) Column(col int) T {
	p.PipeE(Column(col))
	return p.self
}

// Concat reads each line as a file path and outputs the file contents
func (p *Pipeline[T]) Concat() T {
	p.PipeE(Concat())
	return p.self
}

// CountLines returns the number of lines of input, or an error.
func (p *Pipeline[T]) CountLines() T {
	p.PipeE(CountLines())
	return p.self
}

// Dirname reads each line as a file path and outputs each path with just the leading directory remaining
func (p *Pipeline[T]) Dirname() T {
	p.PipeE(Dirname())
	return p.self
}

// Get reads the input as the request body, sends the request and outputs the response
func (p *Pipeline[T]) Do(req *http.Request, c *http.Client) T {
	p.PipeE(Do(req, c))
	return p.self
}

// Deprecated: use [Pipe.FilterLine] or [Pipe.FilterScan] instead
func (p *Pipeline[T]) EachLine(process func(string, *strings.Builder)) T {
	p.PipeE(EachLine(process))
	return p.self
}

// Echo ignores its input and outputs string s
func (p *Pipeline[T]) Echo(s string) T {
	p.PipeE(Echo(s))
	return p.self
}

// FilterLine reads the input, calls the function filter on each line and outputs the result
func (p *Pipeline[T]) FilterLine(filter func(string) string) T {
	p.PipeE(FilterLine(filter))
	return p.self
}

// First produces only the first n lines of the input and outputs only the first n number of lines
func (p *Pipeline[T]) First(n int) T {
	p.PipeE(First(n))
	return p.self
}

// Freq reads the input and outputs only the unique lines, each prefixed with
// a frequency count, in descending numerical order
func (p *Pipeline[T]) Freq() T {
	p.PipeE(Freq())
	return p.self
}

// Get reads the input as the request body, sends a GET request and outputs the response
func (p *Pipeline[T]) Get(url string, c *http.Client) T {
	p.PipeE(Get(url, c))
	return p.self
}

// Join reads all the lines and joins them into a single space-separated string
func (p *Pipeline[T]) Join() T {
	p.PipeE(Join())
	return p.self
}

// First reads the input and outputs only the last n number of lines
func (p *Pipeline[T]) Last(n int) T {
	p.PipeE(Last(n))
	return p.self
}

// Match reads the input and outputs lines that contain the string s
func (p *Pipeline[T]) Match(s string) T {
	p.PipeE(Match(s))
	return p.self
}

// MatchRegexp reads the input and outputs lines that match the compiled regexp re
func (p *Pipeline[T]) MatchRegexp(re *regexp.Regexp) T {
	p.PipeE(MatchRegexp(re))
	return p.self
}

// Get reads the input as the request body, sends a POST request and outputs the response
func (p *Pipeline[T]) Pipe(program pipeline.ProcessE) T {
	p.Pipeline.Pipe(program)
	return p.self
}

// Get reads the input as the request body, sends a POST request and outputs the response
func (p *Pipeline[T]) PipeE(program pipeline.Process) T {
	p.Pipeline.PipeE(program)
	return p.self
}

// Get reads the input as the request body, sends a POST request and outputs the response
func (p *Pipeline[T]) Post(url string, c *http.Client) T {
	p.PipeE(Post(url, c))
	return p.self
}

// Reject reads the input and outputs lines that do not contain the string s
func (p *Pipeline[T]) Reject(s string) T {
	p.PipeE(Reject(s))
	return p.self
}

// RejectRegexp reads the input and outputs lines that do not match the compiled regexp re
func (p *Pipeline[T]) RejectRegexp(re *regexp.Regexp) T {
	p.PipeE(RejectRegexp(re))
	return p.self
}

// Replace reads the input and replaces all occurrences of the string search with the string replace
func (p *Pipeline[T]) Replace(search, replace string) T {
	p.PipeE(Replace(search, replace))
	return p.self
}

// ReplaceRegexp reads the input and replaces all matches of the compiled regexp re with the string replace
func (p *Pipeline[T]) ReplaceRegexp(re *regexp.Regexp, replace string) T {
	p.PipeE(ReplaceRegexp(re, replace))
	return p.self
}

// Scanner reads the input into a scanner, calls the function filter on each line and outputs the result
func (p *Pipeline[T]) Scanner(filter func(string, io.Writer)) T {
	p.PipeE(Scanner(filter))
	return p.self
}

// SHA256Sum reads the input and outputs the hex-encoded SHA-256 hash
func (p *Pipeline[T]) SHA256Sum() T {
	p.PipeE(SHA256Sum())
	return p.self
}

// SHA256Sum reads the input and outputs the hex-encoded SHA-256 hash of each line
func (p *Pipeline[T]) SHA256Sums() T {
	p.PipeE(SHA256Sums())
	return p.self
}

// Exec executes the command with name and arguments, using input as stdin and outputs the result
func (p *Pipeline[T]) Exec(name string, arg ...string) T {
	p.Pipe(Exec(name, arg...))
	return p.self
}

// Tee reads the input and copies it to each of the supplied writers, like Unix tee(1)
func (p *Pipeline[T]) Tee(writers ...io.Writer) T {
	p.PipeE(Tee(writers...))
	return p.self
}

func (p *Pipeline[T]) Wait() T {
	p.Pipeline.Wait()
	return p.self
}

// WriteFile reads the input and writes it to the file path, truncating it if it exists,
// and outputs the number of bytes successfully written
func (p *Pipeline[T]) WriteFile(path string) T {
	p.PipeE(WriteFile(path))
	return p.self
}

// With* functions

// WithError sets the error err on the pipe
func (p *Pipeline[T]) WithError(err error) T {
	p.Pipeline.WithError(err)
	return p.self
}

// WithReader sets the pipe's input reader to r
func (p *Pipeline[T]) WithReader(r io.Reader) T {
	p.Pipeline.WithReader(r)
	return p.self
}

// WithStderr redirects the standard error output for commands
func (p *Pipeline[T]) WithStderr(w io.Writer) T {
	p.Pipeline.WithStderr(w)
	return p.self
}

// WithStdout sets the pipe's standard output to the writer w
func (p *Pipeline[T]) WithStdout(w io.Writer) T {
	p.Pipeline.WithStdout(w)
	return p.self
}
