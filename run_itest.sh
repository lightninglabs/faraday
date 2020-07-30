#!/bin/bash
set -e

TESTCASE=$1

# Build faraday for transfer to docker container. Without CGO_ENABLED=0,
# and the correct OS/ARCH set, the binary will not run in an alpine environment.
# Copy the binary to the itest directory which serves as the docker build
# context.
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 make build
cp faraday itest

# Build itest executable for transfer to docker container.
cd itest
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go test -c -tags itest

# Build itest image.
docker build -t faraday .

# If a command line parameter is present, only execute that test and output
# directly to stdout.
if [[ $TESTCASE ]]; then
        docker run --rm faraday $TESTCASE
        exit
fi

# Verify that the image correctly bubbles up a failed test.
if docker run --rm faraday TestFail; then
        echo "Always failing test not failed"
        exit 1
fi

# Clear log files from previous run.
rm *.log || true

run_test() {
        echo "$1 started"
        LOG_FILE="$1.log"
        
        # Start a new container to only run this specific test case. Direct all
        # output to a test-specific log file.
        docker run --rm faraday $1 > $LOG_FILE 2>&1
        TEST_EXIT_CODE=$?

        if [[ $TEST_EXIT_CODE -eq 0 ]]; then
                echo "$1 passed"
        else
                echo "$1 failed"
        fi

        return $TEST_EXIT_CODE
}

# Export the function so that it is available with xargs below.
export -f run_test

# Query list of tests. Exclude special setup and fail test cases which ran
# already.
TESTS=$(./itest.test -test.list . | grep -vxE "TestSetup|TestFail")

# Run test cases in parallel with a maximum degree.
MAX_PARALLEL_TESTS=4
echo "$TESTS" | xargs -P $MAX_PARALLEL_TESTS -I {} bash -c "run_test {}"

# Dump log files so that they become visible in CI.
echo "$TESTS" | xargs -I {} bash -c "echo ------ {} ------ && cat {}.log && echo"
