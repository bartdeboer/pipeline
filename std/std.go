package std

import (
	"bufio"
	"container/ring"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"math"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/bartdeboer/pipeline"
)

// File reads from the file path.
func File(path string) pipeline.ProcessE {
	return func(_ io.Reader, w io.Writer, e io.Writer) error {
		f, err := os.Open(path)
		if err != nil {
			fmt.Fprintln(e, err)
			return err
		}
		defer f.Close()
		_, err = io.Copy(w, f)
		if err != nil {
			fmt.Fprintln(e, err)
			return err
		}
		return nil
	}
}

// FindFiles creates a pipe listing all the files in the directory dir and its
// subdirectories recursively, one per line, like Unix find(1). If dir doesn't
// exist or can't be read, the pipe's error status will be set.
//
// Each line of the output consists of a slash-separated path, starting with
// the initial directory. For example, if the directory looks like this:
//
//	test/
//	        1.txt
//	        2.txt
//
// the pipe's output will be:
//
//	test/1.txt
//	test/2.txt
func FindFiles(dir string) pipeline.Process {
	return func(_ io.Reader, w io.Writer) error {
		err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}
			if !info.IsDir() {
				_, err = fmt.Fprint(w, path+"\n")
				if err != nil {
					return err
				}
			}
			return nil
		})
		return err
	}
}

// Get makes an HTTP GET request to url, sending the contents of the pipe as
// the request body, and produces the server's response. See [Pipe.Do] for how
// the HTTP response status is interpreted.
func Get(url string, c *http.Client) pipeline.Process {
	return func(r io.Reader, w io.Writer) error {
		req, err := http.NewRequest(http.MethodGet, url, r)
		if err != nil {
			return err
		}
		doFunc := Do(req, c)
		return doFunc(io.NopCloser(r), w) // Wrap in NopCloser if the reader does not implement io.Closer
	}
}

// IfExists tests whether path exists, and creates a pipe whose error status
// reflects the result. If the file doesn't exist, the pipe's error status will
// be set, and if the file does exist, the pipe will have no error status. This
// can be used to do some operation only if a given file exists:
//
//	IfExists("/foo/bar").Exec("/usr/bin/something")
func IfExists(path string) pipeline.Process {
	return func(_ io.Reader, _ io.Writer) error {
		_, err := os.Stat(path)
		return err
	}
}

// ListFiles creates a pipe containing the files or directories specified by
// path, one per line. path can be a glob expression, as for [filepath.Match].
// For example:
//
//	ListFiles("/data/*").Stdout()
//
// ListFiles does not recurse into subdirectories; use [FindFiles] instead.
func ListFiles(path string) pipeline.Process {
	return func(_ io.Reader, w io.Writer) error {
		if strings.ContainsAny(path, "[]^*?\\{}!") {
			fileNames, err := filepath.Glob(path)
			if err != nil {
				return err
			}
			_, err = fmt.Fprint(w, strings.Join(fileNames, "\n")+"\n")
			return err
		}
		entries, err := os.ReadDir(path)
		if err != nil {
			// Check for the case where the path matches exactly one file
			s, err := os.Stat(path)
			if err != nil {
				return err
			}
			if !s.IsDir() {
				_, err = fmt.Fprint(w, path)
				return err
			}
			return err
		}
		for _, e := range entries {
			_, err = fmt.Fprint(w, filepath.Join(path, e.Name())+"\n")
		}
		return err
	}
}

// Post makes an HTTP POST request to url, using the contents of the pipe as
// the request body, and produces the server's response. See [Pipe.Do] for how
// the HTTP response status is interpreted.
func Post(url string, c *http.Client) pipeline.Process {
	return func(r io.Reader, w io.Writer) error {
		req, err := http.NewRequest(http.MethodPost, url, r)
		if err != nil {
			return err
		}
		doFunc := Do(req, c)
		return doFunc(io.NopCloser(r), w) // Wrap in NopCloser if the reader does not implement io.Closer
	}
}

// Stdin reads from [os.Stdin].
func Stdin() pipeline.Process {
	return func(_ io.Reader, w io.Writer) error {
		_, err := io.Copy(w, os.Stdin)
		return err
	}
}

// AppendFile appends the contents of the pipe to the file path, creating it if
// necessary, and returns the number of bytes successfully written, or an
// error.
func AppendFile(path string) pipeline.Process {
	return func(r io.Reader, w io.Writer) error {
		written, err := writeOrAppendFile(r, path, os.O_APPEND|os.O_CREATE|os.O_WRONLY)
		fmt.Fprint(w, written)
		return err
	}
}

