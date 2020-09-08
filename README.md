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

## Usage
Faraday connects to a single instance of lnd. It requires access to macaroons for each subserver and a valid TLS certificate. It will attempt to use the default lnd values if no command line flags are specified.
```
./faraday                                           \
--lnd.macaroondir={directory containing macaroon}   \
--lnd.tlscertpath={path to lnd cert}                \
--lnd.rpcserver={host:port of lnd's rpcserver} 
```

By default, faraday runs on mainnet. The `--network` flag can be used to run in
test environments.

## Transport security

The gRPC and REST connections of `faraday` are encrypted with TLS the same way
`lnd` is.

If no custom loop directory is set then the TLS certificate is stored in
`~/.faraday/<network>/tls.cert`.

The `frcli` command will pick up the file automatically on mainnet if no custom
loop directory is used. For other networks it should be sufficient to add the
`--network` flag to tell the CLI in what sub directory to look for the files.

### Chain Backend
Faraday offers node accounting services which require access to a Bitcoin node with `--txindex` set so that it can perform transaction lookup. Currently the `NodeReport` and `CloseReport` endpoints require this connection, and will fail if it is not present. This connection is *optional*, and all other endpoints will function if it is not configured. 

To connect Faraday to bitcoind:
```
--connect_bitcoin                       \
--bitcoin.host={host:port of bitcoind}  \
--bitcoin.user={bitcoind username}      \
--bitcoin.password={bitcoind  password}
```

To connect Faraday to btcd:
```
--connect_bitcoin                   \
--bitcoin.host={host:port of btcd}  \
--bitcoin.user={btcd username}      \
--bitcoin.password={btcd password}  \
--bitcoin.usetls                    \
--bitcoin.tlspath={path to btcd cert}
```

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
- `nodereport`: produce an accounting report for your node over a period of time, please see the [accounting documentation](https://github.com/lightninglabs/faraday/blob/master/accounting/docs.md) for details. *Requires chain backend*.
- `fiat`: get the USD price for an amount of Bitcoin at a given time, currently obtained from CoinCap's [historical price API](https://docs.coincap.io/?version=latest).
- `closereports`: provides a channel specific fee report, including fees paid on chain. This endpoint is currently only implemented for cooperative closes.  *Requires chain backend*.

#### Metrics currently tracked
The following metrics are tracked in faraday and exposed via `insights` and used for `outliers` and `threshold` close recommendations.
- Uptime
- Revenue
- Total Volume
- Incoming Volume
- Outgoing Volume

## Development
If you would like to contribute to Faraday, please see our [issues page](https://github.com/lightninglabs/faraday/issues) for currently open issues. If a feature that you would like to add is not covered by an existing issue, please open an issue to discuss the proposed addition. Contributions are hugely appreciated, and we will do our best to review pull requests timeously. 

### Tests
To run all the unit tests in the repo:
```
make check
```
To run Faraday's itests locally, you will need docker installed. To run all itests:
```
make itest
```

Individual itests can also be run using:
```
./run_itest.sh {test name}
```