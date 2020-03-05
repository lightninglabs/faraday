# governator

[![MIT licensed](https://img.shields.io/badge/license-MIT-blue.svg)](https://github.com/lightninglabs/governator/blob/master/LICENSE)

The governator is an external service intended to be run in conjunction with the [lnd](https://github.com/lightningnetwork/lnd) implementation of the [Lightning Network](https://lightning.network). It queries LND for information about its existing channels and provides channel close recommendations if channels are under-performing.  

## Installation
A [Makefile](https://github.com/lightninglabs/governator/blob/master/Makefile) is provided. To install governator and al its dependencies, run:

```
go get -d github.com/lightninglabs/governator
cd $GOPATH/src/github.com/lightninglabs/governator
make && make install
```

#### Tests
To run all the unit tests in the repo, use:

```
make check
```

## Usage
Governator connects to a single instance of lnd. It requires access to a macaroon with read permissions and a valid TLS certificate. It will attempt to use the default lnd values if no command line flags are specified.
```
./governator                                    \
--macaroondir={directory containing macaroon}   \
--macaroonfile={macaroon with read permissions} \
--tlscertpath={path to lnd cert}                \
--rpserver={host:port of lnd's rpserver} 
```

By default, governator runs on mainnet. The `--testnet`, `--simnet` or `--regtest` flags can be used to run in test environments.

#### RPCServer
Governator serves requests over grpc by default on `localhost:8465`. This default can be overwritten:
```
--rpclisten={host:port to listen for requests}
```

#### Cli Tool
The RPC server can be conveniently accessed using a command line tool. 
1. Run governator as detailed above
```
./gvncli {command}
```

##### Commands
- `insights`: expose metrics gathered for one or many channels.
- `revenue`: generate a revenue report over a time period for one or many channels.
- `outliers`: close recommendations based whether channels are outliers based on a variety of metrics.
- `threshold`: close recommendations based on thresholds a variety of metrics.

#### Metrics currently tracked
The following metrics are tracked in governator and exposed via `insights` and used for `outliers` and `threshold` close recommendations.
- Uptime
- Revenue
- Total Volume
- Incoming Volume
- Outgoing Volume