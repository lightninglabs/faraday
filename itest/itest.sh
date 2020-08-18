#!/bin/bash

# Exit from script if error was raised.
set -e

# Set exit code of this script to the exit code of the last program to exit
# non-zero. Otherwise failures in programs that are piped into another program
# are ignored.
set -o pipefail

source util.sh

echo "Running integration test $@"

start_bitcoind
start_lnds

cd $WORKDIR
./itest.test -test.run=$@

stop_all

echo "Integration test $@ passed"
