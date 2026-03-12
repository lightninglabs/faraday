# Faraday Architecture: Before & After

## Before: RPCServer owns everything

```mermaid
graph TD
    Main["Main()"] -->|creates| LND["lndclient.GrpcLndServices"]
    Main -->|"defer Close()"| LND
    Main -->|creates| CFG["frdrpcserver.Config<br/>(8 fields: Lnd, RPCListen,<br/>RESTListen, TLS, Macaroon...)"]
    Main -->|creates| RPC["frdrpcserver.RPCServer"]

    RPC -->|owns| GRPC["gRPC Server + Listener"]
    RPC -->|owns| REST["REST Proxy + HTTP Server"]
    RPC -->|owns| MAC["Macaroon Service + DB"]
    RPC -->|owns| START["started/stopped int32"]
    RPC -->|reads| CFG

    subgraph "litd (subserver mode)"
        LITD["LightningTerminal"] -->|creates| CFG2["frdrpcserver.Config<br/>(populated by litd)"]
        LITD -->|creates| RPC2["frdrpcserver.RPCServer"]
        RPC2 -->|"StartAsSubserver(LndServices)"| MAC2["Macaroon Service"]
        LITD -->|"calls Stop()"| RPC2
    end
```

## After: Faraday owns lifecycle, RPCServer is just handlers

```mermaid
graph TD
    Main["Main()"] -->|creates| F["faraday.Faraday"]

    F -->|owns| LND["lndclient.GrpcLndServices"]
    F -->|owns| GRPC["gRPC Server + Listener"]
    F -->|owns| REST["REST Proxy + HTTP Server"]
    F -->|owns| MAC["Macaroon Service + DB"]
    F -->|owns| BTC["BitcoinClient (optional)"]
    F -->|owns| START["started atomic.Bool"]
    F -->|embeds| RPC["frdrpcserver.RPCServer<br/>(handlers only)"]

    RPC -->|reads| CFG["frdrpcserver.Config<br/>(2 fields: Lnd, BitcoinClient)"]
    F -->|"constructs internally"| CFG

    subgraph "litd (subserver mode)"
        LITD["LightningTerminal"] -->|creates| F2["faraday.Faraday"]
        LITD -->|"StartAsSubserver(*GrpcLndServices)"| F2
        F2 -->|"initialize()"| MAC2["Macaroon Service"]
        F2 -->|embeds| RPC2["RPCServer (handlers)"]
        LITD -->|"StopAsSubserver()<br/>(does NOT close lnd)"| F2
    end
```

## Key Changes

| Concern | Before | After |
|---------|--------|-------|
| Lifecycle owner | `frdrpcserver.RPCServer` | `faraday.Faraday` |
| Config scope | `frdrpcserver.Config` (8 fields) | `faraday.Config` (full) + `frdrpcserver.Config` (2 fields) |
| lnd connection | Created/closed by `Main()` | Owned by `Faraday` (standalone) or borrowed (subserver) |
| Bitcoin client | Created by caller, passed in Config | Created inside `Faraday.initialize()` |
| Macaroon setup | Inside `RPCServer.Start()` | Inside `Faraday.initialize()` |
| RPCServer role | Lifecycle + handlers + auth | Handlers only |
| Start guard | `started`/`stopped` int32 (no restart, no reset) | `started` atomic.Bool (resets on stop and failure) |
| Stop pairing | `Stop()` for both modes | `Stop()` = standalone, `StopAsSubserver()` = shared lnd |
