package explain

import (
	cryptoRand "crypto/rand"
	"encoding/base32"
	"errors"
	"math"
	"math/rand"
	"sort"
	"strings"
	"sync"
	"time"

	"dagame/pkg/realtime"
)

const (
	StatusLobby      = "lobby"
	StatusInProgress = "in_progress"
	StatusFinished   = "finished"

	DefaultEmojisPerRound = 8
	MinPlayers           = 2
)

// Emoji set for the explainer's canvas.
var DefaultEmojiPool = []string{
	// Faces & people
	"😀", "😂", "😍", "🥰", "😎", "🤔", "😱", "🥳", "😴", "🤯", "🥺", "😭", "🤩", "😏", "🤫",
	"👶", "👧", "👦", "👩", "👨", "👴", "👵", "🧙", "👸", "🤴", "🦸", "🧛", "🧜", "🧝",
	// Body
	"👀", "👂", "🦶", "🦷", "🧠", "👍", "👎", "✌️", "🤞", "👊", "🙌", "🤝", "🫶",
	// Animals
	"🐶", "🐱", "🐻", "🐼", "🐨", "🦁", "🐯", "🦊", "🐸", "🐔", "🐧", "🦆", "🦅", "🦋", "🐝",
	"🐙", "🦈", "🐳", "🦓", "🦒", "🦘", "🐘", "🦏", "🐪", "🦙", "🐑", "🐄", "🐎", "🐖", "🐓",
	"🐍", "🦎", "🐢", "🦜", "🦩", "🦚", "🦫", "🦦", "🦥", "🐿️",
	// Nature & weather
	"🌸", "🌺", "🌻", "🌲", "🌴", "🌵", "🍄", "🌍", "🌊", "🏔️", "🌋", "🏜️", "🌈", "☀️", "🌙",
	"⭐", "❄️", "🌪️", "⚡", "🔥", "💧", "🌱",
	// Food & drink
	"🍎", "🍕", "🍔", "🌮", "🍣", "🍩", "🎂", "🍦", "🍇", "🍓", "🍌", "🥑", "🥕", "🌽", "🍞",
	"☕", "🍺", "🍷", "🧃", "🍵",
	// Activities & sports
	"⚽", "🏀", "🎾", "🏊", "🚴", "🏋️", "🎯", "🎮", "🎸", "🎵", "🎭", "🎨", "🎬", "🎤", "💃",
	"🏄", "🧗", "🤸", "🥊", "⛷️", "🎿",
	// Objects & places
	"🏠", "🏰", "🗼", "🗽", "⛩️", "🚗", "✈️", "🚀", "🚢", "🚂", "🚁", "🛸", "📷", "💡", "🔑",
	"📚", "🎁", "🎈", "🏆", "💰", "⏰", "📱", "💻", "🔭", "🧪", "🔬", "⚙️", "🧲", "🪄", "🗺️",
	"🛒", "🛏️", "🪞", "🪑", "🚪", "🪟", "🏺", "🖼️", "🧸", "🎀",
	// Symbols & misc
	"❤️", "💔", "💯", "❓", "❗", "✏️", "✂️", "🔍", "🔒", "💬",
}

type Store struct {
	r *realtime.RoomStore[*Game]
}

func NewStore() *Store {
	return &Store{r: realtime.NewRoomStore[*Game]()}
}

func (s *Store) CreateGame(rounds int, duration time.Duration, lang string, emojisPerRound int) *Game {
	g := NewGame(rounds, duration, lang, emojisPerRound)
	s.r.Create(g.ID, g)
	return g
}

func (s *Store) GetGame(id string) (*Game, bool) {
	room, ok := s.r.Get(id)
	if !ok {
		return nil, false
	}
	return room.State, ok
}

func (s *Store) Broadcaster(id string) *realtime.Broadcaster {
	return s.r.Broadcaster(id)
}

func (s *Store) Publish(id string, event string) {
	s.r.Publish(id, event)
}

