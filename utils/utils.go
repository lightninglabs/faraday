package utils

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/btcsuite/btcd/chaincfg/chainhash"
	"github.com/btcsuite/btcd/wire"
)

// GetOutPointFromString gets the channel outpoint from a string.
func GetOutPointFromString(chanStr string) (*wire.OutPoint, error) {
	chanpoint := strings.Split(chanStr, ":")
	if len(chanpoint) != 2 {
		return nil, fmt.Errorf("expected 2 parts of channel point, "+
			"got: %v", len(chanpoint))
	}

	index, err := strconv.ParseInt(chanpoint[1], 10, 32)
	if err != nil {
		return nil, err
	}

	hash, err := chainhash.NewHashFromStr(chanpoint[0])
	if err != nil {
		return nil, err
	}

	return &wire.OutPoint{
		Hash:  *hash,
		Index: uint32(index),
	}, nil
}
