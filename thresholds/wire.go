package thresholds

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

// SnapshotLimits bounds what DecodeSnapshot accepts. A field of zero or
// less takes its default.
type SnapshotLimits struct {
	// MaxBytes caps the encoded snapshot. Default 32 MiB: 256 histograms
	// of a dense shape (a random count of up to 2^20 in every bucket
	// encodes to about 92 KB; a realistic latency shape to about 12 KB),
	// plus headroom.
	MaxBytes int
	// MaxScopes caps the scopes: the test, each scenario and each step.
	// Default 1024.
	MaxScopes int
	// MaxHistograms caps the non-empty hdr fields. Default 256.
	MaxHistograms int
	// MaxNameBytes caps a scenario or step name, in bytes once decoded.
	// Default 4096.
	MaxNameBytes int
}

const (
	defaultSnapshotMaxBytes      = 32 << 20
	defaultSnapshotMaxScopes     = 1024
	defaultSnapshotMaxHistograms = 256
	defaultSnapshotMaxNameBytes  = 4096
)

func (l SnapshotLimits) withDefaults() SnapshotLimits {
	set := func(v *int, d int) {
		if *v <= 0 {
			*v = d
		}
	}
	set(&l.MaxBytes, defaultSnapshotMaxBytes)
	set(&l.MaxScopes, defaultSnapshotMaxScopes)
	set(&l.MaxHistograms, defaultSnapshotMaxHistograms)
	set(&l.MaxNameBytes, defaultSnapshotMaxNameBytes)
	return l
}

// The wire structs. Encode writes the *Out forms; decode reads the *In
// forms, which take the same keys.
type wireDistOut struct {
	Count int64 `json:"count"`
	SumUs int64 `json:"sum_us"`
	MinUs int64 `json:"min_us"`
	MaxUs int64 `json:"max_us"`
	// HDR is the EncodeHistogram text as a JSON string, carried once. A
	// []byte field would base64-encode it a second time.
	HDR string `json:"hdr,omitempty"`
}

type wireScopeOut struct {
	Requests      int64 `json:"requests"`
	Failed        int64 `json:"failed"`
	BytesSent     int64 `json:"bytes_sent"`
	BytesReceived int64 `json:"bytes_received"`

	Checks            *int64 `json:"checks,omitempty"`
	ChecksPassed      *int64 `json:"checks_passed,omitempty"`
	Iterations        *int64 `json:"iterations,omitempty"`
	DroppedIterations *int64 `json:"dropped_iterations,omitempty"`

	Duration          *wireDistOut `json:"duration,omitempty"`
	TTFB              *wireDistOut `json:"ttfb,omitempty"`
	IterationDuration *wireDistOut `json:"iteration_duration,omitempty"`
}

type wireScenarioOut struct {
	wireScopeOut
	Steps map[string]*wireScopeOut `json:"steps,omitempty"`
}

type wireSnapshotOut struct {
	Start     time.Time                   `json:"start"`
	End       time.Time                   `json:"end"`
	Responses int64                       `json:"responses"`
	Test      *wireScopeOut               `json:"test"`
	Scenarios map[string]*wireScenarioOut `json:"scenarios,omitempty"`
}

// wireInt is an integer that records whether its key was present and not
// null, so a missing required key is an error and never reads as zero.
// Unlike *int64 it decodes without allocating.
type wireInt struct {
	v   int64
	set bool
}

// errWireType is the refusal of a wire type's UnmarshalJSON. checkKeys
// refuses the same values first, naming the key: encoding/json reports
// where a method's error happened differently across Go versions.
var errWireType = errors.New("wrong type")

func (w *wireInt) UnmarshalJSON(b []byte) error {
	if string(b) == "null" {
		*w = wireInt{}
		return nil
	}
	v, ok := wireIntValue(b)
	if !ok {
		return errWireType
	}
	*w = wireInt{v: v, set: true}
	return nil
}

// wireIntValue parses an integer literal. No int64 takes more than 20
// bytes: refusing longer input before parsing it keeps a huge literal
// from being copied.
func wireIntValue(b []byte) (int64, bool) {
	if len(b) > 20 {
		return 0, false
	}
	v, err := strconv.ParseInt(string(b), 10, 64)
	return v, err == nil
}

