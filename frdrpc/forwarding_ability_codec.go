package frdrpc

import (
	"bytes"
	"encoding/hex"
	"errors"
	"fmt"
	"math"
	"sort"
	"strings"
)

// maxPackedPeers is the largest peer set a response can address. packed_idx
// splits a uint32 into two 16-bit indices (in << 16 | out), so each direction
// can reference at most 65535 distinct peers.
const maxPackedPeers = 1<<16 - 1

// ForwardingAbility is a client-facing mirror of the raw forwarding facts for
// one direction of a peer pair over the analysis window. Derived rates and
// categories are left to the consumer.
type ForwardingAbility struct {
	// EffectiveUptimeS is the seconds the pair held at least the requested
	// liquidity floor of directional forwardable liquidity over the window.
	// The value is whole seconds: sub-second uptime floors to zero, so a
	// pair that forwarded volume over a fleeting qualifying window can
	// report a zero uptime alongside a non-zero ForwardedMsat.
	EffectiveUptimeS uint64

	// ForwardedMsat is the total successfully forwarded amount over the
	// window, in millisatoshis.
	ForwardedMsat int64

	// FeeMsat is the total fee earned on the pair's forwards over the
	// window, in millisatoshis. A single forward routinely earns less than
	// one satoshi, so a satoshi granular sum would floor a low-fee pair's
	// earnings to zero.
	FeeMsat int64

	// Forwards is how many successful forwards the pair carried. It decides
	// whether the pair forwarded at all, since the fee total can be zero
	// for one that did under a zero-fee policy.
	Forwards int64
}

// abilityTier classifies how a pair is encoded: a full entry, a single "up but
// idle" bit, or omitted entirely.
type abilityTier int

const (
	// tierAbsent omits the pair: it carried no forward and did not hold
	// enough uptime to clear the threshold. Consumers treat absence as
	// zero.
	tierAbsent abilityTier = iota

	// tierBit flags the pair in the up-but-idle bitmask: it held at least
	// the uptime threshold but carried no forward.
	tierBit

	// tierEntry emits a full entry carrying the pair's exact uptime,
	// forwarded volume, fees and forward count. Reserved for pairs that
	// actually forwarded.
	tierEntry
)

// tier decides how a pair is encoded given the minimum qualifying uptime in
// seconds. Forwarding always wins, so a pair that forwarded keeps its exact
// facts even if its uptime is below the threshold; otherwise the pair is
// compacted to a bit when it was up enough, and dropped when it was not.
//
// The count decides whether the pair forwarded, not the sums: a zero-fee policy
// leaves the fee total at zero for a pair that did, and demoting it to a bit
// would record it as idle.
func (a ForwardingAbility) tier(minUptimeS uint64) abilityTier {
	switch {
	case a.Forwards > 0:
		return tierEntry

	case a.EffectiveUptimeS >= minUptimeS:
		return tierBit

	default:
		return tierAbsent
	}
}

// MinQualifyingUptime converts an uptime fraction threshold into the smallest
// whole-second uptime that clears it over the given window. It is the single
// source of truth shared by the encoder (to bucket pairs) and the server (to
// apply the node-down guard), so the two cannot drift. A pair clears the
// threshold when EffectiveUptimeS >= the returned value, matching the "at least
// the threshold fraction" contract. The result is floored at one second so a
// pair with zero uptime is never treated as up. A non-positive window admits
// nothing.
func MinQualifyingUptime(threshold float64, windowSeconds int64) uint64 {
	if windowSeconds <= 0 {
		return math.MaxUint64
	}

	v := uint64(math.Ceil(threshold * float64(windowSeconds)))
	if v < 1 {
		v = 1
	}

	return v
}

// setBit sets the bit at the given index in a packed bitmask. The index is an
// int64 because an n*n bitmask over the full peer set overflows a 32-bit int.
func setBit(mask []byte, index int64) {
	mask[index/8] |= 1 << (index % 8)
}

// getBit reports whether the bit at the given index in a packed bitmask is set.
// The index is an int64 because an n*n bitmask over the full peer set overflows
// a 32-bit int.
func getBit(mask []byte, index int64) bool {
	return mask[index/8]&(1<<(index%8)) != 0
}

