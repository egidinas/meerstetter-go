# Gateway readout scheduling contract

This note corrects the readout contract for Meerstetter-Go gateway and UI
surfaces. It is intentionally conservative: do not infer support for a route
unless the route has been implemented and proven.

## 1. Priority lane means an actual supported ring route

The "priority lane" is not a generic UI fast path. It means controller-internal
ring or CRTVStream readout, and only for routes that actually support that
mechanism.

- If a route can expose internal ring / CRTVStream data, the gateway may use it
  as a priority lane.
- If a route does not support ring readout, the gateway must not pretend that a
  priority lane exists.
- Capability claims must stay route-specific and evidence-based.

### Current support snapshot

- Serial MeCom: ring readout is currently supported.
- CANopen direct path: ring readout is not currently supported unless it has
  been explicitly implemented and proven.
- Kvaser direct CAN path: ring readout is not currently supported unless it has
  been explicitly implemented and proven.

## 2. Buffer tiers serve different operational goals

The gateway should keep the distinction between short hot data and long-term
tile data explicit.

### Hot buffer

- Short-lived.
- Full-resolution.
- Used for live operator inspection, noise improvement, and fault
  investigation.
- May retain more recent samples than the long-term tile path, but it is not the
  historical record.

### Tile / long-term buffer

- Smoothed and decimated to 1 Hz.
- Intended for the long-view chart and wall tiles.
- Should remain bounded and stable for rendering, not become a raw sample dump.

## 3. Lazy catalogue queue is lower priority

Catalogue and dictionary expansion should stay lower priority than hot readout.

- Lazy catalogue fetches are background work.
- When a tree or signal is opened, that item may receive a local boost for the
  next read cycle.
- Opened-tree boost should improve freshness for visible items without changing
  the fundamental priority model.
- Age indicators should be visible so the operator can tell whether a lazy item
  is still warming up or already current.

## 4. Optional boost mode is negotiated, not promised

Optional boost mode is a capability-negotiated dual-route scheduling policy.
It is not a blanket UI guarantee.

- If both routes support the needed capabilities, the gateway may schedule a
  boosted route and a normal route together.
- The UI may expose that a boost is available, but it must not promise boost
  behavior unless the backend has negotiated it.
- This is a runtime capability decision, not a static design assumption.

## 5. Route policy summary

| Route / path | Priority lane | Hot buffer | 1 Hz tiles | Notes |
|--------------|---------------|------------|------------|-------|
| Serial MeCom | Yes, when ring is supported | Yes | Yes | Current ring-readout support exists. |
| CANopen direct | No, unless implemented | Yes | Yes | No ring claim without proof. |
| Kvaser direct CAN | No, unless implemented | Yes | Yes | No ring claim without proof. |

## 6. Non-goals

- Do not describe ring access as a universal transport feature.
- Do not present boost mode as a UI promise independent of capability
  negotiation.
- Do not mix lazy catalogue fetches with the hot full-resolution read path.
- Do not conflate long-term tiles with the short hot investigation buffer.

