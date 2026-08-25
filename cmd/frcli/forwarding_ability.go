package main

import (
	"context"
	"sort"

	"github.com/lightninglabs/faraday/frdrpc"
	"github.com/urfave/cli"
)

var forwardingAbilityCommand = cli.Command{
	Name:     "forwardingability",
	Category: "insights",
	Usage:    "Get forwarding ability analysis of peer pairs.",
	Flags: []cli.Flag{
		cli.Uint64Flag{
			Name: "start_time",
			Usage: "start time of the query range as a unix " +
				"timestamp",
		},
		cli.Uint64Flag{
			Name: "end_time",
			Usage: "end time of the query range as a unix " +
				"timestamp; zero defaults to the server's " +
				"current time",
		},
		cli.Uint64Flag{
			Name: "liquidity_floor_sat",
			Usage: "the minimum directional liquidity in " +
				"satoshis for a pair to count as " +
				"economically forwardable; zero uses the " +
				"server default",
		},
		cli.Float64Flag{
			Name: "uptime_threshold",
			Usage: "the uptime fraction in [0,1] at or above " +
				"which a non-forwarding pair is reported as " +
				"up but idle; zero uses the server default",
		},
	},
	Action: queryForwardingAbility,
}

type pairView struct {
	PeerIn           string  `json:"peer_in"`
	PeerOut          string  `json:"peer_out"`
	EffectiveUptimeS int64   `json:"effective_uptime_s"`
	ForwardedSat     int64   `json:"forwarded_sat"`
	FeeMsat          int64   `json:"fee_msat"`
	Forwards         int64   `json:"forwards"`
	UptimeFraction   float64 `json:"uptime_fraction"`
	Velocity         float64 `json:"velocity"`
	FeeRate          float64 `json:"fee_rate"`
}

func queryForwardingAbility(ctx *cli.Context) error {
	client, cleanup := getClient(ctx)
	defer cleanup()

	req := &frdrpc.ForwardingAbilityRequest{
		StartTime:         ctx.Uint64("start_time"),
		EndTime:           ctx.Uint64("end_time"),
		LiquidityFloorSat: ctx.Uint64("liquidity_floor_sat"),
		UptimeThreshold:   ctx.Float64("uptime_threshold"),
	}

	rpcCtx := context.Background()
	resp, err := client.ForwardingAbility(rpcCtx, req)
	if err != nil {
		return err
	}

	abilities, err := frdrpc.DecodeForwardingAbility(resp)
	if err != nil {
		return err
	}

	// The metrics are raw, so derive uptime fraction, velocity and fee rate
	// here from the window the server reported.
	windowSeconds := resp.EndTime - resp.StartTime

	var views []pairView
	for inPeer, outMap := range abilities {
		for outPeer, ability := range outMap {
			var uptimeFraction, velocity float64
			if windowSeconds > 0 {
				uptimeFraction = float64(
					ability.EffectiveUptimeS,
				) / float64(windowSeconds)
			}

			if ability.EffectiveUptimeS > 0 {
				velocity = float64(ability.ForwardedSat) /
					float64(ability.EffectiveUptimeS)
			}

			// Fees over volume. A pair with only sub-satoshi
			// forwards has no volume to divide by, so it keeps a
			// zero rate rather than an infinite one.
			var feeRate float64
			if ability.ForwardedSat > 0 {
				feeRate = float64(ability.FeeMsat) /
					(float64(ability.ForwardedSat) * 1000)
			}

			views = append(
				views, pairView{
					PeerIn:           inPeer,
					PeerOut:          outPeer,
					EffectiveUptimeS: ability.EffectiveUptimeS,
					ForwardedSat:     ability.ForwardedSat,
					FeeMsat:          ability.FeeMsat,
					Forwards:         ability.Forwards,
					UptimeFraction:   uptimeFraction,
					Velocity:         velocity,
					FeeRate:          feeRate,
				},
			)
		}
	}

	// Stable sort by PeerIn, then PeerOut.
	sort.SliceStable(
		views,
		func(i, j int) bool {
			if views[i].PeerIn != views[j].PeerIn {
				return views[i].PeerIn < views[j].PeerIn
			}

			return views[i].PeerOut < views[j].PeerOut
		},
	)

	printJSON(views)

	return nil
}
