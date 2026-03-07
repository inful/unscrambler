# Game library opportunities: foundation for game variants

This doc outlines refactors that would make it easier to add new games similar to **dagame** (scramble/guess) and **explainer** (emoji canvas). Focus: shared behaviour in a small library so new games need mostly game-specific logic and views.

---

## What you already have

| Layer | Package | Purpose |
|-------|---------|---------|
| IDs | `pkg/id` | `NewID()` for game/player IDs |
| HTTP | `pkg/httputil` | ParseInt, WriteSSE, BuildInviteURL, cookie get/set, WithGame, SSEStream |
| Words | `pkg/words` | LoadWords(fs, dir, lang, minLen), SupportedLanguages |
| Status / base | `pkg/gamecommon` | StatusLobby, StatusInProgress, StatusFinished, BaseGame, BasePlayer, AddPlayer, IsOwner, NewGameID |
| Realtime | `pkg/realtime` | RoomStore[T], GameStore[G], Broadcaster, TimedRounds, RunLoop, Wake |

Each new game still has to: implement CreateGame (and optionally custom EnsureRoundLoop), and game-specific fields/snapshot. Handler lookup, SSE stream loop, and common game/player fields are handled by httputil and gamecommon.

---

## Refactor opportunities (by impact)

### 1. Round-loop interface (high impact, low effort)

**Problem:** Both games have the same EnsureRoundLoop pattern: getState, tick that calls `NextTimer` and `AdvanceIfNeeded`, returns (next, events, stop). Only the event list and optional extra tick logic (e.g. explain’s RevealLettersIfNeeded) differ.

**Proposal:** In `pkg/realtime` (or `pkg/gamecommon`), define a small interface and helper:

```go
// RoundLoopState is implemented by game state that uses TimedRounds and advances on timer.
type RoundLoopState interface {
    NextTimer(now time.Time) (time.Time, bool)
    AdvanceIfNeeded(now time.Time) bool
}
```

Then add a helper that takes a room ID, a way to get state (e.g. from RoomStore), and a function that maps “state just advanced” → events to publish. Each game implements the interface on its `*Game` and calls one shared “run round loop” helper with its event list (and explain can run an extra step before returning, e.g. reveal letters and optionally add `"wordhint"` to events).

**Benefit:** New games implement `NextTimer` and `AdvanceIfNeeded` (which they already do) and plug into one shared loop; no copy-paste of EnsureRoundLoop/tick.

---

### 2. Generic game store (medium impact, medium effort)

**Problem:** Both `internal/game.Store` and `internal/explain.Store` are the same shape: wrap `RoomStore[*Game]`, expose CreateGame (different args), GetGame, Broadcaster, Publish, EnsureRoundLoop, Wake.

**Proposal:** Add a generic in `pkg/realtime` or a new `pkg/game`:

- `GameRoomStore[G RoundLoopState]` (or similar) that holds `*realtime.RoomStore[G]` and provides GetGame, Broadcaster, Publish, Wake, and EnsureRoundLoop by calling the round-loop helper from (1). CreateGame stays game-specific (each game’s Store embeds or composes the generic store and adds its own CreateGame with its own options).

**Benefit:** New game = define your Game type (with TimedRounds, etc.), implement the round-loop interface, and a small Store struct that embeds the generic store and only adds CreateGame(rounds, duration, lang, …).

---

### 3. Handler helpers: “with game” and “with game + player” (high impact, medium effort)

**Problem:** Almost every handler repeats: get `id` from route, GetGame(id), 404 if !ok, get playerID from cookie (and sometimes player name). That’s 5–10 lines per handler.

**Proposal:** Add a small helper (e.g. in `pkg/httputil` or `pkg/game`) that:

- Takes a store with `GetGame(id) (G, bool)` and a cookie-name function.
- Returns a middleware or wrapper that: reads `id` from the route, loads the game, returns 404 if missing, reads playerID from cookie, and calls the next handler with (w, r, game G, playerID string).

