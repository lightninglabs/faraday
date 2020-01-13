package utils

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
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

// WriteJSONToPath marshals the interface passed in to a json struct and
// writes it to a new file located in the specified path, overwriting files
// with the same name and path, should they exist.
func WriteJSONToPath(output interface{}, path, name string) error {
	data, err := json.MarshalIndent(output, "", " ")
	if err != nil {
		return fmt.Errorf("could not marshal insights: %v",
			err)
	}

	filePath := filepath.Join(path, name)
	file, err := os.Create(filePath)
	if err != nil {
		return fmt.Errorf("could not create output file: %v",
			err)
	}

	if _, err := file.Write(data); err != nil {
		// If we could not write the data, we still need to close the
		// file. Report on both errors so that neither is silenced.
		if fileErr := file.Close(); fileErr != nil {
			return fmt.Errorf("could not write: %v, or close"+
				" file: %v", err, fileErr)
		}

		return fmt.Errorf("could not write revenue report: %v",
			err)
	}

	return file.Close()
}
