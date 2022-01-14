package frdrpcserver

import (
	"time"
)

const (
	// faradayMacaroonLocation is the value we use for the faraday
	// macaroons' "Location" field when baking them.
	faradayMacaroonLocation = "faraday"

	// macDatabaseOpenTimeout is how long we wait for acquiring the lock on
	// the macaroon database before we give up with an error.
	macDatabaseOpenTimeout = time.Second * 5
)

var (

	// macDbDefaultPw is the default encryption password used to encrypt the
	// faraday macaroon database. The macaroon service requires us to set a
	// non-nil password so we set it to an empty string. This will cause the
	// keys to be encrypted on disk but won't provide any security at all as
	// the password is known to anyone.
	//
	// TODO(guggero): Allow the password to be specified by the user. Needs
	// create/unlock calls in the RPC. Using a password should be optional
	// though.
	macDbDefaultPw = []byte("")
)