Handler signatures become “given I have the game and playerID, what do I do?” instead of repeating lookup/404/cookie. You can do this with generics: e.g. `WithGame(store, cookieNameForGame)(func(w, r, g *MyGame, playerID string))`.

**Benefit:** New game handlers are shorter and uniform; less chance of forgetting 404 or cookie handling.

---

### 4. SSE stream helper (medium impact, low effort) — **done**

**Problem:** Both streams do the same thing: set SSE headers, subscribe to hub, send initial payload, then loop on context done / event / 25s keepalive. Only “how to build the payload for event X” is game-specific.

**Proposal:** In `pkg/httputil` add something like:

```go
// SSEStream runs an SSE loop: subscribe to hub, call onEvent for each event (and once for "initial" if desired), send keepalive every 25s.
func SSEStream(w http.ResponseWriter, r *http.Request, hub *realtime.Broadcaster, onEvent func(ctx context.Context, event string) (payload string, ok bool))
```

Each game passes a callback that, given an event name (and maybe a “snapshot” fetched inside the callback), returns the HTML or JSON to send. The helper handles flusher check, headers, subscribe/unsubscribe, loop, and keepalive.

**Benefit:** New game implements one “event → payload” function instead of reimplementing the stream loop.

---

### 5. Optional: shared "base game" struct (medium impact, higher effort) — **done**

**Problem:** Both games share a lot of fields: ID, CreatedAt, TimedRounds, Status, Lang, OwnerID, Players map, RoundWinnerID, RoundSolvedAt. Player has ID, Username, JoinedAt, Points (and game adds Progress). AddPlayer and IsOwner are almost identical.

**Proposal:** In `pkg/gamecommon`, add a `BaseGame` (or similar) struct with these fields and a `BasePlayer` (ID, Username, JoinedAt, Points), plus helpers like `AddPlayer(base *BaseGame, username string) *BasePlayer` and `IsOwner(base *BaseGame, playerID string) bool`. Each game embeds BaseGame and adds its own fields (RoundData, Word, Canvas, ExplainerID, etc.) and its own Snapshot/Start/AdvanceIfNeeded.

**Benefit:** New “round-based, timed, multi-player” game composes the base and only adds what’s different (round content, rules, snapshot shape). More refactor and some naming/embedding decisions.

---

### 6. Optional: fragment helper (lower impact, low effort)

**Problem:** Many fragment handlers: get game, 404, get player, get snapshot, render one fragment. Repetitive.

**Proposal:** A small helper: “with game and player, load snapshot and call render(snapshot)”. Games that use it pass a function (game, playerID) → snapshot and (snapshot) → templ component. Reduces boilerplate a bit; less critical if (3) is in place.

---

## Suggested order

1. **Round-loop interface + helper** so EnsureRoundLoop is shared and new games only supply the tick/events.
2. **“With game” handler helper** so all handlers stop repeating get-game/404/cookie.
3. **SSE stream helper** so the stream loop is shared and games only supply event → payload.
4. **Generic game store** so Store is “embed + CreateGame” for new games.
5. **Base game struct** only if you add several more games and see repeated field/layout; otherwise the interface + store + handler/SSE helpers may be enough.

---

## What a new game would look like after these refactors

1. **Game type:** struct that embeds or composes TimedRounds (and optionally BaseGame), implements `NextTimer` and `AdvanceIfNeeded` (and any extra tick behaviour).
2. **Store:** embed generic GameRoomStore[MyGame], add `CreateGame(...)` that builds your game and calls `roomStore.Create(id, g)`.
3. **Handlers:** use WithGame(store, cookieName) so handlers receive (w, r, game, playerID); implement gamePage (build view model from game.Snapshot), join, start, and any game-specific actions (guess, canvas, etc.).
4. **Stream:** use SSEStream(hub, func(event) { return renderFragment(event, game.Snapshot(...)) }).
5. **Views:** your own templ pages and fragments; view models stay game-specific.

That gives you a solid, reusable foundation without forcing every game into one rigid type: the library handles room store, round loop, “with game” context, and SSE loop; each game keeps its own state shape, snapshot, and UI.