func (s *Store) EnsureRoundLoop(id string, _ *Game) {
	getState := func() *Game {
		room, ok := s.r.Get(id)
		if !ok {
			return nil
		}
		return room.State
	}
	tick := func(state *Game, now time.Time) (time.Time, []string, bool) {
		if state == nil {
			return time.Time{}, nil, true
		}
		next, ok := state.NextTimer(now)
		if !ok {
			return time.Time{}, nil, true
		}
		advanced := state.AdvanceIfNeeded(now)
		if advanced {
			next2, ok2 := state.NextTimer(now)
			if !ok2 {
				return time.Time{}, nil, true
			}
			return next2, []string{"round", "scores", "players", "wordhint", "canvas"}, false
		}
		// Publish wordhint when letters are revealed (50%, 75%) even if round didn't advance
		if state.RevealLettersIfNeeded(now) {
			return next, []string{"wordhint"}, false
		}
		return next, nil, false
	}
	s.r.RunLoop(id, getState, tick)
}

func (s *Store) Wake(id string) {
	s.r.Wake(id)
}

// Game holds state for one explain game session.
type Game struct {
	mu          sync.Mutex
	ID          string
	CreatedAt   time.Time
	TimedRounds realtime.TimedRounds
	RoundData   []RoundData // pre-picked words per round (so explainer sees same word)
	Status      string
	Lang        string
	OwnerID     string
	Players     map[string]*Player

	// Current round: word, explainer, canvas, revealed indices, emojis for this round
	Word              string   // current round word (secret from guessers)
	ExplainerID       string   // player ID of explainer this round
	Canvas            []CanvasItem
	RevealedIndices   []int    // indices into Word that have been revealed to guessers
	RoundEmojis       []string // n random emojis explainer can use this round
	EmojisPerRound    int
	RoundWinnerID     string   // guesser who got it this round (if any)
	RoundSolvedAt     time.Time
}

type RoundData struct {
	Word   string
	Emojis []string
}

type CanvasItem struct {
	ID    string
	Emoji string
	X     float64
	Y     float64
}

type Player struct {
	ID       string
	Username string
	JoinedAt time.Time
	Points   int
}

func NewGame(rounds int, duration time.Duration, lang string, emojisPerRound int) *Game {
	if lang == "" {
		lang = "en"
	}
	if emojisPerRound <= 0 {
		emojisPerRound = DefaultEmojisPerRound
	}
	rng := rand.New(rand.NewSource(time.Now().UnixNano()))
	roundData := make([]RoundData, rounds)
	for i := 0; i < rounds; i++ {
		word := PickRandomWord(lang, rng)
		emojis := pickRandomEmojis(emojisPerRound, rng)
		roundData[i] = RoundData{Word: word, Emojis: emojis}
	}
	return &Game{
		ID:             newID(),
		CreatedAt:      time.Now().UTC(),
		TimedRounds: realtime.TimedRounds{
			Rounds:   rounds,
			Duration: duration,
			Cooldown: realtime.DefaultCooldown,
		},
		RoundData:        roundData,
		Status:           StatusLobby,
		Lang:             lang,
		Players:          make(map[string]*Player),
		EmojisPerRound:   emojisPerRound,
		Canvas:           nil,
		RevealedIndices:  nil,
	}
}

func pickRandomEmojis(n int, rng *rand.Rand) []string {
	pool := make([]string, len(DefaultEmojiPool))
	copy(pool, DefaultEmojiPool)
	rng.Shuffle(len(pool), func(i, j int) { pool[i], pool[j] = pool[j], pool[i] })
	if n > len(pool) {
		n = len(pool)
	}
	return pool[:n]
}

func newID() string {
	buf := make([]byte, 10)
	_, _ = cryptoRand.Read(buf)
	encoder := base32.StdEncoding.WithPadding(base32.NoPadding)
	return strings.ToLower(encoder.EncodeToString(buf))
}

func (g *Game) AddPlayer(username string) *Player {
	g.mu.Lock()
	defer g.mu.Unlock()
	p := &Player{
		ID:       newID(),
		Username: username,
		JoinedAt: time.Now().UTC(),
	}
	g.Players[p.ID] = p
	if g.OwnerID == "" {
		g.OwnerID = p.ID
	}
	return p
}

