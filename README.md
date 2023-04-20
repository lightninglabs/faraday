# faraday

[![MIT licensed](https://img.shields.io/badge/license-MIT-blue.svg)](https://github.com/lightninglabs/faraday/blob/master/LICENSE)

Faraday is a suite of tools built to help node operators and businesses run [lnd](https://github.com/lightningnetwork/lnd), the leading implementation of the [Lightning Network](https://github.com/lightningnetwork/lightning-rfc). Faraday’s tools decrease the operational overhead of running a Lightning node and make it easier to build businesses on Lightning. The current features in the Faraday suite provide insight into node channel performance and support for accounting with both on-chain and off-chain reports for lnd. 
## LND
Note that Faraday requires lnd to be built with **all of its subservers** and requires running at least v0.11.1. Download the [official release binary](https://github.com/lightningnetwork/lnd/releases/tag/v0.11.1-beta) or see the [instructions](https://github.com/lightningnetwork/lnd/blob/master/docs/INSTALL.md) in the lnd repo for more detailed installation instructions. If you choose to build lnd from source, following command to enable all the relevant subservers:

```shell
make install tags="signrpc walletrpc chainrpc invoicesrpc"
```


## Installation
A [Makefile](https://github.com/lightninglabs/faraday/blob/master/Makefile) is provided. To install faraday and all its dependencies, run:

```shell
git clone https://github.com/lightninglabs/faraday.git
cd faraday
make && make install
```

## Usage
Faraday connects to a single instance of lnd. It requires access to `lnd`'s
`admin.macaroon` (or a custom scoped macaroon, see below) and a valid TLS
certificate. It will attempt to use the default `lnd` values if no command line
flags are specified.

```shell
./faraday                                                   \
--lnd.macaroonpath={full path to lnd's admin.macaroon}   \
--lnd.tlscertpath={path to lnd cert}                        \
--lnd.rpcserver={host:port of lnd's rpcserver} 
```

By default, faraday runs on mainnet. The `--network` flag can be used to run in
test environments.

### Baking a custom macaroon for Faraday

Faraday needs to derive a shared key with `lnd` to create an encryption password
for its macaroon database. That's why on top of the permissions in the
`readonly.macaroon` the `uri:/signrpc.Signer/DeriveSharedKey` is also required.
A custom scoped macaroon just for Faraday can be baked with:

```shell
lncli bakemacaroon onchain:read offchain:read address:read peers:read info:read invoices:read uri:/signrpc.Signer/DeriveSharedKey
```

## Authentication and transport security

The gRPC and REST connections of `faraday` are encrypted with TLS and secured
with macaroon authentication the same way `lnd` is.

If no custom faraday directory is set then the TLS certificate is stored in
`~/.faraday/<network>/tls.cert` and the base macaroon in
`~/.faraday/<network>/faraday.macaroon`.

The `frcli` command will pick up these file automatically on mainnet if no
custom faraday directory is used. For other networks it should be sufficient to
add the `--network` flag to tell the CLI in what sub directory to look for the
files.

For more information on macaroons,
[see the macaroon documentation of lnd.](https://github.com/lightningnetwork/lnd/blob/master/docs/macaroons.md)

**NOTE**: Faraday's macaroons are independent from `lnd`'s. The same macaroon
cannot be used for both `faraday` and `lnd`.

### Chain Backend
Faraday offers node accounting services which require access to a Bitcoin node with `--txindex` set so that it can perform transaction lookup. Currently the `CloseReport` endpoint requires this connection, and will fail if it is not present. It is *strongly recommended* to provide this connection when utilizing the `NodeAudit` endpoint, but it is not required. This connection is *optional*, and all other endpoints will function if it is not configured. 

To connect Faraday to bitcoind:
```text
--connect_bitcoin                       \
--bitcoin.host={host:port of bitcoind}  \
--bitcoin.user={bitcoind username}      \
--bitcoin.password={bitcoind  password}
```

To connect Faraday to btcd:
```text
--connect_bitcoin                   \
--bitcoin.host={host:port of btcd}  \
--bitcoin.user={btcd username}      \
--bitcoin.password={btcd password}  \
--bitcoin.usetls                    \
--bitcoin.tlspath={path to btcd cert}
```

#### RPCServer
Faraday serves requests over grpc by default on `localhost:8465`. This default can be overwritten:
```text
--rpclisten={host:port to listen for requests}
```

#### Cli Tool
The RPC server can be conveniently accessed using a command line tool. 
1. Run faraday as detailed above
```shell
./frcli {command}
```

##### Commands
- `insights`: expose metrics gathered for one or many channels.
- `revenue`: generate a revenue report over a time period for one or many channels.
- `outliers`: close recommendations based whether channels are outliers based on a variety of metrics.
- `threshold`: close recommendations based on thresholds a variety of metrics.
- `audit`: produce an accounting report for your node over a period of time, please see the [accounting documentation](https://github.com/lightninglabs/faraday/blob/master/docs/accounting.md) for details. *Chain backend strongly recommended*, fee entries for channel closes and sweeps will be *missing* if a chain connection is not provided.
- `fiat`: get the USD price for an amount of Bitcoin at a given time, currently obtained from CoinCap's [historical price API](https://docs.coincap.io/?version=latest).
- `closereport`: provides a channel specific fee report, including fees paid on chain. This endpoint is currently only implemented for cooperative closes.  *Requires chain backend*.

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
```shell
make check
```
To run Faraday's itests locally, you will need docker installed. To run all itests:
```shell
make itest
```

Individual itests can also be run using:
```shell
./run_itest.sh {test name}
```
