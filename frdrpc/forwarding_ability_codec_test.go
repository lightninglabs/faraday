package frdrpc

import (
	"encoding/hex"
	"fmt"
	"math"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// fwdKey returns a distinct 33-byte compressed-pubkey hex string for n. Keys
// sort ascending in n, matching the byte ordering the encoder applies.
func fwdKey(n int) string {
	return fmt.Sprintf("02%064x", n)
}

// pair is one expected decoded entry, flattened from the nested result map for
// easy comparison.
type pair struct {
	in      string
	out     string
	ability ForwardingAbility
}

// TestForwardingAbilityCodecRoundTrip verifies the three-tier encoding: pairs
// that forwarded keep exact facts as entries, pairs up at least the threshold
// but idle collapse to a bitmask bit (decoded at full window uptime), and
// sub-threshold idle pairs are dropped. The window is [0, 100) and the
// threshold 0.5, so the minimum qualifying uptime is 50 seconds.
func TestForwardingAbilityCodecRoundTrip(t *testing.T) {
	const (
		startTime, endTime int64   = 0, 100
		threshold          float64 = 0.5
	)

	tests := []struct {
		name        string
		abilities   map[string]map[string]ForwardingAbility
		wantPeers   []string
		wantPairs   []pair
		wantBitmask bool
	}{
		{
			// Forwarding wins regardless of uptime, so a
			// zero-uptime pair that moved volume survives with its
			// exact facts and never lands in the bitmask.
			name: "forwarded pairs keep exact facts",
			abilities: map[string]map[string]ForwardingAbility{
				fwdKey(1): {
					fwdKey(2): {
						EffectiveUptimeS: 80,
						ForwardedSat:     1500,
						FeeMsat:          1500,
						Forwards:         3,
					},
				},
				fwdKey(2): {
					fwdKey(1): {
						EffectiveUptimeS: 0,
						ForwardedSat:     2500,
						FeeMsat:          5000,
						Forwards:         1,
					},
				},
			},
			wantPeers: []string{
				fwdKey(1),
				fwdKey(2),
			},
			wantPairs: []pair{
				{
					fwdKey(1),
					fwdKey(2),
					ForwardingAbility{
						EffectiveUptimeS: 80,
						ForwardedSat:     1500,
						FeeMsat:          1500,
						Forwards:         3,
					},
				},
				{
					fwdKey(2),
					fwdKey(1),
					ForwardingAbility{
						EffectiveUptimeS: 0,
						ForwardedSat:     2500,
						FeeMsat:          5000,
						Forwards:         1,
					},
				},
			},
			wantBitmask: false,
		},
		{
			// Up at or above the threshold but no forwards: a bit,
			// decoded back at the full window's uptime.
			name: "up but idle becomes a bit",
			abilities: map[string]map[string]ForwardingAbility{
				fwdKey(1): {
					fwdKey(2): {
						EffectiveUptimeS: 80,
					},
				},
				fwdKey(3): {
					fwdKey(1): {
						EffectiveUptimeS: 50,
					},
				},
			},
			wantPeers: []string{
				fwdKey(1),
				fwdKey(2),
				fwdKey(3),
			},
			wantPairs: []pair{
				{
					fwdKey(1),
					fwdKey(2),
					ForwardingAbility{
						EffectiveUptimeS: 100,
					},
				},
				{
					fwdKey(3),
					fwdKey(1),
					ForwardingAbility{
						EffectiveUptimeS: 100,
					},
				},
			},
			wantBitmask: true,
		},
		{
			// Below the threshold with no forwards: dropped.
			name: "sub-threshold idle pairs dropped",
			abilities: map[string]map[string]ForwardingAbility{
				fwdKey(1): {
					fwdKey(2): {
						EffectiveUptimeS: 49,
					},
				},
			},
			wantPeers:   []string{},
			wantPairs:   []pair{},
			wantBitmask: false,
		},
		{
			// All three tiers at once, including a peer that only
			// appears via the bitmask.
			name: "mixed tiers",
			abilities: map[string]map[string]ForwardingAbility{
				fwdKey(1): {
					fwdKey(2): {
						EffectiveUptimeS: 80,
						ForwardedSat:     1500,
						FeeMsat:          750,
						Forwards:         2,
					},
					fwdKey(3): {
						EffectiveUptimeS: 60,
					},
				},
				fwdKey(2): {
					fwdKey(3): {
						EffectiveUptimeS: 10,
					},
				},
				fwdKey(3): {
					fwdKey(1): {
						ForwardedSat: 500,
						FeeMsat:      250,
						Forwards:     1,
					},
				},
			},
			wantPeers: []string{
				fwdKey(1),
				fwdKey(2),
				fwdKey(3),
			},
			wantPairs: []pair{
				{
					fwdKey(1),
					fwdKey(2),
					ForwardingAbility{
						EffectiveUptimeS: 80,
						ForwardedSat:     1500,
						FeeMsat:          750,
						Forwards:         2,
					},
				},
				{
					fwdKey(1),
					fwdKey(3),
					ForwardingAbility{
						EffectiveUptimeS: 100,
					},
				},
				{
					fwdKey(3),
					fwdKey(1),
					ForwardingAbility{
						ForwardedSat: 500,
						FeeMsat:      250,
						Forwards:     1,
					},
				},
			},
			wantBitmask: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			resp, err := EncodeForwardingAbility(
				tc.abilities, startTime, endTime, threshold,
			)
			require.NoError(t, err)
			require.Equal(t, startTime, resp.StartTime)
			require.Equal(t, endTime, resp.EndTime)
			require.Equal(t, threshold, resp.UptimeThreshold)

			require.Equal(
				t, tc.wantBitmask,
				len(resp.UpButIdleBitmask) > 0,
			)

			// A present bitmask must address exactly n*n bits.
			if tc.wantBitmask {
				n := len(resp.Peers)
				require.Len(t, resp.UpButIdleBitmask, (n*n+7)/8)
			}

			gotPeers := make([]string, len(resp.Peers))
			for i, p := range resp.Peers {
				gotPeers[i] = hex.EncodeToString(p)
			}
			require.Equal(t, tc.wantPeers, gotPeers)

			decoded, err := DecodeForwardingAbility(resp)
			require.NoError(t, err)

			got := make(map[string]ForwardingAbility)
			for in, outMap := range decoded {
				for out, ability := range outMap {
					got[in+"->"+out] = ability
				}
			}
			require.Len(t, got, len(tc.wantPairs))
			for _, wp := range tc.wantPairs {
				require.Equal(
					t, wp.ability, got[wp.in+"->"+wp.out],
				)
			}
		})
	}
}

