package lndwrap

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/btcsuite/btcd/wire"
	"github.com/btcsuite/btcutil"
	"github.com/lightninglabs/faraday/test"
	"github.com/lightningnetwork/lnd/chainntnfs"
	"github.com/stretchr/testify/require"
)

const (
	waitDuration = 5 * time.Second
	waitTick     = 500 * time.Millisecond
)

var (
	// Create some outpoints that we can use to test our input total
	// calculation.
	outpoint0      = &wire.OutPoint{Index: 0}
	outpoint0Value = btcutil.Amount(39)

	outpoint1      = &wire.OutPoint{Index: 1}
	outpoint1Value = btcutil.Amount(33)

	outpoint2      = &wire.OutPoint{Index: 2}
	outpoint2Value = btcutil.Amount(94)

	// Create a set of inputs that uses all three of our outpoints.
	allInputs = []*wire.TxIn{
		{
			PreviousOutPoint: *outpoint0,
		},
		{
			PreviousOutPoint: *outpoint1,
		},
		{
			PreviousOutPoint: *outpoint2,
		},
	}

	// Create transactions that produce each output at their respective
	// index within the txout set.
	createOutput0 = &wire.MsgTx{
		TxOut: []*wire.TxOut{
			{
				Value: int64(outpoint0Value),
			},
		},
	}

	createOutput1 = &wire.MsgTx{
		TxOut: []*wire.TxOut{
			{},
			{
				Value: int64(outpoint1Value),
			},
		},
	}

	createOutput2 = &wire.MsgTx{
		TxOut: []*wire.TxOut{
			{},
			{},
			{
				Value: int64(outpoint2Value),
			},
		},
	}

	// Create a spend notification for outpoint our outpoints using the
	// creating tx.
	outpoint0Spend = &chainntnfs.SpendDetail{
		SpentOutPoint: outpoint0,
		SpendingTx:    createOutput0,
	}

	outpoint1Spend = &chainntnfs.SpendDetail{
		SpentOutPoint: outpoint1,
		SpendingTx:    createOutput1,
	}

	outpoint2Spend = &chainntnfs.SpendDetail{
		SpentOutPoint: outpoint2,
		SpendingTx:    createOutput2,
	}
)

var timeout = time.Second

type testInputsCtx struct {
	t    *testing.T
	done chan struct{}

	// timeoutGetInputs is the amount of time we wait for our mocked lnd to
	// send us spend details for our tx.
	timeoutGetInputs time.Duration

	// Notification map contains a map of the outpoint of the goroutine that
	// registered a notification to a set of channels used to notify that
	// goroutine.
	notificationMap   map[wire.OutPoint]notifications
	notificationMutex sync.Mutex

	inputValue     btcutil.Amount
	executionError error
}

// notifications contains the channels we need to notify an individual goroutine.
type notifications struct {
	spendChan chan *chainntnfs.SpendDetail
	errChan   chan error
}

// newNotifications creates a set of notifications.
func newNotifications() notifications {
	return notifications{
		spendChan: make(chan *chainntnfs.SpendDetail),
		errChan:   make(chan error),
	}
}

func newTestInputsCtx(t *testing.T) *testInputsCtx {
	return &testInputsCtx{
		t:                t,
		timeoutGetInputs: time.Second * 5,
		done:             make(chan struct{}),
		notificationMap:  make(map[wire.OutPoint]notifications),
	}
}

func (c *testInputsCtx) start(inputs []*wire.TxIn) {
	// Our mocked register function will block the getInputTotal function
	// because we need to pipe values into our channels, so we run this
	// function in a goroutine with a done that closes once we've run it
	// to make sure our test does not complete before this
	go func() {
		c.inputValue, c.executionError = getInputTotal(
			context.Background(), c.register, c.timeoutGetInputs,
			inputs,
		)
		close(c.done)
	}()
}

// register is a mocked register spend function which uses the test mock's
// channels to send values to our running function.
func (c *testInputsCtx) register(_ context.Context, out *wire.OutPoint,
	_ []byte, _ int32) (chan *chainntnfs.SpendDetail, chan error,
	error) {

	c.notificationMutex.Lock()
	defer c.notificationMutex.Unlock()

	_, ok := c.notificationMap[*out]
	if ok {
		return nil, nil, fmt.Errorf("duplicate outpoint registered")
	}

	// Create a fresh set of channels for this output and return them to
	// the goroutine.
	n := newNotifications()
	c.notificationMap[*out] = n

	return n.spendChan, n.errChan, nil
}