// wireTime is a time that records whether its key was present and not
// null. It refuses long input before parsing it.
type wireTime struct {
	t   time.Time
	set bool
}

func (w *wireTime) UnmarshalJSON(b []byte) error {
	if string(b) == "null" {
		*w = wireTime{}
		return nil
	}
	t, ok := wireTimeValue(b)
	if !ok {
		return errWireType
	}
	*w = wireTime{t: t, set: true}
	return nil
}

func wireTimeValue(b []byte) (time.Time, bool) {
	var t time.Time
	if len(b) > 64 || t.UnmarshalJSON(b) != nil {
		return time.Time{}, false
	}
	return t, true
}

// wireHDR is the hdr text. EncodeSnapshot writes it as base64 in a JSON
// string, which needs no escape. Unquoting an escaped string would copy
// all of it before its length could be checked, so UnmarshalJSON refuses,
// before copying anything, a literal that is not a string, that is longer
// than DecodeHistogram accepts or that has an escape.
type wireHDR string

func (h *wireHDR) UnmarshalJSON(b []byte) error {
	// null leaves the value as it is, as for any string.
	if string(b) == "null" {
		return nil
	}
	if !plainHDR(b) {
		return errWireType
	}
	*h = wireHDR(b[1 : len(b)-1])
	return nil
}

func plainHDR(b []byte) bool {
	return len(b) >= 2 && b[0] == '"' && len(b)-2 <= maxEncodedHistogram && bytes.IndexByte(b, '\\') < 0
}

type wireDistIn struct {
	Count wireInt `json:"count"`
	SumUs wireInt `json:"sum_us"`
	MinUs wireInt `json:"min_us"`
	MaxUs wireInt `json:"max_us"`
	HDR   wireHDR `json:"hdr"`
}

type wireScopeIn struct {
	Requests      wireInt `json:"requests"`
	Failed        wireInt `json:"failed"`
	BytesSent     wireInt `json:"bytes_sent"`
	BytesReceived wireInt `json:"bytes_received"`

	Checks            wireInt `json:"checks"`
	ChecksPassed      wireInt `json:"checks_passed"`
	Iterations        wireInt `json:"iterations"`
	DroppedIterations wireInt `json:"dropped_iterations"`

	Duration          *wireDistIn `json:"duration"`
	TTFB              *wireDistIn `json:"ttfb"`
	IterationDuration *wireDistIn `json:"iteration_duration"`
}

// On decode the scenario and step maps stay raw: encoding/json never
// builds them, so DecodeSnapshot can count each scope before allocating
// it. Raw fields capture exactly what Unmarshal would have mapped, after
// its case-insensitive key matching; checkKeys has already refused a key
// given twice.
type wireScenarioIn struct {
	wireScopeIn
	Steps json.RawMessage `json:"steps"`
}

type wireSnapshotIn struct {
	Start     wireTime        `json:"start"`
	End       wireTime        `json:"end"`
	Responses wireInt         `json:"responses"`
	Test      *wireScopeIn    `json:"test"`
	Scenarios json.RawMessage `json:"scenarios"`
}

// EncodeSnapshot returns s in the JSON wire form (see the package
// documentation). It writes s as it is, without validating it, so a
// broken scope reaches the reader, whose DecodeSnapshot reports it. It
// leaves out nil scenarios and steps, distributions with Count 0, SumUs
// 0 and no recorded values, and the hdr of a distribution without a
// histogram. It fails on a nil snapshot, a histogram without the snapshot
// configuration (ErrHistogramConfig), and a Start or End outside years
// 0-9999.
func EncodeSnapshot(s *Snapshot) ([]byte, error) {
	if s == nil {
		return nil, fmt.Errorf("%w: nil snapshot", ErrInvalidScope)
	}
	out := wireSnapshotOut{Start: s.Start, End: s.End, Responses: s.Responses}
	var err error
	if out.Test, err = encodeScope(&s.Test, place{}); err != nil {
		return nil, err
	}
	for name, sc := range s.Scenarios {
		if sc == nil {
			continue
		}
		ws, err := encodeScope(&sc.Scope, place{scenario: name, depth: 1})
		if err != nil {
			return nil, err
		}
		wsc := &wireScenarioOut{wireScopeOut: *ws}
		for step, st := range sc.Steps {
			if st == nil {
				continue
			}
			w, err := encodeScope(st, place{scenario: name, step: step, depth: 2})
			if err != nil {
				return nil, err
			}
			if wsc.Steps == nil {
				wsc.Steps = map[string]*wireScopeOut{}
			}
			wsc.Steps[step] = w
		}
		if out.Scenarios == nil {
			out.Scenarios = map[string]*wireScenarioOut{}
		}
		out.Scenarios[name] = wsc
	}
	b, err := json.Marshal(out)
	if err != nil {
		return nil, fmt.Errorf("thresholds: encode snapshot: %w", err)
	}
	return b, nil
}