// TestMinQualifyingUptime verifies the threshold-to-seconds conversion shared
// by the encoder and the server guard, including its boundary behavior.
func TestMinQualifyingUptime(t *testing.T) {
	tests := []struct {
		name      string
		threshold float64
		window    int64
		want      int64
	}{
		{
			"half of clean window",
			0.5,
			100,
			50,
		},
		{
			"rounds up a fraction",
			0.333,
			100,
			34,
		},
		{
			"integer boundary",
			0.9,
			2_592_000,
			2_332_800,
		},
		{
			"floored at one second",
			0.0,
			100,
			1,
		},
		{
			"non-positive window admits nothing",
			0.5,
			0,
			math.MaxInt64,
		},
	}

	for _, tc := range tests {
		t.Run(
			tc.name,
			func(t *testing.T) {
				require.Equal(
					t, tc.want, MinQualifyingUptime(
						tc.threshold, tc.window,
					),
				)
			},
		)
	}
}

// TestBitmaskHelpers verifies that setBit and getBit address the same bit.
func TestBitmaskHelpers(t *testing.T) {
	mask := make([]byte, 2)
	require.False(t, getBit(mask, 9))

	setBit(mask, 9)
	require.True(t, getBit(mask, 9))
	require.False(t, getBit(mask, 8))
	require.False(t, getBit(mask, 10))
}

// TestForwardingAbilityDecodeEntryPrecedence verifies that when a pair is both
// listed as an entry and flagged in the bitmask, the entry's exact facts win.
func TestForwardingAbilityDecodeEntryPrecedence(t *testing.T) {
	// Two peers => a 2*2 bitmask needs (4+7)/8 = 1 byte. Set the bit for
	// pair (0, 1) at index 0*2+1 = 1, and also list it as an entry.
	mask := make([]byte, 1)
	setBit(mask, 1)

	resp := &ForwardingAbilityResponse{
		Peers: [][]byte{
			{
				1,
			},
			{
				2,
			},
		},
		StartTime: 0,
		EndTime:   100,
		Entries: []*ForwardingAbilityEntry{
			{
				PackedIdx:        (0 << 16) | 1,
				EffectiveUptimeS: 42,
				ForwardedSat:     7,
				FeeMsat:          21,
				Forwards:         2,
			},
		},
		UpButIdleBitmask: mask,
	}

	decoded, err := DecodeForwardingAbility(resp)
	require.NoError(t, err)
	require.Equal(
		t, ForwardingAbility{
			EffectiveUptimeS: 42,
			ForwardedSat:     7,
			FeeMsat:          21,
			Forwards:         2,
		},
		decoded[hex.EncodeToString([]byte{1})][hex.EncodeToString(
			[]byte{2},
		)],
	)
}