func (g *Game) Start(now time.Time) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.Status != StatusLobby {
		return errors.New("game already started")
	}
	if len(g.Players) < MinPlayers {
		return errors.New("need at least 2 players")
	}
	g.Status = StatusInProgress
	g.TimedRounds.Start(now)
	g.startRoundLocked(now)
	return nil
}

func (g *Game) startRoundLocked(now time.Time) {
	// Explainer for this round: rotate by round index
	playerIDs := make([]string, 0, len(g.Players))
	for id := range g.Players {
		playerIDs = append(playerIDs, id)
	}
	sort.Strings(playerIDs)
	idx := (g.TimedRounds.CurrentRound - 1) % len(playerIDs)
	if idx < 0 || idx >= len(playerIDs) {
		idx = 0
	}
	g.ExplainerID = playerIDs[idx]
	rd := g.currentRoundDataLocked()
	g.Word = rd.Word
	g.RoundEmojis = rd.Emojis
	g.Canvas = nil
	g.RevealedIndices = nil
	g.RoundWinnerID = ""
	g.RoundSolvedAt = time.Time{}
}

func (g *Game) currentRoundDataLocked() RoundData {
	if g.TimedRounds.CurrentRound <= 0 || g.TimedRounds.CurrentRound > len(g.RoundData) {
		return RoundData{}
	}
	return g.RoundData[g.TimedRounds.CurrentRound-1]
}

// NextTimer returns next wake time for the round loop (including 50%/75% letter-reveal times).
func (g *Game) NextTimer(now time.Time) (time.Time, bool) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.Status != StatusInProgress {
		return time.Time{}, false
	}
	next, ok := g.TimedRounds.NextWake(now)
	if !ok {
		return time.Time{}, false
	}
	// Also wake at 50% and 75% of round for letter reveals
	if g.TimedRounds.RoundEndedAt.IsZero() && !g.TimedRounds.RoundStarted.IsZero() {
		start := g.TimedRounds.RoundStarted
		dur := g.TimedRounds.Duration
		half := start.Add(dur / 2)
		threeq := start.Add((dur * 3) / 4)
		if now.Before(half) && (next.IsZero() || half.Before(next)) {
			next = half
		}
		if now.Before(threeq) && (next.IsZero() || threeq.Before(next)) {
			next = threeq
		}
	}
	return next, true
}

// advanceIfNeededLocked advances the game state. Must be called with g.mu already held.
func (g *Game) advanceIfNeededLocked(now time.Time) bool {
	if g.Status != StatusInProgress || g.TimedRounds.RoundStarted.IsZero() {
		return false
	}
	advanced, finished := g.TimedRounds.Advance(now)
	if finished {
		g.Status = StatusFinished
		return true
	}
	if advanced {
		g.RoundWinnerID = ""
		g.RoundSolvedAt = time.Time{}
		g.startRoundLocked(now)
		return true
	}
	return false
}

// AdvanceIfNeeded advances to next round or finishes game; updates TimedRounds and game state.
func (g *Game) AdvanceIfNeeded(now time.Time) bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.advanceIfNeededLocked(now)
}

// RevealLettersIfNeeded reveals one letter at 50% and one at 75% of round time. Returns true if state changed.
func (g *Game) RevealLettersIfNeeded(now time.Time) bool {
	if g.Word == "" || g.Status != StatusInProgress {
		return false
	}
	start := g.TimedRounds.RoundStarted
	dur := g.TimedRounds.Duration
	elapsed := now.Sub(start)
	// At 50% we want 1 letter, at 75% we want 2 letters
	wantRevealed := 0
	if elapsed >= dur/2 {
		wantRevealed = 1
	}
	if elapsed >= (dur*3)/4 {
		wantRevealed = 2
	}
	if wantRevealed <= len(g.RevealedIndices) {
		return false
	}
	// Pick a random unrevealed index
	available := make([]int, 0, len(g.Word))
	revealedSet := make(map[int]bool)
	for _, i := range g.RevealedIndices {
		revealedSet[i] = true
	}
	for i := 0; i < len(g.Word); i++ {
		if !revealedSet[i] {
			available = append(available, i)
		}
	}
	if len(available) == 0 {
		return false
	}
	rng := rand.New(rand.NewSource(now.UnixNano()))
	idx := available[rng.Intn(len(available))]
	g.RevealedIndices = append(g.RevealedIndices, idx)
	sort.Ints(g.RevealedIndices)
	return true
}

