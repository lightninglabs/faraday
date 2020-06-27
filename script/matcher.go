package script

import (
	"bytes"
	"io"

	"github.com/btcsuite/btcd/txscript"
)

// scriptMatcher is an interface implemented by functions which can be used to
// match elements in bitcoin script.
type scriptMatcher interface {
	match(r io.Reader) (bool, error)
}

// exactMatcher matches script exactly with an expected set of bytes, checking
// that we read the expected number of bytes out and that the value is exactly
// the same.
type exactMatcher struct {
	script []byte
}

// newOpcodeMatcher creates an exact matcher for opcodes. It uses script builder
// to cover any edge cases where an opcode adds more than one element to the
// stack.
func newOpcodeMatcher(opcode byte) (*exactMatcher, error) {
	builder := txscript.NewScriptBuilder()
	builder.AddOp(opcode)

	script, err := builder.Script()
	if err != nil {
		return nil, err
	}

	return &exactMatcher{
		script: script,
	}, nil
}

// newDataMatcher returns an exact matcher which will exactly match data
// elements with our script. It uses a script builder to match data elements
// because script adds different data opcodes based on length.
func newDataMatcher(data []byte) (*exactMatcher, error) {
	builder := txscript.NewScriptBuilder()
	builder.AddData(data)

	script, err := builder.Script()
	if err != nil {
		return nil, err
	}

	return &exactMatcher{
		script: script,
	}, nil
}

// match matches an expected script against our reader. It checks that we read
// the number of bytes we expect, and that the value is the same.
//
// Note: part of the scriptMatcher interface.
func (e *exactMatcher) match(r io.Reader) (bool, error) {
	scratch := make([]byte, len(e.script))

	n, err := r.Read(scratch)
	if err != nil {
		return false, err
	}

	// Check that we read our expected length from the reader, this check
	// ensures that we did not run out of bytes.
	if n != len(e.script) {
		return false, nil
	}

	// Return true if the element we read from the buffer is our script.
	return bytes.Equal(e.script, scratch), nil
}

// lengthMatcher matches a data element in bitcoin script by its length, but
// does not check the actual contents against any specific value. This type of
// matcher should be used to data elements that we know to be present, but do
// not have the exact values for them.
type lengthMatcher struct {
	length int
}

// newVariableIntMatcher returns the a matcher that will match a variable
// integer value. This is used for integer values which can have variable
// values, because script adapts the opcode used for integers based on the
// number of bytes they actually require.
func newVariableIntMatcher(value int64) (*lengthMatcher, error) {
	builder := txscript.NewScriptBuilder()
	builder.AddInt64(value)

	script, err := builder.Script()
	if err != nil {
		return nil, err
	}

	return &lengthMatcher{
		length: len(script),
	}, nil
}

// newLengthMatcher creates a length based matcher which will check that a data
// element of the expected length is present, but does not match its actual
// contents.
func newLengthMatcher(length int) (*lengthMatcher, error) {
	scratch := make([]byte, length)

	builder := txscript.NewScriptBuilder()
	builder.AddData(scratch)

	script, err := builder.Script()
	if err != nil {
		return nil, err
	}

	return &lengthMatcher{
		length: len(script),
	}, nil
}

// match matches a data element by length but does not check the actual
// contents.
//
// Note: part of the scriptMatcher interface.
func (l *lengthMatcher) match(r io.Reader) (bool, error) {
	// Create a byte slice equal to the length of our desired data element.
	scratch := make([]byte, l.length)

	n, err := r.Read(scratch)
	if err != nil {
		return false, err
	}

	// Since we don't need to check our actual value, we just check our
	// length against the number of bytes we read out.
	return n == l.length, nil
}

// matchScript attempts to match a bitcoin script with the set of required
// elements provided.
func matchScript(script []byte, scriptMatches []scriptMatcher) (bool, error) {
	r := bytes.NewReader(script)

	// Run through our set of script matchers, consuming the bytes in our
	// reader. If we do not get a match, we fail the match.
	for _, m := range scriptMatches {
		ok, err := m.match(r)
		switch err {
		// If we get an EOF error, we ran out of bytes in our script so
		// we can't possibly match it.
		case io.EOF:
			return false, nil

		// If we have no error, fallthrough to check our match.
		case nil:

		// If our error is a non-nil error that is not an EOF, we return
		// it.
		default:
			return false, err
		}

		// If we did not successfully match this item, return false.
		if !ok {
			return false, nil
		}
	}

	// Return true if there are no bytes left in our script and we have
	// matched everything.
	return r.Len() == 0, nil
}