// TestForwardingAbilityDecodeBadIndex verifies that a packed index referencing
// a peer beyond the decoded peer list is rejected rather than silently mapped.
func TestForwardingAbilityDecodeBadIndex(t *testing.T) {
	resp := &ForwardingAbilityResponse{
		Peers: [][]byte{
			{
				1,
				2,
				3,
			},
		},
		Entries: []*ForwardingAbilityEntry{
			{
				// Out index 1 is out of bounds for a single
				// peer.
				PackedIdx:        (0 << 16) | 1,
				EffectiveUptimeS: 3600,
				ForwardedSat:     1000,
			},
		},
	}

	_, err := DecodeForwardingAbility(resp)
	require.ErrorContains(t, err, "peer index out of bounds")
}

// TestForwardingAbilityDecodeBadBitmaskLen verifies that a bitmask whose length
// does not match the n*n pairs of the peer set is rejected.
func TestForwardingAbilityDecodeBadBitmaskLen(t *testing.T) {
	resp := &ForwardingAbilityResponse{
		// Two peers expect a 1-byte bitmask; supply two bytes.
		Peers: [][]byte{
			{
				1,
			},
			{
				2,
			},
		},
		UpButIdleBitmask: []byte{
			0x00,
			0x00,
		},
	}

	_, err := DecodeForwardingAbility(resp)
	require.ErrorContains(t, err, "bitmask length")
}

// TestForwardingAbilityEncodePeerCap verifies that a peer set too large to
// address with packed_idx is rejected loudly instead of overflowing an index
// into the wrong peer pair.
func TestForwardingAbilityEncodePeerCap(t *testing.T) {
	outMap := make(map[string]ForwardingAbility)
	for i := 1; i <= maxPackedPeers+1; i++ {
		// Use a carried forward so inclusion is threshold-independent.
		outMap[fwdKey(i)] = ForwardingAbility{
			ForwardedSat: 1,
			Forwards:     1,
		}
	}
	abilities := map[string]map[string]ForwardingAbility{
		fwdKey(0): outMap,
	}

	_, err := EncodeForwardingAbility(abilities, 0, 1, 0.5)
	require.ErrorContains(t, err, "exceeds")
}

// TestForwardingAbilityEncodeNormalizesCase verifies that a peer appearing in
// mixed hex case collapses to a single index rather than producing a duplicate
// peer entry.
func TestForwardingAbilityEncodeNormalizesCase(t *testing.T) {
	// Use a key with hex letters so its upper- and lower-case forms are
	// genuinely distinct map keys.
	peer := fwdKey(0xabcdef)

	abilities := map[string]map[string]ForwardingAbility{
		strings.ToUpper(peer): {
			fwdKey(2): {
				ForwardedSat: 20,
				Forwards:     1,
			},
		},
		peer: {
			fwdKey(3): {
				ForwardedSat: 40,
				Forwards:     1,
			},
		},
	}

	resp, err := EncodeForwardingAbility(abilities, 0, 1, 0.5)
	require.NoError(t, err)

	// The upper- and lower-case forms of the shared peer must dedup to one
	// index, leaving exactly three distinct peers.
	require.Len(t, resp.Peers, 3)

	decoded, err := DecodeForwardingAbility(resp)
	require.NoError(t, err)
	require.Equal(
		t, ForwardingAbility{
			ForwardedSat: 20,
			Forwards:     1,
		},
		decoded[peer][fwdKey(2)],
	)
	require.Equal(
		t, ForwardingAbility{
			ForwardedSat: 40,
			Forwards:     1,
		},
		decoded[peer][fwdKey(3)],
	)
}