// UpdateCanvas replaces the canvas (explainer only). Caller holds lock or doesn't; we lock inside.
func (g *Game) UpdateCanvas(playerID string, items []CanvasItem) bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.Status != StatusInProgress || g.ExplainerID != playerID {
		return false
	}
	g.Canvas = items
	return true
}

// SubmitGuess returns (correct, error). On correct, awards points to guesser and explainer by time remaining.
func (g *Game) SubmitGuess(playerID string, guess string, now time.Time) (bool, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.Status != StatusInProgress {
		return false, errors.New("game not in progress")
	}
	if playerID == g.ExplainerID {
		return false, errors.New("explainer cannot guess")
	}
	if _, ok := g.Players[playerID]; !ok {
		return false, errors.New("player not found")
	}
	g.advanceIfNeededLocked(now)
	if g.Status != StatusInProgress {
		return false, nil
	}
	if g.RoundWinnerID != "" {
		return false, nil // already solved
	}
	normalized := strings.ToLower(strings.TrimSpace(guess))
	normalized = strings.ReplaceAll(normalized, " ", "")
	if normalized == "" || g.Word == "" {
		return false, nil
	}
	if normalized != g.Word {
		return false, nil
	}
	// Award points based on remaining time.
	//
	// Guesser:  1–10 pts  (ceil(10 * remaining/duration)) — rewards fast guessing.
	// Explainer: 1–5 pts  (ceil(5  * remaining/duration)) — rewards clear explanations,
	//            but always less than the guesser earns, so deliberately explaining
	//            poorly to deny an opponent points is never a winning strategy.
	elapsed := now.Sub(g.TimedRounds.RoundStarted)
	remaining := g.TimedRounds.Duration - elapsed
	if remaining < 0 {
		remaining = 0
	}
	fraction := float64(remaining) / float64(g.TimedRounds.Duration)
	guesserPoints := int(math.Ceil(10 * fraction))
	if guesserPoints < 1 {
		guesserPoints = 1
	}
	explainerPoints := int(math.Ceil(5 * fraction))
	if explainerPoints < 1 {
		explainerPoints = 1
	}
	if guesser, ok := g.Players[playerID]; ok {
		guesser.Points += guesserPoints
	}
	if explainer, ok := g.Players[g.ExplainerID]; ok {
		explainer.Points += explainerPoints
	}
	g.RoundWinnerID = playerID
	g.RoundSolvedAt = now
	g.TimedRounds.RoundEndedAt = now
	return true, nil
}

func (g *Game) IsOwner(playerID string) bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	return playerID != "" && playerID == g.OwnerID
}

func (g *Game) PlayerName(playerID string) (string, bool) {
	g.mu.Lock()
	defer g.mu.Unlock()
	p, ok := g.Players[playerID]
	if !ok {
		return "", false
	}
	return p.Username, true
}

func (g *Game) WordLength() int {
	g.mu.Lock()
	defer g.mu.Unlock()
	return len(g.Word)
}

// RevealedWordForGuessers returns the word with only revealed positions filled (e.g. "a__l_").
func (g *Game) RevealedWordForGuessers() string {
	g.mu.Lock()
	defer g.mu.Unlock()
	return revealedWord(g.Word, g.RevealedIndices)
}

