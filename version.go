// Copyright (c) 2013-2017 The btcsuite developers
// Copyright (c) 2015-2016 The Decred developers
// Heavily inspired by https://github.com/btcsuite/btcd/blob/master/version.go
// Copyright (C) 2015-2020 The Lightning Network Developers

package faraday

import (
	"bytes"
	"fmt"
	"strings"
)

// Commit stores the current commit hash of this build, this should be set using
// the -ldflags during compilation.
var Commit string

// semanticAlphabet is a list of all allowed characters that can be used in the
// appPreRelease part.
const semanticAlphabet = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz-"

// These constants define the application version and follow the semantic
// versioning 2.0.0 spec (http://semver.org/).
const (
	// Please update release_notes.md when updating this!
	appMajor uint = 0
	appMinor uint = 2
	appPatch uint = 15

	// appPreRelease MUST only contain characters from semanticAlphabet
	// per the semantic versioning spec.
	appPreRelease = "alpha"
)

// Version returns the application version as a properly formed string per the
// semantic versioning 2.0.0 spec (http://semver.org/).
func Version() string {
	// Start with the major, minor, and patch versions.
	version := fmt.Sprintf("%d.%d.%d", appMajor, appMinor, appPatch)

	// Append pre-release version if there is one.  The hyphen called for
	// by the semantic versioning spec is automatically appended and should
	// not be contained in the pre-release string.  The pre-release version
	// is not appended if it contains invalid characters.
	preRelease := normalizeVerString(appPreRelease)
	if preRelease != "" {
		version = fmt.Sprintf("%s-%s", version, preRelease)
	}

	// Append commit hash of current build to version.
	version = fmt.Sprintf("%s commit=%s", version, Commit)

	return version
}

// normalizeVerString returns the passed string stripped of all characters which
// are not valid according to the semantic versioning guidelines for pre-release
// version and build metadata strings.  In particular they MUST only contain
// characters in semanticAlphabet.
func normalizeVerString(str string) string {
	var result bytes.Buffer
	for _, r := range str {
		if strings.ContainsRune(semanticAlphabet, r) {
			result.WriteRune(r)
		}
	}
	return result.String()
}