// Basename reads paths from the pipe, one per line, and removes any leading
// directory components from each. So, for example, /usr/local/bin/foo would
// become just foo. This is the complementary operation to [Pipe.Dirname].
//
// If any line is empty, Basename will transform it to a single dot. Trailing
// slashes are removed. The behaviour of Basename is the same as
// [filepath.Base] (not by coincidence).
func Basename() func(r io.Reader, w io.Writer) error {
	return FilterLine(filepath.Base)
}

// Column produces column col of each line of input, where the first column is
// column 1, and columns are delimited by Unicode whitespace. Lines containing
// fewer than col columns will be skipped.
func Column(col int) pipeline.Process {
	return Scanner(func(line string, w io.Writer) {
		columns := strings.Fields(line)
		if col > 0 && col <= len(columns) {
			fmt.Fprintln(w, columns[col-1])
		}
	})
}

// Concat reads paths from the pipe, one per line, and produces the contents of
// all the corresponding files in sequence. If there are any errors (for
// example, non-existent files), these will be ignored, execution will
// continue, and the pipe's error status will not be set.
//
// This makes it convenient to write programs that take a list of paths on the
// command line. For example:
//
//	script.Args().Concat().Stdout()
//
// The list of paths could also come from a file:
//
//	script.File("filelist.txt").Concat()
//
// Or from the output of a command:
//
//	script.Exec("ls /var/app/config/").Concat().Stdout()
//
// Each input file will be closed once it has been fully read. If any of the
// files can't be opened or read, Concat will simply skip these and carry on,
// without setting the pipe's error status. This mimics the behaviour of Unix
// cat(1).
func Concat() pipeline.Process {
	return Scanner(func(line string, w io.Writer) {
		input, err := os.Open(line)
		if err != nil {
			return
		}
		defer input.Close()
		io.Copy(w, input)
	})
}

// CountLines returns the number of lines of input, or an error.
func CountLines() pipeline.Process {
	lines := 0
	return func(r io.Reader, w io.Writer) error {
		scanner := newScanner(r)
		for scanner.Scan() {
			lines++
		}
		fmt.Fprintln(w, lines)
		return scanner.Err()
	}
}

// Dirname reads paths from the pipe, one per line, and produces only the
// parent directories of each path. For example, /usr/local/bin/foo would
// become just /usr/local/bin. This is the complementary operation to
// [Pipe.Basename].
//
// If a line is empty, Dirname will transform it to a single dot. Trailing
// slashes are removed, unless Dirname returns the root folder. Otherwise, the
// behaviour of Dirname is the same as [filepath.Dir] (not by coincidence).
func Dirname() pipeline.Process {
	return Scanner(func(line string, w io.Writer) {
		// filepath.Dir() does not handle trailing slashes correctly
		if len(line) > 1 && strings.HasSuffix(line, "/") {
			line = line[:len(line)-1]
		}
		dirname := filepath.Dir(line)
		// filepath.Dir() does not preserve a leading './'
		if strings.HasPrefix(line, "./") {
			dirname = "./" + dirname
		}
		fmt.Fprintln(w, dirname)
	})
}

// Do performs the HTTP request req using the pipe's configured HTTP client, as
// set by [Pipe.WithHTTPClient], or [http.DefaultClient] otherwise. The
// response body is streamed concurrently to the pipe's output. If the response
// status is anything other than HTTP 200-299, the pipe's error status is set.
func Do(req *http.Request, c *http.Client) pipeline.Process {
	return func(r io.Reader, w io.Writer) error {
		resp, err := c.Do(req)
		if err != nil {
			return err
		}
		defer resp.Body.Close()
		_, err = io.Copy(w, resp.Body)
		if err != nil {
			return err
		}
		// Any HTTP 2xx status code is considered okay
		if resp.StatusCode/100 != 2 {
			return fmt.Errorf("unexpected HTTP response status: %s", resp.Status)
		}
		return nil
	}
}

// EachLine calls the function process on each line of input, passing it the
// line as a string, and a [*strings.Builder] to write its output to.
//
// Deprecated: use [Pipe.FilterLine] or [Pipe.FilterScan] instead, which run
// concurrently and don't do unnecessary reads on the input.
func EachLine(process func(string, *strings.Builder)) pipeline.Process {
	return func(r io.Reader, w io.Writer) error {
		scanner := newScanner(r)
		output := new(strings.Builder)
		for scanner.Scan() {
			process(scanner.Text(), output)
		}
		fmt.Fprint(w, output.String())
		return scanner.Err()
	}
}

