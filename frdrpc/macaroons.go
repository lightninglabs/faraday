package frdrpc

import (
	"context"
	"fmt"
	"io/ioutil"
	"os"
	"time"

	"github.com/lightningnetwork/lnd/lnrpc"
	"github.com/lightningnetwork/lnd/macaroons"
	"google.golang.org/grpc"
	"gopkg.in/macaroon-bakery.v2/bakery"
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
	// RequiredPermissions is a map of all faraday RPC methods and their
	// required macaroon permissions to access faraday.
	RequiredPermissions = map[string][]bakery.Op{
		"/frdrpc.FaradayServer/OutlierRecommendations": {{
			Entity: "recommendation",
			Action: "read",
		}},
		"/frdrpc.FaradayServer/ThresholdRecommendations": {{
			Entity: "recommendation",
			Action: "read",
		}},
		"/frdrpc.FaradayServer/RevenueReport": {{
			Entity: "report",
			Action: "read",
		}},
		"/frdrpc.FaradayServer/ChannelInsights": {{
			Entity: "insights",
			Action: "read",
		}},
		"/frdrpc.FaradayServer/ExchangeRate": {{
			Entity: "rates",
			Action: "read",
		}},
		"/frdrpc.FaradayServer/NodeAudit": {{
			Entity: "audit",
			Action: "read",
		}},
		"/frdrpc.FaradayServer/CloseReport": {{
			Entity: "report",
			Action: "read",
		}},
	}

	// allPermissions is the list of all existing permissions that exist
	// for faraday's RPC. The default macaroon that is created on startup
	// contains all these permissions and is therefore equivalent to lnd's
	// admin.macaroon but for faraday.
	allPermissions = []bakery.Op{{
		Entity: "recommendation",
		Action: "read",
	}, {
		Entity: "report",
		Action: "read",
	}, {
		Entity: "audit",
		Action: "read",
	}, {
		Entity: "insights",
		Action: "read",
	}, {
		Entity: "rates",
		Action: "read",
	}}

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

// startMacaroonService starts the macaroon validation service, creates or
// unlocks the macaroon database and creates the default macaroon if it doesn't
// exist yet.
func (s *RPCServer) startMacaroonService() error {
	// Create the macaroon authentication/authorization service.
	var err error
	s.macaroonService, err = macaroons.NewService(
		s.cfg.FaradayDir, faradayMacaroonLocation, false,
		macDatabaseOpenTimeout, macaroons.IPLockChecker,
	)
	if err != nil {
		return fmt.Errorf("unable to set up macaroon authentication: "+
			"%v", err)
	}

	// Try to unlock the macaroon store with the private password.
	err = s.macaroonService.CreateUnlock(&macDbDefaultPw)
	if err != nil {
		return fmt.Errorf("unable to unlock macaroon DB: %v", err)
	}

	// Create macaroon files for faraday CLI to use if they don't exist.
	if !lnrpc.FileExists(s.cfg.MacaroonPath) {
		// We don't offer the ability to rotate macaroon root keys yet,
		// so just use the default one since the service expects some
		// value to be set.
		idCtx := macaroons.ContextWithRootKeyID(
			context.Background(), macaroons.DefaultRootKeyID,
		)

		// We only generate one default macaroon that contains all
		// existing permissions (equivalent to the admin.macaroon in
		// lnd). Custom macaroons can be created through the bakery
		// RPC.
		faradayMac, err := s.macaroonService.Oven.NewMacaroon(
			idCtx, bakery.LatestVersion, nil, allPermissions...,
		)
		if err != nil {
			return err
		}
		frdMacBytes, err := faradayMac.M().MarshalBinary()
		if err != nil {
			return err
		}
		err = ioutil.WriteFile(s.cfg.MacaroonPath, frdMacBytes, 0644)
		if err != nil {
			if err := os.Remove(s.cfg.MacaroonPath); err != nil {
				log.Errorf("Unable to remove %s: %v",
					s.cfg.MacaroonPath, err)
			}
			return err
		}
	}

	return nil
}

// stopMacaroonService closes the macaroon database.
func (s *RPCServer) stopMacaroonService() error {
	return s.macaroonService.Close()
}

// macaroonInterceptor creates gRPC server options with the macaroon security
// interceptors.
func (s *RPCServer) macaroonInterceptor() []grpc.ServerOption {
	unaryInterceptor := s.macaroonService.UnaryServerInterceptor(
		RequiredPermissions,
	)
	streamInterceptor := s.macaroonService.StreamServerInterceptor(
		RequiredPermissions,
	)
	return []grpc.ServerOption{
		grpc.UnaryInterceptor(unaryInterceptor),
		grpc.StreamInterceptor(streamInterceptor),
	}
}