// TestForwardingAbilityEncodeRejectsCaseCollision verifies that two input keys
// that differ only by hex case but address the same peer pair are rejected
// rather than silently collapsing onto one packed index and dropping a fact.
func TestForwardingAbilityEncodeRejectsCaseCollision(t *testing.T) {
	inPeer := fwdKey(0xabcdef)
	outPeer := fwdKey(2)

	// Both in-peer spellings normalize to the same index and share the same
	// out-peer, so they collide on packed_idx.
	abilities := map[string]map[string]ForwardingAbility{
		strings.ToUpper(inPeer): {
			outPeer: {
				ForwardedSat: 10,
				Forwards:     1,
			},
		},
		inPeer: {
			outPeer: {
				ForwardedSat: 20,
				Forwards:     1,
			},
		},
	}

	_, err := EncodeForwardingAbility(abilities, 0, 1, 0.5)
	require.Error(t, err)
}

// TestForwardingAbilityCodecRoundTripHighIndices round-trips a large peer set
// so that packed indices exceed a single byte and exercise the high bits of
// each 16-bit direction field, and the up-but-idle bitmask spans many bytes. It
// guards index packing and bitmask addressing against regressions that only
// surface beyond the small indices the other round-trip cases use.
func TestForwardingAbilityCodecRoundTripHighIndices(t *testing.T) {
	const (
		numPeers           = 300
		startTime, endTime = int64(0), int64(100)
		threshold          = 0.5
	)

	// Build a cycle so every peer appears and takes a stable index equal to
	// its fwdKey ordinal. Even edges forward (kept as exact entries); odd
	// edges are up but idle at >= threshold (collapsed to a bitmask bit,
	// decoded back at the full window uptime).
	abilities := make(map[string]map[string]ForwardingAbility, numPeers)
	want := make(map[string]ForwardingAbility, numPeers)
	for i := range numPeers {
		in, out := fwdKey(i), fwdKey((i+1)%numPeers)

		var enc, dec ForwardingAbility
		if i%2 == 0 {
			// Add pair that forwarded.
			enc = ForwardingAbility{
				EffectiveUptimeS: 70,
				ForwardedSat:     int64(i + 1),
				FeeMsat:          int64(i + 1),
				Forwards:         1,
			}
			dec = enc
		} else {
			// Add up, but idle pair.
			enc = ForwardingAbility{EffectiveUptimeS: 60}
			dec = ForwardingAbility{
				EffectiveUptimeS: endTime - startTime,
			}
		}

		abilities[in] = map[string]ForwardingAbility{out: enc}
		want[in+"->"+out] = dec
	}

	resp, err := EncodeForwardingAbility(
		abilities, startTime, endTime, threshold,
	)
	require.NoError(t, err)
	require.Len(t, resp.Peers, numPeers)

	// With 300 peers the indices exceed one byte, so at least one packed
	// index must use the high bits of its 16-bit field.
	var sawHighIdx bool
	for _, e := range resp.Entries {
		if e.PackedIdx>>16 > 0xff || e.PackedIdx&0xffff > 0xff {
			sawHighIdx = true
			break
		}
	}
	require.True(t, sawHighIdx, "expected an index beyond one byte")

	decoded, err := DecodeForwardingAbility(resp)
	require.NoError(t, err)

	got := make(map[string]ForwardingAbility)
	for in, outMap := range decoded {
		for out, ability := range outMap {
			got[in+"->"+out] = ability
		}
	}
	require.Equal(t, want, got)
}

// TestForwardingAbilityDecodeNil verifies that decoding a nil response yields
// an empty map rather than panicking.
func TestForwardingAbilityDecodeNil(t *testing.T) {
	decoded, err := DecodeForwardingAbility(nil)
	require.NoError(t, err)
	require.Empty(t, decoded)
}

// TestForwardingAbilityDecodeIgnoresPaddingBit verifies that a bit set in the
// padding region beyond the n*n pairs of the final byte is ignored rather than
// decoded into a bogus pair.
func TestForwardingAbilityDecodeIgnoresPaddingBit(t *testing.T) {
	// Two peers => 2*2 = 4 valid bits in a 1-byte mask; bits 4..7 are
	// padding. Set padding bit 5 and assert nothing decodes from it.
	mask := make([]byte, 1)
	setBit(mask, 5)

	resp := &ForwardingAbilityResponse{
		Peers:            [][]byte{{1}, {2}},
		StartTime:        0,
		EndTime:          100,
		UpButIdleBitmask: mask,
	}

	decoded, err := DecodeForwardingAbility(resp)
	require.NoError(t, err)
	require.Empty(t, decoded)
}
