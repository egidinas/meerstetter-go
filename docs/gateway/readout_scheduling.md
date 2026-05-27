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
- Raw ring samples should enter the graph history through the normal tile
  pyramid so long ranges use decimated levels instead of re-reading the raw
  stream.

## 3. Controller ring operating envelope

CRTVStream is a bounded controller resource, not an unlimited high-rate bus.

- The device ring is a 4096-byte circular buffer.
- `?RS0001` reads are capped at 279 bytes per request in the gateway client.
- The protocol can configure 16 capture slots, but the gateway default is four
  slots per controller so shared serial or TCP routes do not fall behind the
  device ring.
- Ring capture uses the same lazy `?VX` queue as fallback. Values that do not
  fit into the bounded ring set, and values on transports without ring support,
  stay real controller reads via the round-robin queue.
- A route that reports no ring capability must move ring candidates into the
  background queue rather than reporting a priority lane that cannot actually
  deliver samples.

## 4. Lazy catalogue queue is lower priority

Catalogue and dictionary expansion should stay lower priority than hot readout.

- Lazy catalogue fetches are background work.
- When a tree or signal is opened, that item may receive a local boost for the
  next read cycle.
- Opened-tree boost should improve freshness for visible items without changing
  the fundamental priority model.
- Age indicators should be visible so the operator can tell whether a lazy item
  is still warming up or already current.

## 5. Optional boost mode is negotiated, not promised

Optional boost mode is a capability-negotiated dual-route scheduling policy.
It is not a blanket UI guarantee.

- If both routes support the needed capabilities, the gateway may schedule a
  boosted route and a normal route together.
- The UI may expose that a boost is available, but it must not promise boost
  behavior unless the backend has negotiated it.
- This is a runtime capability decision, not a static design assumption.

## 6. Route policy summary

| Route / path | Priority lane | Hot buffer | 1 Hz tiles | Notes |
|--------------|---------------|------------|------------|-------|
| Serial MeCom | Yes, when ring is supported | Yes | Yes | Bounded ring capture plus lazy `?VX` fallback. |
| CANopen direct | No, unless implemented | Yes | Yes | No ring claim without proof. |
| Kvaser direct CAN | No, unless implemented | Yes | Yes | No ring claim without proof. |

## 7. Non-goals

- Do not describe ring access as a universal transport feature.
- Do not configure all 16 CRTVStream slots by default on shared production
  routes.
- Do not present boost mode as a UI promise independent of capability
  negotiation.
- Do not mix lazy catalogue fetches with the hot full-resolution read path.
- Do not conflate long-term tiles with the short hot investigation buffer.