func revealedWord(word string, indices []int) string {
	if word == "" {
		return ""
	}
	runes := []rune(word)
	set := make(map[int]bool)
	for _, i := range indices {
		if i >= 0 && i < len(runes) {
			set[i] = true
		}
	}
	out := make([]rune, len(runes))
	for i := range runes {
		if set[i] {
			out[i] = runes[i]
		} else {
			out[i] = '_'
		}
	}
	return string(out)
}

// Snapshot for rendering.
type Snapshot struct {
	ID              string
	Status          string
	CurrentRound    int
	Rounds          int
	RoundDuration   time.Duration
	RoundStarted    time.Time
	RoundEndedAt    time.Time
	NextRoundAt     time.Time
	WordLength      int
	RevealedWord    string   // for guessers: e.g. "a__le"
	Word            string   // for explainer only (set in handler when role=explainer)
	ExplainerID     string
	ExplainerName   string
	RoundEmojis     []string
	Canvas          []CanvasItem
	Players         []PlayerInfo
	Scores          []ScoreEntry
	RoundWinnerName string
	WinnerName      string
	IsExplainer     bool
	IsGuesser       bool
}

type PlayerInfo struct {
	ID       string
	Name     string
	IsExplainer bool
}

type ScoreEntry struct {
	Name   string
	Points int
}

func (g *Game) Snapshot(now time.Time, playerID string) Snapshot {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.TimedRounds.Advance(now)
	g.RevealLettersIfNeeded(now)

	players := make([]PlayerInfo, 0, len(g.Players))
	scores := make([]ScoreEntry, 0, len(g.Players))
	for _, p := range g.Players {
		players = append(players, PlayerInfo{
			ID:          p.ID,
			Name:        p.Username,
			IsExplainer: p.ID == g.ExplainerID,
		})
		scores = append(scores, ScoreEntry{Name: p.Username, Points: p.Points})
	}
	sort.Slice(scores, func(i, j int) bool {
		if scores[i].Points != scores[j].Points {
			return scores[i].Points > scores[j].Points
		}
		return scores[i].Name < scores[j].Name
	})

	// Look up names directly — g.mu is already held, cannot call g.PlayerName() (would deadlock).
	explainerName := ""
	if p, ok := g.Players[g.ExplainerID]; ok {
		explainerName = p.Username
	}
	roundWinnerName := ""
	if p, ok := g.Players[g.RoundWinnerID]; ok {
		roundWinnerName = p.Username
	}
	winnerName := ""
	if g.Status == StatusFinished && len(scores) > 0 {
		winnerName = scores[0].Name
	}
	var nextRoundAt time.Time
	if !g.TimedRounds.RoundEndedAt.IsZero() {
		nextRoundAt = g.TimedRounds.RoundEndedAt.Add(g.TimedRounds.Cooldown)
	}

	wordForView := ""
	revealedWord := revealedWord(g.Word, g.RevealedIndices)
	if playerID == g.ExplainerID {
		wordForView = g.Word
	}
	// Reveal the full word to guessers once someone has guessed it correctly.
	if g.RoundWinnerID != "" {
		revealedWord = g.Word
	}

	return Snapshot{
		ID:              g.ID,
		Status:          g.Status,
		CurrentRound:   g.TimedRounds.CurrentRound,
		Rounds:         g.TimedRounds.Rounds,
		RoundDuration:  g.TimedRounds.Duration,
		RoundStarted:   g.TimedRounds.RoundStarted,
		RoundEndedAt:   g.TimedRounds.RoundEndedAt,
		NextRoundAt:    nextRoundAt,
		WordLength:     len(g.Word),
		RevealedWord:   revealedWord,
		Word:           wordForView,
		ExplainerID:    g.ExplainerID,
		ExplainerName:  explainerName,
		RoundEmojis:    append([]string(nil), g.RoundEmojis...),
		Canvas:         append([]CanvasItem(nil), g.Canvas...),
		Players:        players,
		Scores:         scores,
		RoundWinnerName: roundWinnerName,
		WinnerName:     winnerName,
		IsExplainer:    playerID == g.ExplainerID,
		IsGuesser:      playerID != "" && playerID != g.ExplainerID,
	}
}