func encodeScope(sc *Scope, at place) (*wireScopeOut, error) {
	w := &wireScopeOut{
		Requests: sc.Requests, Failed: sc.Failed, BytesSent: sc.BytesSent, BytesReceived: sc.BytesReceived,
		Checks: sc.Checks, ChecksPassed: sc.ChecksPassed, Iterations: sc.Iterations, DroppedIterations: sc.DroppedIterations,
	}
	var err error
	if w.Duration, err = encodeDist(sc.Duration, at, "duration"); err != nil {
		return nil, err
	}
	if w.TTFB, err = encodeDist(sc.TTFB, at, "ttfb"); err != nil {
		return nil, err
	}
	if w.IterationDuration, err = encodeDist(sc.IterationDuration, at, "iteration_duration"); err != nil {
		return nil, err
	}
	return w, nil
}

func encodeDist(d Distribution, at place, key string) (*wireDistOut, error) {
	if d.Count == 0 && d.SumUs == 0 && (d.Hist == nil || d.Hist.TotalCount() == 0) {
		return nil, nil
	}
	w := &wireDistOut{Count: d.Count, SumUs: d.SumUs, MinUs: d.MinUs, MaxUs: d.MaxUs}
	if d.Hist != nil {
		b, err := EncodeHistogram(d.Hist)
		if err != nil {
			return nil, inSnapshot(err, at.String()+", "+key)
		}
		w.HDR = string(b)
	}
	return w, nil
}

