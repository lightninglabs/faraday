package itest

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"os/exec"
)

// newPrefixStdout returns a new io.Writer instance that prefixes every line and
// writes it to stdout.
func newPrefixStdout(prefix string) *prefixWriter {
	return &prefixWriter{
		writer: os.Stdout,
		prefix: prefix,
	}
}

// prefixWriter is a pass-through writer that prefixes every line with the given
// prefix.
type prefixWriter struct {
	writer io.Writer
	prefix string

	prefixWritten bool
}

// Write writes a slice of bytes to the underlying writer, after inserting
// prefixes.
func (w *prefixWriter) Write(p []byte) (int, error) {
	var b bytes.Buffer
	for _, c := range p {
		// Only write a prefix is there are more characters coming.
		// Otherwise the final output may get skewed if there are
		// multiple processes writing logs.
		if !w.prefixWritten {
			b.WriteString(fmt.Sprintf("[%v] ", w.prefix))
			w.prefixWritten = true
		}

		b.WriteByte(c)
		if c == '\n' {
			w.prefixWritten = false
		}
	}

	_, err := w.writer.Write(b.Bytes())
	if err != nil {
		return 0, err
	}

	return len(p), nil
}

// attachPrefixStdout attaches a prefixed stdout writer to the command's stdout
// and stderr output.
func attachPrefixStdout(cmd *exec.Cmd, prefix string) {
	cmd.Stdout = newPrefixStdout(prefix)
	cmd.Stderr = newPrefixStdout(prefix + "-err")
}