// EncodeForwardingAbility serializes a nested map of peer forwarding abilities
// into a memory-efficient sparse gRPC response over [startTime, endTime]. To
// optimize payload size it tiers each pair: pairs that carried at least one
// forward keep a full entry, pairs that were up at least uptimeThreshold of the
// window but carried none collapse to a single bit in the up-but-idle bitmask,
// and pairs below the threshold that carried none are omitted entirely. Public
// keys are deduplicated and peer pairs packed into 32-bit indices.
func EncodeForwardingAbility(abilities map[string]map[string]ForwardingAbility,
	startTime, endTime int64,
	uptimeThreshold float64) (*ForwardingAbilityResponse, error) {

	minUptimeS := MinQualifyingUptime(uptimeThreshold, endTime-startTime)

	// First, find all unique peers involved in pairs that warrant either an
	// entry or a bit. Keys are normalized to lower-case hex so a peer that
	// appears in mixed case across entries collapses to a single index
	// rather than being silently dropped at lookup time.
	peerSet := make(map[string]struct{})
	for inPeer, outMap := range abilities {
		for outPeer, ability := range outMap {
			if ability.tier(minUptimeS) == tierAbsent {
				continue
			}

			peerSet[strings.ToLower(inPeer)] = struct{}{}
			peerSet[strings.ToLower(outPeer)] = struct{}{}
		}
	}

	// Decode to raw bytes and sort.
	var rawPeers [][]byte
	for peerHex := range peerSet {
		b, err := hex.DecodeString(peerHex)
		if err != nil {
			return nil, err
		}
		rawPeers = append(rawPeers, b)
	}

	sort.Slice(
		rawPeers,
		func(i, j int) bool {
			return bytes.Compare(rawPeers[i], rawPeers[j]) < 0
		},
	)

	// Peer indices occupy 16 bits each in packed_idx, so the set must stay
	// within maxPackedPeers. Beyond it, an index would overflow its field
	// and silently decode to the wrong peer pair, so fail loudly instead.
	if len(rawPeers) > maxPackedPeers {
		return nil, fmt.Errorf("peer set of %d exceeds the %d "+
			"addressable by packed_idx", len(rawPeers),
			maxPackedPeers)
	}

	// Create map for index lookup using normalized lowercase hex strings.
	peerIndex := make(map[string]uint32)
	for idx, b := range rawPeers {
		peerIndex[hex.EncodeToString(b)] = uint32(idx)
	}

	// The bitmask addresses every ordered pair over the peer set, so it
	// needs n*n bits. Allocation is deferred until a bit is actually set so
	// a response with no up-but-idle pairs carries no bitmask at all.
	n := int64(len(rawPeers))
	var bitmask []byte

	// Build the entries and bitmask. seen guards against two input keys
	// that differ only by hex case collapsing onto the same packed pair.
	var entries []*ForwardingAbilityEntry
	seen := make(map[uint32]struct{})

	// addEntry appends a full entry for a forwarded pair.
	addEntry := func(packed uint32, a ForwardingAbility) {
		entries = append(entries, &ForwardingAbilityEntry{
			PackedIdx:        packed,
			EffectiveUptimeS: a.EffectiveUptimeS,
			ForwardedMsat:    a.ForwardedMsat,
			FeeMsat:          a.FeeMsat,
			Forwards:         a.Forwards,
		})
	}

	for inPeer, outMap := range abilities {
		inIdx, okIn := peerIndex[strings.ToLower(inPeer)]
		if !okIn {
			continue
		}

		for outPeer, ability := range outMap {
			tier := ability.tier(minUptimeS)
			if tier == tierAbsent {
				continue
			}

			outIdx, okOut := peerIndex[strings.ToLower(outPeer)]
			if !okOut {
				continue
			}

			// Pack the in-peer index into the high 16 bits and the
			// out-peer index into the low 16.
			packed := (inIdx << 16) | outIdx

			// Reject a case-folded collision rather than silently
			// dropping one of the two entries' facts.
			if _, dup := seen[packed]; dup {
				return nil, fmt.Errorf("duplicate peer pair "+
					"after case normalization: "+
					"in=%s out=%s", inPeer, outPeer)
			}
			seen[packed] = struct{}{}

			switch tier {
			case tierEntry:
				addEntry(packed, ability)

			case tierBit:
				// The bitmask addresses n*n ordered pairs, one
				// bit each.
				if bitmask == nil {
					bitmask = make(
						[]byte, (n*n+7)/8,
					)
				}
				setBit(
					bitmask,
					int64(inIdx)*n+int64(outIdx),
				)
			}
		}
	}

	// Sort entries by packed_idx for deterministic output and testability.
	sort.Slice(
		entries,
		func(i, j int) bool {
			return entries[i].PackedIdx < entries[j].PackedIdx
		},
	)

	return &ForwardingAbilityResponse{
		Peers:            rawPeers,
		Entries:          entries,
		StartTime:        startTime,
		EndTime:          endTime,
		UpButIdleBitmask: bitmask,
		UptimeThreshold:  uptimeThreshold,
	}, nil
}