func (c *testInputsCtx) waitForRegister(outpoint wire.OutPoint) notifications {
	// We need to wait until our outpoint is in the set of outpoints
	// that registered for spend notifications.
	registered := func() bool {
		c.notificationMutex.Lock()
		defer c.notificationMutex.Unlock()

		_, ok := c.notificationMap[outpoint]
		return ok
	}

	// Wait for our output to register its notification with the test ctx.
	require.Eventuallyf(
		c.t, registered, waitDuration, waitTick,
		"outpoint: %v never registered spend notification",
		outpoint,
	)

	// Once we know that we are registered, we get our channels from the
	// test context's map.
	c.notificationMutex.Lock()
	defer c.notificationMutex.Unlock()

	return c.notificationMap[outpoint]
}

// notifySpend sends a spend detail to our mocked notify spend response.
func (c *testInputsCtx) notifySpend(spendDetail *chainntnfs.SpendDetail) {
	// First, we need to wait for the goroutine to register a spend
	// notification.
	n := c.waitForRegister(*spendDetail.SpentOutPoint)

	// Then we can dispatch the notification to the channel that the
	// goroutine is reading from
	select {
	case n.spendChan <- spendDetail:
	case <-time.After(timeout):
		c.t.Fatal("could not send spend detail")
	}
}

// notifyError sends an error to our mocked notify spend response.
func (c *testInputsCtx) notifyError(outpoint wire.OutPoint, err error) {
	// First, we need to wait for the goroutine to register a spend
	// notification.
	n := c.waitForRegister(outpoint)

	select {
	case n.errChan <- err:
	case <-time.After(timeout):
		c.t.Fatal("could not send error")
	}
}

// assertFinished asserts that our function exited, and checks that we have the
// amount and error we expect.
func (c *testInputsCtx) assertFinished(value btcutil.Amount, err error) {
	// Wait for our input total function to exit.
	<-c.done

	// Expect our function to have exited with no errors.
	require.Equal(c.t, err, c.executionError)

	// Expect the value returned to be the single value we got.
	require.Equal(c.t, value, c.inputValue)
}

// TestSuccessfulGetInputs tests getting the total input value for three inputs
// that all have successful spend notifications returned by lnd.
func TestSuccessfulGetInputs(t *testing.T) {
	defer test.Guard(t)()

	c := newTestInputsCtx(t)
	c.start(allInputs)

	// Notify spends for all of our outpoints.
	c.notifySpend(outpoint0Spend)
	c.notifySpend(outpoint1Spend)
	c.notifySpend(outpoint2Spend)

	// Assert that we return the total value for our outputs.
	total := outpoint0Value + outpoint1Value + outpoint2Value
	c.assertFinished(total, nil)
}

// TestTimedOutGetInputs tests the case where we do not receive a spend
// notification from lnd in time, so we fail our lookup.
func TestTimedOutGetInputs(t *testing.T) {
	defer test.Guard(t)()

	// Create a our test context, but set our timeout to 0. This allows us
	// to test the timeout case without having to wait.
	c := newTestInputsCtx(t)
	c.timeoutGetInputs = 0

	c.start(allInputs)
	c.assertFinished(0, ErrNotificationTimeout)
}

// TestFailingGetInputs tests the case where our spend notification fails, so
//
func TestFailingGetInputs(t *testing.T) {
	defer test.Guard(t)()

	c := newTestInputsCtx(t)
	c.start(allInputs)

	err := fmt.Errorf("outpoint does not exist")

	// Notify spends for one of our outpoints, then notify an error. One of
	// our goroutines will have exited, one will have errored our and one
	// will still be running. This tests the case where we instruction
	// goroutines that are still executing to give up because we have an
	// error.
	c.notifySpend(outpoint0Spend)
	c.notifyError(*outpoint1, err)

	// Assert that get an error.
	c.assertFinished(0, err)
}