// DecodeSnapshot reads a snapshot in the JSON wire form, the output of
// EncodeSnapshot. It is safe on bytes from another process: lim bounds
// its memory and time, and it never panics.
//
// It refuses, wrapping ErrMalformedSnapshot: input over lim.MaxBytes;
// input that is not valid UTF-8; invalid JSON or a value of the wrong
// type; a key of the top object, a scope or a distribution (unknown keys
// included) that is longer than 32 bytes, has an escape or is not ASCII;
// a key of the wire form given twice in the top object, a scope or a
// distribution, matched ignoring ASCII case (an unknown key may repeat);
// a missing or null start, end, responses or test; a start or end that
// is not an RFC 3339 time of at most 64 bytes; a missing or null required
// key in a scope or a distribution; scenarios, steps, a scenario or a
// step that is not an object (null included); more scopes than
// lim.MaxScopes, a name over lim.MaxNameBytes, or a scenario, or a step of
// one scenario, named twice; more hdr fields than lim.MaxHistograms; an
// hdr that is not a string, is longer than DecodeHistogram accepts or has
// an escape; and a distribution with count 0, which EncodeSnapshot never
// writes. It refuses with ErrInvalidScope a scope that Scope.Validate
// rejects, a zero start or end, an end before the start and a negative
// responses. An hdr DecodeHistogram refuses keeps its error
// (ErrMalformedHistogram, ErrHistogramConfig or ErrOverflow). Messages
// name the scope, with long names cut.
//
// Every limit is checked before any histogram is decoded. encoding/json
// never builds the scenario or step maps: DecodeSnapshot walks them,
// counting each scope and checking its name before it decodes it.
// Memory: transient up to about 3x lim.MaxBytes (the scenarios and the
// current scenario's steps as raw bytes, and the hdr texts), about 96 MiB
// at the default, plus about 1 KB per scope and one histogram decode at
// a time (at most about 1.5 MB); retained about 1 KB per scope plus about
// 188 KB per histogram, about 50 MB at the default limits. Allocations
// are a small constant per scope.
func DecodeSnapshot(data []byte, lim SnapshotLimits) (*Snapshot, error) {
	lim = lim.withDefaults()
	if len(data) > lim.MaxBytes {
		return nil, fmt.Errorf("%w: %d bytes, over the limit of %d", ErrMalformedSnapshot, len(data), lim.MaxBytes)
	}
	// encoding/json accepts invalid UTF-8 in a string or key and replaces
	// each bad byte with the three-byte U+FFFD in a doubling buffer, which
	// costs about 7 to 25 times the input before any limit below applies.
	// EncodeSnapshot always writes valid UTF-8.
	if !utf8.Valid(data) {
		return nil, fmt.Errorf("%w: not valid UTF-8", ErrMalformedSnapshot)
	}
	// Invalid JSON is left to Unmarshal, which refuses it before it
	// decodes anything.
	if json.Valid(data) {
		if err := checkKeys(data, keysTop, place{}, ""); err != nil {
			return nil, err
		}
	}
	// Unmarshal checks that the whole document is valid JSON before it
	// decodes anything, so the raw maps below are valid.
	var top wireSnapshotIn
	if err := json.Unmarshal(data, &top); err != nil {
		return nil, jsonError(err, "")
	}
	d := &snapshotDecoder{lim: lim, total: 1}
	s := &Snapshot{}
	if !top.Start.set {
		return nil, fmt.Errorf("%w: missing \"start\"", ErrMalformedSnapshot)
	}
	if !top.End.set {
		return nil, fmt.Errorf("%w: missing \"end\"", ErrMalformedSnapshot)
	}
	s.Start, s.End = top.Start.t, top.End.t
	if !top.Responses.set {
		return nil, fmt.Errorf("%w: missing \"responses\"", ErrMalformedSnapshot)
	}
	s.Responses = top.Responses.v
	if top.Test == nil {
		return nil, fmt.Errorf("%w: missing \"test\"", ErrMalformedSnapshot)
	}
	if err := d.scope(&s.Test, top.Test, place{}); err != nil {
		return nil, err
	}
	if top.Scenarios != nil {
		if err := d.scenarios(s, top.Scenarios); err != nil {
			return nil, err
		}
	}
	top = wireSnapshotIn{}

	for i := range d.hists {
		p := &d.hists[i]
		h, err := DecodeHistogram([]byte(p.text))
		if err != nil {
			return nil, inSnapshot(err, p.at.String()+", "+p.key)
		}
		p.dst.Hist, p.text = h, ""
	}
	for _, ps := range d.scopes {
		if err := ps.scope.check(); err != nil {
			return nil, inSnapshot(err, ps.at.String())
		}
	}
	if s.Start.IsZero() || s.End.IsZero() {
		return nil, fmt.Errorf("%w: snapshot has no start or end time", ErrInvalidScope)
	}
	if s.End.Before(s.Start) {
		return nil, fmt.Errorf("%w: snapshot ends before it starts", ErrInvalidScope)
	}
	if s.Responses < 0 {
		return nil, fmt.Errorf("%w: snapshot has negative responses", ErrInvalidScope)
	}
	return s, nil
}

// snapshotDecoder holds the running counts of one DecodeSnapshot call.
type snapshotDecoder struct {
	lim    SnapshotLimits
	total  int // scopes so far, the test included
	hists  []pendingHist
	scopes []placedScope
	// Reused for each scenario and step.
	scenario wireScenarioIn
	step     wireScopeIn
}

type pendingHist struct {
	dst  *Distribution
	text string
	at   place
	key  string
}

type placedScope struct {
	scope *Scope
	at    place
}

// place names a scope in messages.
type place struct {
	scenario, step string
	depth          int // 0 test, 1 scenario, 2 step
}

func (p place) String() string {
	switch p.depth {
	case 1:
		return "scenario " + quoteShort(p.scenario)
	case 2:
		return "scenario " + quoteShort(p.scenario) + ", step " + quoteShort(p.step)
	}
	return "test"
}

