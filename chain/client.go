package chain

import (
	"io/ioutil"
	"os"
	"sync"

	"github.com/btcsuite/btcd/btcjson"
	"github.com/btcsuite/btcd/chaincfg/chainhash"
	"github.com/btcsuite/btcd/rpcclient"
)

// BitcoinClient is an interface which represents a connection to a bitcoin
// client.
type BitcoinClient interface {
	// GetTxDetail looks up a transaction.
	GetTxDetail(txHash *chainhash.Hash) (*btcjson.TxRawResult, error)
}

// BitcoinConfig defines exported config options for the connection to the
// btcd/bitcoind backend.
type BitcoinConfig struct {
	Host         string `long:"host" description:"host:port of the bitcoind/btcd instance address"`
	User         string `long:"user" description:"bitcoind/btcd user name"`
	Password     string `long:"password" description:"bitcoind/btcd password"`
	HTTPPostMode bool   `long:"httppostmode" description:"Use HTTP POST mode? bitcoind only supports this mode"`
	UseTLS       bool   `long:"usetls" description:"Use TLS to connect? bitcoind only supports non-TLS connections"`
	TLSPath      string `long:"tlspath" description:"Path to btcd tls certificate, bitcoind only supports non-TLS connections"`
}

// DefaultConfig is the default config that we use to
var DefaultConfig = &BitcoinConfig{
	Host:         "localhost:8332",
	UseTLS:       false,
	HTTPPostMode: true,
}

// bitcoinClient is a wrapper around the RPC connection to the chain backend
// and allows transactions to be queried.
type bitcoinClient struct {
	sync.Mutex

	rpcClient *rpcclient.Client

	// txDetailCache holds a cache of transactions we have previously looked
	// up.
	txDetailCache map[string]*btcjson.TxRawResult
}

// GetTxDetail fetches a single transaction from the chain and returns it
// in a format that contains more details, like the block hash it was included
// in for example.
func (c *bitcoinClient) GetTxDetail(txHash *chainhash.Hash) (
	*btcjson.TxRawResult, error) {

	c.Lock()
	cachedTx, ok := c.txDetailCache[txHash.String()]
	c.Unlock()
	if ok {
		return cachedTx, nil
	}

	tx, err := c.rpcClient.GetRawTransactionVerbose(txHash)
	if err != nil {
		return nil, err
	}

	// Do not cache the transaction if it has not confirmed yet. If we do,
	// we won't ever lookup the confirmed transaction because it is already
	// cached.
	if tx.BlockHash == "" {
		return tx, nil
	}

	c.Lock()
	c.txDetailCache[txHash.String()] = tx
	c.Unlock()

	return tx, nil
}

// NewBitcoinClient attempts to connect to a bitcoin rpcclient with the config
// provided and returns a BitcoinClient wrapper which can be used to access the
// chain connection.
func NewBitcoinClient(cfg *BitcoinConfig) (BitcoinClient, error) {
	client, err := getBitcoinConn(cfg)
	if err != nil {
		return nil, err
	}

	return &bitcoinClient{
		rpcClient:     client,
		txDetailCache: make(map[string]*btcjson.TxRawResult),
	}, nil
}

// getBitcoinConn gets a bitcoin rpc client from the config details provided.
func getBitcoinConn(cfg *BitcoinConfig) (*rpcclient.Client, error) {
	// In case we use TLS and a certificate argument is provided, we need to
	// read that file and provide it to the RPC connection as byte slice.
	var rpcCert []byte
	if cfg.UseTLS && cfg.TLSPath != "" {
		certFile, err := os.Open(cfg.TLSPath)
		if err != nil {
			return nil, err
		}
		rpcCert, err = ioutil.ReadAll(certFile)
		if err != nil {
			return nil, err
		}
		if err := certFile.Close(); err != nil {
			return nil, err
		}
	}

	// Connect to bitcoin core RPC server using HTTP POST mode.
	connCfg := &rpcclient.ConnConfig{
		Host:         cfg.Host,
		User:         cfg.User,
		Pass:         cfg.Password,
		HTTPPostMode: cfg.HTTPPostMode,
		DisableTLS:   !cfg.UseTLS,
		Certificates: rpcCert,
	}

	// Notice the notification parameter is nil since notifications are
	// not supported in HTTP POST mode.
	return rpcclient.New(connCfg, nil)
}

// Stop closes the connection to the chain backend and should always be
// called on cleanup.
func (c *bitcoinClient) Stop() {
	c.rpcClient.Shutdown()
}
