# screen-tiers

The live screen's quality tiers (#6396, `docs/extensions/computer-use.md` "Live screen video (#3954)"): what the
browser's port (`web/src/screen/tiers/`, #6558) must give. One scenario per file: its input and the expected output, never internal state.

## `ladder/`

`TierLadder.make`: `input` is `points` and `pixels` (`[width, height]`), `maxSize` (the long-edge cap; 0: none),
`bias` (`auto` / `sharper` / `smoother`), `capKbps` (0: no cap), `maxFps` and `codec` (`hevc` / `h264`). `expected` is
the whole ladder, best first: `scale` (what the helper is asked for), `fps`, `width`, `height`, `minKbps`, `maxKbps`,
`pixelsPerPoint` (the scale the picture really has: `width` over the display's points, #7842).

## `signals/`

`ReceiverSignals`: `steps` in order, each one of
- `frame`: `capture`, `arrival` and `firstByte` (µs; `firstByte` null when the frame came in one read), `bytes`,
  `refinement`, `keyframe` (#7842: a keyframe's wake is left out of `queueP95Ms`), `fps`;
- `decoded`: `ms`, `at` (µs); `shown`: `replaced`, `at` (µs); `pathChanged`: `{}`;
- `take`: `now` (µs), with `expected`, every `TierSignals` field (`frames`, `queueP50Ms`, `queueP95Ms`, `queueLastMs`,
  `queueRising`, `queueFalling`, `deliveredKbps`, `burstKbps`, `lateShare`, `decodeP95Ms`, `replacedShare`), null
  where it isn't measured.

## `policy/`

`TierPolicy`: `init` (`display` with `points`, `pixels` and `maxSize`; `bias`, `capKbps`, `maxFps`, `maxScale` (null: no
limit; Data Saver: 1), `link` (`kind`, `via`, `minRttMs`, `srttMs`; null before a route is measured), `remembered`
(null), `now`), then `start`, the Change `init` begins on, then `steps` in order, each one of
- `helper`: the helper's `timing` as the policy reads it (`targetKbps`, `kbps`, `skipped`), with no answer;
- `tick`: `signals` (every `TierSignals` field, as in `signals/`) and `now`;
- `path`: `to` (a link as above) and `now`;
- `setBias`: `bias` and `now`; `setBudget`: `capKbps`, `maxFps`, `maxScale` and `now`;

each with `expected`, the Change it returned (`index`, `scale`, `fps`, `width`, `height`, `minKbps`, `maxKbps`,
`pixelsPerPoint`, `targetKbps`, `reason`) or null. Times are seconds, as the API takes them.

## Numbers

Units are the API's: µs in `signals`, seconds in `policy`, kbit/s and pixels elsewhere. Sizes as `CapturePlan.size`
makes them (integer division, both sides even); bitrates rounded half up (the same as `Math.round`). Floats are compared within 1e-6.