// members names the scenarios, or a scenario's steps, in messages.
func (p place) members() string {
	if p.depth == 0 {
		return "scenarios"
	}
	return p.String() + ", steps"
}

func (d *snapshotDecoder) scenarios(s *Snapshot, raw []byte) error {
	parent := place{depth: 0}
	ow, err := walkObject(raw, parent)
	if err != nil {
		return err
	}
	s.Scenarios = map[string]*ScenarioScope{}
	for {
		key, value, more, err := ow.next()
		if err != nil || !more {
			return err
		}
		name, err := d.member(key, value, parent, func(n string) bool { _, dup := s.Scenarios[n]; return dup })
		if err != nil {
			return err
		}
		at := place{scenario: name, depth: 1}
		if err := checkKeys(value, keysScope, at, ""); err != nil {
			return err
		}
		d.scenario = wireScenarioIn{}
		if err := json.Unmarshal(value, &d.scenario); err != nil {
			return jsonError(err, at.String())
		}
		sc := &ScenarioScope{}
		s.Scenarios[name] = sc
		if err := d.scope(&sc.Scope, &d.scenario.wireScopeIn, at); err != nil {
			return err
		}
		if d.scenario.Steps != nil {
			if err := d.steps(sc, at, d.scenario.Steps); err != nil {
				return err
			}
		}
	}
}

func (d *snapshotDecoder) steps(sc *ScenarioScope, parent place, raw []byte) error {
	ow, err := walkObject(raw, parent)
	if err != nil {
		return err
	}
	sc.Steps = map[string]*Scope{}
	for {
		key, value, more, err := ow.next()
		if err != nil || !more {
			return err
		}
		name, err := d.member(key, value, parent, func(n string) bool { _, dup := sc.Steps[n]; return dup })
		if err != nil {
			return err
		}
		at := place{scenario: parent.scenario, step: name, depth: 2}
		if err := checkKeys(value, keysScope, at, ""); err != nil {
			return err
		}
		d.step = wireScopeIn{}
		if err := json.Unmarshal(value, &d.step); err != nil {
			return jsonError(err, at.String())
		}
		st := &Scope{}
		sc.Steps[name] = st
		if err := d.scope(st, &d.step, at); err != nil {
			return err
		}
	}
}

// member counts one scope against MaxScopes, then decodes and checks its
// name, before anything of the scope is decoded.
func (d *snapshotDecoder) member(key, value []byte, parent place, seen func(string) bool) (string, error) {
	d.total++
	if d.total > d.lim.MaxScopes {
		return "", fmt.Errorf("%w: more than %d scopes", ErrMalformedSnapshot, d.lim.MaxScopes)
	}
	// A decoded byte takes at most 6 bytes of JSON (\u0000), so a longer
	// key is over the limit without decoding it.
	if len(key)-2 > 6*d.lim.MaxNameBytes {
		return "", fmt.Errorf("%w: %s: a name is longer than %d bytes", ErrMalformedSnapshot, parent.members(), d.lim.MaxNameBytes)
	}
	name, err := jsonString(key)
	if err != nil {
		return "", fmt.Errorf("%w: %s: invalid JSON", ErrMalformedSnapshot, parent.members())
	}
	if len(name) > d.lim.MaxNameBytes {
		return "", fmt.Errorf("%w: %s: name %s is longer than %d bytes", ErrMalformedSnapshot, parent.members(), quoteShort(name), d.lim.MaxNameBytes)
	}
	if seen(name) {
		return "", fmt.Errorf("%w: %s: %s appears twice", ErrMalformedSnapshot, parent.members(), quoteShort(name))
	}
	if value[0] != '{' {
		return "", fmt.Errorf("%w: %s: %s is not an object", ErrMalformedSnapshot, parent.members(), quoteShort(name))
	}
	return name, nil
}

// maxMemberKey caps a key of the top, scope and distribution objects, the
// keys encoding/json matches against struct fields. It folds each such
// key into a 32-byte stack buffer, and grows a heap buffer for a longer
// one; it copies one with an escape. A flood of such keys, or one huge
// key, would cost many times the input, and the key would be ignored.
// The wire form's keys are short lower-case ASCII.
const maxMemberKey = 32