// Echo sets the pipe's reader to one that produces the string s, detaching any
// existing reader without draining or closing it.
func Echo(s string) pipeline.Process {
	return func(_ io.Reader, w io.Writer) error {
		_, err := fmt.Fprint(w, s)
		return err
	}
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
func Exec(name string, arg ...string) pipeline.ProcessE {
	return func(r io.Reader, w io.Writer, e io.Writer) error {
		cmd := exec.Command(name, arg...)
		cmd.Stdin = r
		cmd.Stdout = w
		cmd.Stderr = e
		if err := cmd.Start(); err != nil {
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
func ExecForEach(name string, arg ...string) pipeline.ProcessE {
	return func(r io.Reader, w io.Writer, e io.Writer) error {
		scanner := newScanner(r)
		for scanner.Scan() {
			cmd := exec.Command(name, arg...)
			cmd.Stdout = w
			cmd.Stderr = e
			err := cmd.Start()
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

// FilterLine sends the contents of the pipe to the function filter, a line at
// a time, and produces the result. filter takes each line as a string and
// returns a string as its output. See [Pipe.Filter] for concurrency handling.
func FilterLine(filter func(string) string) pipeline.Process {
	return Scanner(func(line string, w io.Writer) {
		fmt.Fprintln(w, filter(line))
	})
}

// First produces only the first n lines of the pipe's contents, or all the
// lines if there are less than n. If n is zero or negative, there is no output
// at all.
func First(n int) pipeline.Process {
	return func(r io.Reader, w io.Writer) error {
		scanner := newScanner(r)
		for i := 0; i < n && scanner.Scan(); i++ {
			_, err := fmt.Fprintln(w, scanner.Text())
			if err != nil {
				return err
			}
		}
		return scanner.Err()
	}
}

// Freq produces only the unique lines from the pipe's contents, each prefixed
// with a frequency count, in descending numerical order (most frequent lines
// first). Lines with equal frequency will be sorted alphabetically.
//
// For example, we could take a common shell pipeline like this:
//
//	sort input.txt |uniq -c |sort -rn
//
// and replace it with:
//
//	File("input.txt").Freq().Stdout()
//
// Or to get only the ten most common lines:
//
//	File("input.txt").Freq().First(10).Stdout()
//
// Like Unix uniq(1), Freq right-justifies its count values in a column for
// readability, padding with spaces if necessary.
func Freq() pipeline.Process {
	freq := map[string]int{}
	type frequency struct {
		line  string
		count int
	}
	return func(r io.Reader, w io.Writer) error {
		scanner := newScanner(r)
		for scanner.Scan() {
			freq[scanner.Text()]++
		}
		freqs := make([]frequency, 0, len(freq))
		max := 0
		for line, count := range freq {
			freqs = append(freqs, frequency{line, count})
			if count > max {
				max = count
			}
		}
		sort.Slice(freqs, func(i, j int) bool {
			x, y := freqs[i].count, freqs[j].count
			if x == y {
				return freqs[i].line < freqs[j].line
			}
			return x > y
		})
		fieldWidth := len(strconv.Itoa(max))
		for _, item := range freqs {
			fmt.Fprintf(w, "%*d %s\n", fieldWidth, item.count, item.line)
		}
		return nil
	}
}

// Join joins all the lines in the pipe's contents into a single
// space-separated string, which will always end with a newline.
func Join() pipeline.Process {
	return func(r io.Reader, w io.Writer) error {
		scanner := newScanner(r)
		first := true
		for scanner.Scan() {
			if !first {
				fmt.Fprint(w, " ")
			}
			line := scanner.Text()
			fmt.Fprint(w, line)
			first = false
		}
		fmt.Fprintln(w)
		return scanner.Err()
	}
}

// Last produces only the last n lines of the pipe's contents, or all the lines
// if there are less than n. If n is zero or negative, there is no output at
// all.
func Last(n int) pipeline.Process {
	return func(r io.Reader, w io.Writer) error {
		if n <= 0 {
			return nil
		}
		scanner := newScanner(r)
		input := ring.New(n)
		for scanner.Scan() {
			input.Value = scanner.Text()
			input = input.Next()
		}
		input.Do(func(p interface{}) {
			if p != nil {
				fmt.Fprintln(w, p)
			}
		})
		return scanner.Err()
	}
}

// Match produces only the input lines that contain the string s.
func Match(s string) pipeline.Process {
	return Scanner(func(line string, w io.Writer) {
		if strings.Contains(line, s) {
			fmt.Fprintln(w, line)
		}
	})
}

// MatchRegexp produces only the input lines that match the compiled regexp re.
func MatchRegexp(re *regexp.Regexp) pipeline.Process {
	return Scanner(func(line string, w io.Writer) {
		if re.MatchString(line) {
			fmt.Fprintln(w, line)
		}
	})
}

// Reject produces only lines that do not contain the string s.
func Reject(s string) pipeline.Process {
	return Scanner(func(line string, w io.Writer) {
		if !strings.Contains(line, s) {
			fmt.Fprintln(w, line)
		}
	})
}

// RejectRegexp produces only lines that don't match the compiled regexp re.
func RejectRegexp(re *regexp.Regexp) pipeline.Process {
	return Scanner(func(line string, w io.Writer) {
		if !re.MatchString(line) {
			fmt.Fprintln(w, line)
		}
	})
}

// Replace replaces all occurrences of the string search with the string
// replace.
func Replace(search, replace string) pipeline.Process {
	return Scanner(func(line string, w io.Writer) {
		fmt.Fprintln(w, strings.ReplaceAll(line, search, replace))
	})
}

// ReplaceRegexp replaces all matches of the compiled regexp re with the string
// replace. $x variables in the replace string are interpreted as by
// [regexp#Regexp.Expand]; for example, $1 represents the text of the first submatch.
func ReplaceRegexp(re *regexp.Regexp, replace string) pipeline.Process {
	return Scanner(func(line string, w io.Writer) {
		fmt.Fprintln(w, re.ReplaceAllString(line, replace))
	})
}

func Scanner(filter func(string, io.Writer)) pipeline.Process {
	return pipeline.Scanner(filter)
}

// SHA256Sum returns the hex-encoded SHA-256 hash of the entire contents of the
// pipe, or an error.
func SHA256Sum() pipeline.Process {
	return func(r io.Reader, w io.Writer) error {
		hasher := sha256.New()
		_, err := io.Copy(hasher, r)
		if err != nil {
			return err
		}
		_, err = fmt.Fprint(w, hex.EncodeToString(hasher.Sum(nil)))
		return err
	}
}

// SHA256Sums reads paths from the pipe, one per line, and produces the
// hex-encoded SHA-256 hash of each corresponding file, one per line. Any files
// that cannot be opened or read will be ignored.
func SHA256Sums() pipeline.Process {
	return Scanner(func(line string, w io.Writer) {
		f, err := os.Open(line)
		if err != nil {
			return // skip unopenable files
		}
		defer f.Close()
		h := sha256.New()
		_, err = io.Copy(h, f)
		if err != nil {
			return // skip unreadable files
		}
		fmt.Fprintln(w, hex.EncodeToString(h.Sum(nil)))
	})
}

// Stdout copies the pipe's contents to its configured standard output (using
// [Pipe.WithStdout]), or to [os.Stdout] otherwise, and returns the number of
// bytes successfully written, together with any error.
func Stdout() pipeline.Process {
	return func(r io.Reader, _ io.Writer) error {
		_, err := io.Copy(os.Stdout, r)
		return err
	}
}

// Tee copies the pipe's contents to each of the supplied writers, like Unix
// tee(1). If no writers are supplied, the default is the pipe's standard
// output.
func Tee(writers ...io.Writer) pipeline.Process {
	return func(r io.Reader, w io.Writer) error {
		allWriters := make([]io.Writer, len(writers), len(writers)+1)
		copy(allWriters, writers)
		if w != nil {
			allWriters = append(allWriters, w)
		}
		var teeWriter io.Writer
		if len(allWriters) == 1 {
			teeWriter = allWriters[0]
		} else {
			teeWriter = io.MultiWriter(allWriters...)
		}
		teeReader := io.TeeReader(r, teeWriter)
		_, err := io.Copy(io.Discard, teeReader)
		return err
	}
}

// Wait reads the pipe to completion and discards the result. This is mostly
// useful for waiting until concurrent filters have completed (see
// [Pipe.Filter]).
func Wait() pipeline.Process {
	return func(r io.Reader, w io.Writer) error {
		fmt.Println("Start program wait")
		_, err := io.Copy(w, r)
		fmt.Printf("Wait error: %v\n", err)
		fmt.Println("End program Wait")
		return err
	}
}

// WriteFile writes the pipe's contents to the file path, truncating it if it
// exists, and returns the number of bytes successfully written, or an error.
func WriteFile(path string) pipeline.Process {
	return func(r io.Reader, w io.Writer) error {
		written, err := writeOrAppendFile(r, path, os.O_RDWR|os.O_CREATE|os.O_TRUNC)
		fmt.Fprint(w, written)
		return err
	}
}

func writeOrAppendFile(r io.Reader, path string, mode int) (int64, error) {
	out, err := os.OpenFile(path, mode, 0o666)
	if err != nil {
		return 0, err
	}
	defer out.Close()
	return io.Copy(out, r)
}

func newScanner(r io.Reader) *bufio.Scanner {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 4096), math.MaxInt)
	return scanner
}