// DecodeForwardingAbility reconstructs the nested map of peer forwarding
// abilities from a sparse packed gRPC response. Forwarded pairs come back with
// their exact facts; up-but-idle pairs flagged in the bitmask come back at full
// window uptime with no forwards, and therefore no volume and no fees. It
// validates packed indices and the bitmask length against the decoded peer list
// to prevent out-of-bounds errors.
func DecodeForwardingAbility(resp *ForwardingAbilityResponse) (
	map[string]map[string]ForwardingAbility, error) {

	result := make(map[string]map[string]ForwardingAbility)
	if resp == nil {
		return result, nil
	}

	numPeers := len(resp.Peers)

	// packed_idx addresses peers with 16-bit indices, so a response with
	// more than maxPackedPeers peers is malformed. Rejecting it here also
	// keeps the n*n bitmask-length computation below from overflowing a
	// 32-bit int.
	if numPeers > maxPackedPeers {
		return nil, fmt.Errorf("peer set of %d exceeds the %d "+
			"addressable by packed_idx", numPeers, maxPackedPeers)
	}

	record := func(inIdx, outIdx int, ability ForwardingAbility) {
		inPeer := hex.EncodeToString(resp.Peers[inIdx])
		outPeer := hex.EncodeToString(resp.Peers[outIdx])

		if _, ok := result[inPeer]; !ok {
			result[inPeer] = make(map[string]ForwardingAbility)
		}
		result[inPeer][outPeer] = ability
	}

	// Decode the forwarded entries first so they take precedence over any
	// bit set for the same pair.
	for _, entry := range resp.Entries {
		// Unpack the pair: the in-peer index is the high 16 bits, the
		// out-peer index the low 16.
		inIdx := int(entry.PackedIdx >> 16)
		outIdx := int(entry.PackedIdx & 0xffff)

		if inIdx >= numPeers || outIdx >= numPeers {
			return nil, errors.New("decoded peer index out of " +
				"bounds")
		}

		record(
			inIdx, outIdx, ForwardingAbility{
				EffectiveUptimeS: entry.EffectiveUptimeS,
				ForwardedMsat:    entry.ForwardedMsat,
				FeeMsat:          entry.FeeMsat,
				Forwards:         entry.Forwards,
			},
		)
	}

	// Expand the up-but-idle bitmask. An absent bitmask simply means no
	// pair was flagged; a present one must address exactly the n*n pairs.
	bitmask := resp.UpButIdleBitmask
	if len(bitmask) == 0 {
		return result, nil
	}

	// Compute the expected length in int64 so the n*n multiplication does
	// not overflow a 32-bit int for a large peer set.
	totalPairs := int64(numPeers) * int64(numPeers)
	if want := int((totalPairs + 7) / 8); len(bitmask) != want {
		return nil, fmt.Errorf("bitmask length %d does not match the "+
			"%d expected for %d peers", len(bitmask), want,
			numPeers)
	}

	// Up-but-idle pairs were up the whole window by definition of the
	// threshold bucket, so reconstruct them at full window uptime with no
	// forwards, and therefore no volume and no fees. Iterate over the
	// bitmask bytes directly, skipping zero bytes, so cost scales with the
	// number of set bits rather than the O(n*n) pair space; padding bits
	// beyond n*n are ignored.
	// The window is a difference of signed unix timestamps, so an end
	// before the start yields zero uptime rather than wrapping into a
	// near-infinite one.
	var windowSeconds uint64
	if d := resp.EndTime - resp.StartTime; d > 0 {
		windowSeconds = uint64(d)
	}
	for i, b := range bitmask {
		if b == 0 {
			continue
		}

		for bit := range 8 {
			// If the bit is not set, skip the pair. This also
			// implicitly ignores any padding bits in the last byte
			// beyond the n*n pairs.
			if b&(1<<bit) == 0 {
				continue
			}

			// Compute the pair index from the byte index and bit
			// position.
			k := int64(i)*8 + int64(bit)
			if k >= totalPairs {
				break
			}

			// Unpack the pair: the in-peer index is the high 16
			// bits, the out-peer index the low 16. The bounds were
			// already checked against the bitmask length, so this
			// cannot overflow.
			inIdx := int(k / int64(numPeers))
			outIdx := int(k % int64(numPeers))

			// An entry for this pair takes precedence; never
			// overwrite it.
			inPeer := hex.EncodeToString(resp.Peers[inIdx])
			outPeer := hex.EncodeToString(resp.Peers[outIdx])
			if _, ok := result[inPeer][outPeer]; ok {
				continue
			}

			record(
				inIdx, outIdx, ForwardingAbility{
					EffectiveUptimeS: windowSeconds,
					ForwardedMsat:    0,
				},
			)
		}
	}

	return result, nil
}