// What checkKeys checks.
const (
	keysTop   = iota // the top object, then the test scope in it
	keysScope        // a scope, then its distributions
	keysDist         // a distribution
)

// memberKeys are the keys of the wire form that encoding/json decodes in
// each kind of object. steps, last, is decoded only in a scenario.
var memberKeys = [...][]string{
	keysTop: {"start", "end", "responses", "test", "scenarios"},
	keysScope: {"requests", "failed", "bytes_sent", "bytes_received",
		"checks", "checks_passed", "iterations", "dropped_iterations",
		"duration", "ttfb", "iteration_duration", "steps"},
	keysDist: {"count", "sum_us", "min_us", "max_us", "hdr"},
}

// checkKeys refuses, in the object raw, a key over maxMemberKey bytes,
// with an escape or with a byte outside ASCII. It refuses a key of
// memberKeys given twice: encoding/json decodes every occurrence, and
// some decodes allocate (an object again after a null, a copy of an hdr
// or of raw scenarios or steps, a time zone for a time whose offset is
// not whole hours), so a flood of one key would cost many times the
// input. An unknown key may repeat: encoding/json skips it. It refuses,
// naming the key, a value of memberKeys that is not null and not of the
// key's type. checkKeys then checks the same way each member that
// encoding/json decodes as an object below it: the test in the top
// object, and the distributions of a scope. It does not enter scenarios
// or steps, whose keys are names and which DecodeSnapshot checks as it
// walks them. raw is valid JSON; path is its key path, for messages. The
// keys it has passed are plain ASCII, so encoding/json matches them as
// keyIs does.
func checkKeys(raw []byte, kind int, at place, path string) error {
	i := skipSpace(raw, 0)
	if i >= len(raw) || raw[i] != '{' {
		// Every value below the top has been checked as null or an object.
		if kind == keysTop && i < len(raw) && raw[i] != 'n' {
			return fmt.Errorf("%w: not an object", ErrMalformedSnapshot)
		}
		return nil
	}
	where := func() string {
		switch kind {
		case keysScope:
			return at.String() + ": "
		case keysDist:
			return at.String() + ", " + path[strings.LastIndexByte(path, '.')+1:] + ": "
		}
		return ""
	}
	// below is the key path of a member; field names it in messages.
	below := func(name string) string {
		if path == "" {
			return name
		}
		return path + "." + name
	}
	field := func(name string) string {
		if at.depth > 0 {
			return at.String() + ": " + below(name)
		}
		return below(name)
	}
	names := memberKeys[kind]
	if kind == keysScope && at.depth != 1 {
		names = names[:len(names)-1]
	}
	var seen uint16
	ow := objectWalker{raw: raw, i: i + 1, parent: at}
	for {
		key, value, more, err := ow.next()
		if err != nil || !more {
			return err
		}
		if !plainKey(key) {
			return fmt.Errorf("%w: %sa key is longer than %d bytes, has an escape or is not ASCII", ErrMalformedSnapshot, where(), maxMemberKey)
		}
		for j, name := range names {
			if !keyIs(key, name) {
				continue
			}
			if seen&(1<<j) != 0 {
				return fmt.Errorf("%w: %s%q appears twice", ErrMalformedSnapshot, where(), name)
			}
			seen |= 1 << j
			if msg := badValue(name, value); msg != "" {
				return fmt.Errorf("%w: %s: %s", ErrMalformedSnapshot, field(name), msg)
			}
			switch {
			case kind == keysTop && name == "test":
				err = checkKeys(value, keysScope, place{}, below(name))
			case kind == keysScope && (name == "duration" || name == "ttfb" || name == "iteration_duration"):
				err = checkKeys(value, keysDist, at, below(name))
			}
			break
		}
		if err != nil {
			return err
		}
	}
}

