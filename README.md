## governator

[![Build Status](https://img.shields.io/travis/lightningnetwork/lnd.svg)](https://travis-ci.org/lightningnetwork/lnd)
[![MIT licensed](https://img.shields.io/badge/license-MIT-blue.svg)](https://github.com/lightningnetwork/lnd/blob/master/LICENSE)

The governator is an external service intended to be run in conjunction with the [lnd](https://github.com/lightningnetwork/lnd) implementation of the [Lightning Network](https://lightning.network). It queries LND for information about its existing channels and provides channel close recommendations if channels are under-performing.  

Future iterations of this project will automate the channel closing process. 

### Installation
A [Makefile](https://github.com/lightninglabs/governator/blob/master/Makefile) is provided. To install governator and al its dependencies, run:

```
go get -d github.com/lightninglabs/governator
cd $GOPATH/src/github.com/lightninglabs/governator
make && make install
```

##### Tests
To run all the unit tests inthe repo, use:

```
make check
```
