#!/bin/bash

# Exit from script if error was raised.
set -e

# Set exit code of this script to the exit code of the last program to exit
# non-zero. Otherwise failures in programs that are piped into another program
# are ignored.
set -o pipefail

# Load common script code.
source util.sh

echo "Running integration test setup"

start_bitcoind

echo "Mining initial blocks"
$BTCCTL generatetoaddress 400 2N9kBLwWmJjoPxBddwR8G9hwLMrQyHum44K

start_lnds

# Run test setup code. The test TestSetup is a special test that is ran during
# the build of the docker image.
./itest.test -test.run=TestSetup

stop_all

echo "Set up complete"