// badValue says why value, of the wire key name, is refused, or is empty
// when it is null or of the key's type.
func badValue(name string, value []byte) string {
	if string(value) == "null" {
		return ""
	}
	switch name {
	case "start", "end":
		if _, ok := wireTimeValue(value); !ok {
			return "not an RFC 3339 time"
		}
	case "hdr":
		if !plainHDR(value) {
			return fmt.Sprintf("not a string of at most %d bytes without escapes", maxEncodedHistogram)
		}
	case "test", "duration", "ttfb", "iteration_duration":
		if value[0] != '{' {
			return "not an object"
		}
	case "scenarios", "steps":
		// Walked by DecodeSnapshot, which refuses a non-object.
	default:
		if _, ok := wireIntValue(value); !ok {
			return "wrong type or out of range"
		}
	}
	return ""
}

// plainKey reports whether the quoted key lit is at most maxMemberKey
// bytes of ASCII without escapes.
func plainKey(lit []byte) bool {
	inner := lit[1 : len(lit)-1]
	if len(inner) > maxMemberKey {
		return false
	}
	for _, c := range inner {
		if c >= utf8.RuneSelf || c == '\\' {
			return false
		}
	}
	return true
}

// keyIs reports whether the plain key lit matches the lower-case ASCII
// name as encoding/json matches a field: ignoring ASCII case.
func keyIs(lit []byte, name string) bool {
	inner := lit[1 : len(lit)-1]
	if len(inner) != len(name) {
		return false
	}
	for i, c := range inner {
		if 'A' <= c && c <= 'Z' {
			c += 'a' - 'A'
		}
		if c != name[i] {
			return false
		}
	}
	return true
}

// objectWalker walks a JSON object that json.Unmarshal has already
// validated, member by member, without decoding it.
type objectWalker struct {
	raw    []byte
	i      int
	parent place
}

func walkObject(raw []byte, parent place) (*objectWalker, error) {
	i := skipSpace(raw, 0)
	if i >= len(raw) || raw[i] != '{' {
		return nil, fmt.Errorf("%w: %s is not an object", ErrMalformedSnapshot, parent.members())
	}
	return &objectWalker{raw: raw, i: i + 1, parent: parent}, nil
}

// next returns the next member's key, quoted, and its value; more is
// false at the end of the object.
func (w *objectWalker) next() (key, value []byte, more bool, err error) {
	raw := w.raw
	i := skipSpace(raw, w.i)
	if i < len(raw) && raw[i] == '}' {
		return nil, nil, false, nil
	}
	if i < len(raw) && raw[i] == ',' {
		i = skipSpace(raw, i+1)
	}
	ks := i
	ke := skipValue(raw, ks)
	if ke < 0 || raw[ks] != '"' {
		return nil, nil, false, w.bad()
	}
	i = skipSpace(raw, ke)
	if i >= len(raw) || raw[i] != ':' {
		return nil, nil, false, w.bad()
	}
	vs := skipSpace(raw, i+1)
	ve := skipValue(raw, vs)
	if ve < 0 {
		return nil, nil, false, w.bad()
	}
	w.i = ve
	return raw[ks:ke], raw[vs:ve], true, nil
}

func (w *objectWalker) bad() error {
	return fmt.Errorf("%w: %s: invalid JSON", ErrMalformedSnapshot, w.parent.members())
}

// scope fills dst from w, checks the required keys and the distributions,
// and queues the histograms for decoding.
func (d *snapshotDecoder) scope(dst *Scope, w *wireScopeIn, at place) error {
	var err error
	need := func(f wireInt, key string) int64 {
		if !f.set && err == nil {
			err = fmt.Errorf("%w: %s: missing %q", ErrMalformedSnapshot, at, key)
		}
		return f.v
	}
	dst.Requests = need(w.Requests, "requests")
	dst.Failed = need(w.Failed, "failed")
	dst.BytesSent = need(w.BytesSent, "bytes_sent")
	dst.BytesReceived = need(w.BytesReceived, "bytes_received")
	if err != nil {
		return err
	}
	dst.Checks, dst.ChecksPassed = optional(w.Checks), optional(w.ChecksPassed)
	dst.Iterations, dst.DroppedIterations = optional(w.Iterations), optional(w.DroppedIterations)
	if err := d.dist(&dst.Duration, w.Duration, at, "duration"); err != nil {
		return err
	}
	if err := d.dist(&dst.TTFB, w.TTFB, at, "ttfb"); err != nil {
		return err
	}
	if err := d.dist(&dst.IterationDuration, w.IterationDuration, at, "iteration_duration"); err != nil {
		return err
	}
	d.scopes = append(d.scopes, placedScope{scope: dst, at: at})
	return nil
}

