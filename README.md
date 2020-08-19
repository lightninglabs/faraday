# faraday

[![MIT licensed](https://img.shields.io/badge/license-MIT-blue.svg)](https://github.com/lightninglabs/faraday/blob/master/LICENSE)

Faraday is an external service intended to be run in conjunction with the [lnd](https://github.com/lightningnetwork/lnd) implementation of the [Lightning Network](https://lightning.network). It queries LND for information about its existing channels and provides channel close recommendations if channels are under-performing. 

## LND
Note that Faraday requires lnd to be built with **all of its subservers** and requires running at least v0.10.1. Please see the [instructions](https://github.com/lightningnetwork/lnd/blob/master/docs/INSTALL.md) in the lnd repo for more detailed installation instructions. You will need to build lnd with the following command to enable all the relevant subservers:
```
make install tags="signrpc walletrpc chainrpc invoicesrpc"
```


## Installation
A [Makefile](https://github.com/lightninglabs/faraday/blob/master/Makefile) is provided. To install faraday and all its dependencies, run:

```
go get -d github.com/lightninglabs/faraday
cd $GOPATH/src/github.com/lightninglabs/faraday
make && make install
```

#### Tests
To run all the unit tests in the repo, use:

```
make check
```

## Usage
Faraday connects to a single instance of lnd. It requires access to macaroons for each subserver and a valid TLS certificate. It will attempt to use the default lnd values if no command line flags are specified.
```
./faraday                                       \
--macaroondir={directory containing macaroon}   \
--tlscertpath={path to lnd cert}                \
--rpcserver={host:port of lnd's rpcserver} 
```

By default, faraday runs on mainnet. The `--testnet`, `--simnet` or `--regtest` flags can be used to run in test environments.

#### RPCServer
Faraday serves requests over grpc by default on `localhost:8465`. This default can be overwritten:
```
--rpclisten={host:port to listen for requests}
```

#### Cli Tool
The RPC server can be conveniently accessed using a command line tool. 
1. Run faraday as detailed above
```
./frcli {command}
```

##### Commands
- `insights`: expose metrics gathered for one or many channels.
- `revenue`: generate a revenue report over a time period for one or many channels.
- `outliers`: close recommendations based whether channels are outliers based on a variety of metrics.
- `threshold`: close recommendations based on thresholds a variety of metrics.

#### Metrics currently tracked
The following metrics are tracked in faraday and exposed via `insights` and used for `outliers` and `threshold` close recommendations.
- Uptime
- Revenue
- Total Volume
- Incoming Volume
- Outgoing Volume