# Next release

## Breaking Changes

* Migrate exported Bitcoin types and dependencies to the btcd v2 modules.
  Source builds require Go 1.26.8.

* `frcli forwardingability` prints a JSON object with the fields
  `unobserved_s` and `pairs`. The `pairs` field holds the array the command
  printed before. The `uptime_fraction` of a pair uses the observed window.

## RPC Changes

* `ForwardingAbility` reports `unobserved_s`, the seconds of the window in
  which faraday did not observe lnd, for example while faraday or lnd was
  down. Effective uptime excludes this time, and the uptime threshold applies
  to the observed window. Divide effective uptime by
  `end_time - start_time - unobserved_s`. An outage that still goes on counts
  as observed time until the next initial sync marks it as unobserved.

## Improvements

* The channel events monitor runs the initial sync again each time the
  subscription to lnd reconnects. This re-baselines the channel balances and
  marks the break as unobserved time.

## Maintenance

* Retain the lnd v0.21.4 recovery fixes while migrating to btcd v2.