// optional is nil for a counter that was not measured (absent or null).
func optional(f wireInt) *int64 {
	if !f.set {
		return nil
	}
	v := f.v
	return &v
}

func (d *snapshotDecoder) dist(dst *Distribution, w *wireDistIn, at place, key string) error {
	if w == nil {
		return nil
	}
	for _, f := range []struct {
		f    wireInt
		name string
		dst  *int64
	}{
		{w.Count, "count", &dst.Count},
		{w.SumUs, "sum_us", &dst.SumUs},
		{w.MinUs, "min_us", &dst.MinUs},
		{w.MaxUs, "max_us", &dst.MaxUs},
	} {
		if !f.f.set {
			return fmt.Errorf("%w: %s, %s: missing %q", ErrMalformedSnapshot, at, key, f.name)
		}
		*f.dst = f.f.v
	}
	if dst.Count == 0 {
		return fmt.Errorf("%w: %s, %s: count is 0; leave the distribution out", ErrMalformedSnapshot, at, key)
	}
	if w.HDR == "" {
		return nil
	}
	if len(d.hists) >= d.lim.MaxHistograms {
		return fmt.Errorf("%w: more than %d histograms", ErrMalformedSnapshot, d.lim.MaxHistograms)
	}
	d.hists = append(d.hists, pendingHist{dst: dst, text: string(w.HDR), at: at, key: key})
	return nil
}

// jsonString decodes a JSON string literal the way encoding/json decodes
// map keys, escapes resolved. DecodeSnapshot has already refused input
// that is not valid UTF-8, so a literal without escapes is its own value.
func jsonString(lit []byte) (string, error) {
	inner := lit[1 : len(lit)-1]
	if bytes.IndexByte(inner, '\\') < 0 {
		return string(inner), nil
	}
	var s string
	err := json.Unmarshal(lit, &s)
	return s, err
}

// jsonError turns an encoding/json error into a bounded message: the
// error's own text can repeat a whole input literal. checkKeys has
// refused, naming the key, every value encoding/json could refuse on
// valid JSON, so only a syntax error is expected here.
func jsonError(err error, at string) error {
	if at != "" {
		at += ": "
	}
	var se *json.SyntaxError
	if errors.As(err, &se) {
		return fmt.Errorf("%w: %sinvalid JSON at byte %d", ErrMalformedSnapshot, at, se.Offset)
	}
	return fmt.Errorf("%w: %swrong type", ErrMalformedSnapshot, at)
}

func inSnapshot(err error, where string) error {
	return &mergeError{msg: "thresholds: snapshot: " + where + ": " + strings.TrimPrefix(err.Error(), "thresholds: "), err: err}
}

func skipSpace(b []byte, i int) int {
	for i < len(b) && isSpace(b[i]) {
		i++
	}
	return i
}

// skipValue returns the index just past the JSON value starting at b[i],
// or -1. b is known to be valid JSON, so it only tracks strings and
// nesting.
func skipValue(b []byte, i int) int {
	if i >= len(b) {
		return -1
	}
	switch b[i] {
	case '"':
		for j := i + 1; j < len(b); j++ {
			switch b[j] {
			case '\\':
				j++
			case '"':
				return j + 1
			}
		}
		return -1
	case '{', '[':
		depth := 0
		for j := i; j < len(b); j++ {
			switch b[j] {
			case '"':
				e := skipValue(b, j)
				if e < 0 {
					return -1
				}
				j = e - 1
			case '{', '[':
				depth++
			case '}', ']':
				if depth--; depth == 0 {
					return j + 1
				}
			}
		}
		return -1
	}
	j := i
	for j < len(b) && !isSpace(b[j]) && b[j] != ',' && b[j] != '}' && b[j] != ']' {
		j++
	}
	if j == i {
		return -1
	}
	return j
}
