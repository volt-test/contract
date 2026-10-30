package definition

import (
	"bytes"
	"encoding/json"
	"errors"
	"reflect"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"unicode/utf8"
)

// objectFormer is implemented by types that decode themselves with
// UnmarshalJSON and also accept a JSON object, such as a value that is
// either a string or an object. objectForm returns a value (or pointer) of
// the struct type that describes the object form; the walkers check object
// input against that struct's fields. Input that is not an object is
// checked by calling the type's UnmarshalJSON.
type objectFormer interface {
	objectForm() any
}

var (
	unmarshalerType  = reflect.TypeOf((*json.Unmarshaler)(nil)).Elem()
	objectFormerType = reflect.TypeOf((*objectFormer)(nil)).Elem()
)

func derefType(t reflect.Type) reflect.Type {
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	return t
}

var objectFormCache sync.Map // reflect.Type -> reflect.Type (nil when t has no object form)

// objectFormOf returns the struct type to walk for object input, or nil.
func objectFormOf(t reflect.Type) reflect.Type {
	if f, ok := objectFormCache.Load(t); ok {
		form, _ := f.(reflect.Type)
		return form
	}
	var form reflect.Type
	if reflect.PointerTo(t).Implements(objectFormerType) {
		form = derefType(reflect.TypeOf(reflect.New(t).Interface().(objectFormer).objectForm()))
	}
	objectFormCache.Store(t, form)
	return form
}

// kindProblem checks a non-null JSON value against a Go type that is not a
// struct, slice, array or map, the way encoding/json would decode it. It
// returns "" when the value fits, or a message that names JSON kinds only.
func kindProblem(raw []byte, t reflect.Type) string {
	if reflect.PointerTo(t).Implements(unmarshalerType) {
		v := reflect.New(t).Interface().(json.Unmarshaler)
		if err := v.UnmarshalJSON(raw); err != nil {
			return strings.TrimPrefix(err.Error(), "definition: ")
		}
		return ""
	}
	switch t.Kind() {
	case reflect.Interface:
		return ""
	case reflect.String:
		if raw[0] != '"' {
			return "must be a string"
		}
	case reflect.Bool:
		if string(raw) != "true" && string(raw) != "false" {
			return "must be true or false"
		}
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		if !isNumberStart(raw[0]) {
			return "must be an integer"
		}
		if _, err := strconv.ParseInt(string(raw), 10, t.Bits()); err != nil {
			return intMessage(raw, err)
		}
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		if !isNumberStart(raw[0]) {
			return "must be an integer"
		}
		if _, err := strconv.ParseUint(string(raw), 10, t.Bits()); err != nil {
			return intMessage(raw, err)
		}
	case reflect.Float32, reflect.Float64:
		if raw[0] != '-' && (raw[0] < '0' || raw[0] > '9') {
			return "must be a number"
		}
	}
	return ""
}

func isNumberStart(c byte) bool { return c == '-' || (c >= '0' && c <= '9') }

func intMessage(raw []byte, err error) string {
	var ne *strconv.NumError
	if errors.As(err, &ne) && ne.Err == strconv.ErrRange {
		return "is out of range"
	}
	if raw[0] == '-' || (raw[0] >= '0' && raw[0] <= '9') {
		return "must be an integer without a decimal point or exponent"
	}
	return "must be an integer"
}

// anyProblem checks a value decoded into an interface (any). encoding/json
// accepts every JSON value there except a number out of float64 range,
// such as 1e999, anywhere inside it. It returns the path of the first such
// number below the value ("" for the value itself) and a message.
//
// It reads the value in one pass, keeping a stack of the containers it is
// in, and builds a path only when it finds a number out of range, so deep
// nesting costs time and memory linear in its size.
func anyProblem(raw []byte) (sub, msg string, bad bool) {
	type frame struct {
		array bool
		index int
		key   []byte
	}
	var stack []frame
	expectKey := false
	for i := 0; i < len(raw); {
		switch c := raw[i]; {
		case c == '{':
			stack = append(stack, frame{})
			expectKey = true
			i++
		case c == '[':
			stack = append(stack, frame{array: true})
			i++
		case c == '}' || c == ']':
			if len(stack) == 0 {
				return "", "is not valid JSON", true
			}
			stack = stack[:len(stack)-1]
			expectKey = false
			i++
		case c == ',':
			if len(stack) > 0 {
				if top := &stack[len(stack)-1]; top.array {
					top.index++
				} else {
					expectKey = true
				}
			}
			i++
		case c == '"':
			end, err := skipValue(raw, i)
			if err != nil {
				return "", "is not valid JSON", true
			}
			if expectKey && len(stack) > 0 {
				key, err := decodeKey(raw[i:end])
				if err != nil {
					return "", "is not valid JSON", true
				}
				stack[len(stack)-1].key = key
				expectKey = false
			}
			i = end
		case isNumberStart(c):
			end, err := skipValue(raw, i)
			if err != nil {
				return "", "is not valid JSON", true
			}
			if _, err := strconv.ParseFloat(string(raw[i:end]), 64); err != nil {
				var b strings.Builder
				for _, f := range stack {
					if f.array {
						b.WriteByte('[')
						b.WriteString(strconv.Itoa(f.index))
						b.WriteByte(']')
						continue
					}
					seg := joinKey("", f.key) // "key" or ["odd key"]
					if b.Len() > 0 && seg[0] != '[' {
						b.WriteByte('.')
					}
					b.WriteString(seg)
				}
				return shortenPath(b.String()), "is out of range", true
			}
			i = end
		default:
			i++ // whitespace, ':' and the letters of true, false and null
		}
	}
	return "", "", false
}

// maxPath bounds a path inside an any value, which can nest thousands of
// levels deep.
const maxPath = 512

// shortenPath keeps the start and the end of a very long path.
func shortenPath(p string) string {
	if len(p) <= maxPath {
		return p
	}
	head := truncate(p, maxPath/2)
	head = strings.TrimSuffix(head, "...")
	tail := len(p) - maxPath/2
	for tail < len(p) && !utf8.RuneStart(p[tail]) {
		tail++
	}
	return head + "..." + p[tail:]
}

// appendPath joins a path inside a value to the value's path.
func appendPath(path, sub string) string {
	switch {
	case sub == "":
		return path
	case path == "" || sub[0] == '[':
		return path + sub
	}
	return path + "." + sub
}

// containerProblem checks that a non-null value has the JSON kind a
// struct, slice, array or map needs. ok is false for other Go kinds.
func containerProblem(raw []byte, t reflect.Type) (msg string, ok bool) {
	switch t.Kind() {
	case reflect.Struct, reflect.Map:
		if raw[0] != '{' {
			return "must be an object", true
		}
		return "", true
	case reflect.Slice:
		if t.Elem().Kind() == reflect.Uint8 {
			// encoding/json reads []byte from a base64 string.
			return kindProblem(raw, reflect.TypeOf("")), true
		}
		fallthrough
	case reflect.Array:
		if raw[0] != '[' {
			return "must be an array", true
		}
		return "", true
	}
	return "", false
}

// walker checks a v2 document against the Go types before it is decoded:
// every key must match a json tag exactly (encoding/json would accept any
// case), v1-only fields are rejected, and every value must have the JSON
// type its field expects. It walks every level of the document, so fields
// added to the types later are covered without changes here.
type walker struct {
	unknown  []string
	omitted  int // unknown keys beyond maxKept
	problems problemList
	// visited, when non-nil, records each struct type whose object was walked.
	visited map[reflect.Type]bool
	// objectForms records the paths of values written in the object form
	// of a type that also has a non-object form (a threshold object).
	objectForms map[string]bool
}

func (w *walker) problem(path, msg string) {
	w.problems.add(path, msg)
}

// smallObject is the number of members lastMembers handles without a map,
// and the size of the stack buffer callers give it.
const smallObject = 16

// member is one key and value of a JSON object.
type member struct {
	key []byte
	val []byte
}

// lastMembers appends an object's members to ms, sorted by key, keeping
// only the last occurrence of a duplicate key, as encoding/json does.
// Callers pass a small stack array so that small objects do not allocate.
func lastMembers(ms []member, raw []byte) ([]member, error) {
	var seen map[string]int // key -> index in ms, once the object is large
	err := forEachMember(raw, func(k, _, v []byte) {
		if seen == nil {
			if len(ms) < smallObject {
				// Small objects: a linear scan beats a map.
				for i := range ms {
					if bytes.Equal(ms[i].key, k) {
						ms[i].val = v
						return
					}
				}
				ms = append(ms, member{k, v})
				return
			}
			seen = make(map[string]int, 2*len(ms))
			for i, m := range ms {
				seen[string(m.key)] = i
			}
		}
		if i, ok := seen[string(k)]; ok {
			ms[i].val = v
			return
		}
		seen[string(k)] = len(ms)
		ms = append(ms, member{k, v})
	})
	if err != nil {
		return nil, err
	}
	slices.SortFunc(ms, func(a, b member) int { return bytes.Compare(a.key, b.key) })
	return ms, nil
}

// isLeafType reports whether values of t are plain JSON leaves (strings,
// numbers, booleans) that the walkers check without descending.
func isLeafType(t reflect.Type) bool {
	switch t.Kind() {
	case reflect.Struct, reflect.Map, reflect.Slice, reflect.Array, reflect.Interface:
		return false
	}
	return !reflect.PointerTo(t).Implements(unmarshalerType)
}

func indexPath(path string, i int) string {
	return path + "[" + strconv.Itoa(i) + "]"
}

func (w *walker) walk(raw json.RawMessage, t reflect.Type, path string) {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 {
		w.problem(path, "is empty")
		return
	}
	if string(raw) == "null" {
		if t.Kind() != reflect.Interface {
			w.problem(path, "must not be null")
		}
		return
	}
	t = derefType(t)
	if t == fieldsType {
		// Fields decodes itself, but its problems belong to its members:
		// each value must be a string and each name used once.
		if raw[0] != '{' {
			w.problem(path, "must be an object")
			return
		}
		if err := fieldsProblems(raw, path, w.problem); err != nil {
			w.problem(path, "must be an object")
		}
		return
	}
	if form := objectFormOf(t); form != nil && raw[0] == '{' {
		if w.objectForms == nil {
			w.objectForms = map[string]bool{}
		}
		w.objectForms[path] = true
		w.walkStruct(raw, form, path)
		return
	}
	if reflect.PointerTo(t).Implements(unmarshalerType) {
		if msg := kindProblem(raw, t); msg != "" {
			w.problem(path, msg)
		}
		return
	}
	if msg, ok := containerProblem(raw, t); ok {
		if msg != "" {
			w.problem(path, msg)
			return
		}
		switch t.Kind() {
		case reflect.Struct:
			w.walkStruct(raw, t, path)
		case reflect.Slice, reflect.Array:
			if t.Kind() == reflect.Slice && t.Elem().Kind() == reflect.Uint8 {
				return
			}
			et := derefType(t.Elem())
			leaf := isLeafType(et)
			err := forEachElement(raw, func(i int, e []byte) {
				if !leaf {
					w.walk(e, t.Elem(), indexPath(path, i))
					return
				}
				// Plain leaves are checked here so that a large array
				// builds a path only for the values it reports.
				msg := ""
				if string(e) == "null" {
					msg = "must not be null"
				} else {
					msg = kindProblem(e, et)
				}
				if msg != "" {
					if w.problems.full() {
						w.problems.omitted++
					} else {
						w.problem(indexPath(path, i), msg)
					}
				}
			})
			if err != nil {
				w.problem(path, "must be an array")
			}
		case reflect.Map:
			var stack [smallObject]member
			members, err := lastMembers(stack[:0], raw)
			if err != nil {
				w.problem(path, "must be an object")
				return
			}
			for _, m := range members {
				w.walk(m.val, t.Elem(), joinKey(path, m.key))
			}
		}
		return
	}
	if t.Kind() == reflect.Interface {
		if sub, msg, bad := anyProblem(raw); bad {
			w.problem(appendPath(path, sub), msg)
		}
		return
	}
	if msg := kindProblem(raw, t); msg != "" {
		w.problem(path, msg)
	}
}

func (w *walker) walkStruct(raw json.RawMessage, t reflect.Type, path string) {
	if raw[0] != '{' {
		w.problem(path, "must be an object")
		return
	}
	var stack [smallObject]member
	members, err := lastMembers(stack[:0], raw)
	if err != nil {
		w.problem(path, "must be an object")
		return
	}
	if w.visited != nil {
		w.visited[t] = true
	}
	fields := fieldsOf(t)
	for _, m := range members {
		f, ok := fields.byName[string(m.key)]
		if !ok || f.v1Only {
			if len(w.unknown) >= maxKept {
				w.omitted++
			} else {
				w.unknown = append(w.unknown, joinKey(path, m.key))
			}
			continue
		}
		w.walk(m.val, f.typ, joinKey(path, m.key))
	}
}

// v1walker reads a v1 document the way encoding/json decodes it into the
// types (keys match exactly first, then case-insensitively; unknown keys
// and null are ignored) and records every value of the wrong JSON type,
// split into core and loose problems. It also rewrites the document for
// decoding: unknown and v2-only keys are dropped, and loose values of the
// wrong type are removed (object members) or replaced by null (array
// elements), so that they decode as if absent.
//
// Loose problems are kept once per field: pattern is the path with array
// indexes and map keys wildcarded, and later values of the same field only
// raise the first problem's Count. The internal pattern writes an array
// index as "[*]" and a map key as ".*"; the public patterns of
// ReadSet write "[]" and stop at a map field.
type v1walker struct {
	core      problemList
	loose     []Problem
	loosePats map[[2]string]int // {pattern, message} -> index in loose
}

// elemProblem records a problem for element i of the array at path,
// building the element's path only when the problem is kept.
func (w *v1walker) elemProblem(path string, i int, pattern, msg string, loose bool) {
	if loose {
		if j, ok := w.loosePats[[2]string{pattern, msg}]; ok {
			w.loose[j].Count++
			return
		}
	} else if w.core.full() {
		w.core.omitted++
		return
	}
	w.problem(indexPath(path, i), pattern, msg, loose)
}

func (w *v1walker) problem(path, pattern, msg string, loose bool) {
	if !loose {
		w.core.add(path, msg)
		return
	}
	if w.loosePats == nil {
		w.loosePats = map[[2]string]int{}
	}
	key := [2]string{pattern, msg}
	if i, ok := w.loosePats[key]; ok {
		w.loose[i].Count++
		return
	}
	w.loosePats[key] = len(w.loose)
	w.loose = append(w.loose, Problem{Path: path, Message: msg, Count: 1})
}

// value returns the rewritten value and ok=false when the value itself has
// the wrong JSON type (the problem is already recorded).
func (w *v1walker) value(raw json.RawMessage, t reflect.Type, path, pattern string, loose bool) (json.RawMessage, bool) {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || string(raw) == "null" {
		return raw, true
	}
	t = derefType(t)
	if form := objectFormOf(t); form != nil && raw[0] == '{' {
		return w.object(raw, form, path, pattern, loose)
	}
	if t.Kind() == reflect.Interface {
		if sub, msg, bad := anyProblem(raw); bad {
			w.problem(appendPath(path, sub), pattern, msg, loose)
			return nil, false
		}
		return raw, true
	}
	if reflect.PointerTo(t).Implements(unmarshalerType) {
		if msg := kindProblem(raw, t); msg != "" {
			w.problem(path, pattern, msg, loose)
			return nil, false
		}
		return raw, true
	}
	msg, isContainer := containerProblem(raw, t)
	if !isContainer {
		msg = kindProblem(raw, t)
	}
	if msg != "" {
		w.problem(path, pattern, msg, loose)
		return nil, false
	}
	switch t.Kind() {
	case reflect.Struct:
		return w.object(raw, t, path, pattern, loose)
	case reflect.Map:
		return w.mapValue(raw, t, path, pattern, loose)
	case reflect.Slice, reflect.Array:
		if t.Kind() == reflect.Slice && t.Elem().Kind() == reflect.Uint8 {
			return raw, true
		}
		et := derefType(t.Elem())
		// Elements that are plain JSON leaves are checked here, so that an
		// array of a million values does not build a million paths.
		leaf := isLeafType(et)
		elemPattern := pattern + "[*]"
		var buf bytes.Buffer
		buf.Grow(len(raw))
		buf.WriteByte('[')
		err := forEachElement(raw, func(i int, e []byte) {
			if i > 0 {
				buf.WriteByte(',')
			}
			if leaf {
				if string(e) == "null" {
					buf.Write(e)
				} else if msg := kindProblem(e, et); msg != "" {
					w.elemProblem(path, i, elemPattern, msg, loose)
					buf.WriteString("null")
				} else {
					buf.Write(e)
				}
				return
			}
			out, ok := w.value(e, t.Elem(), indexPath(path, i), elemPattern, loose)
			if !ok {
				out = json.RawMessage("null")
			}
			buf.Write(out)
		})
		if err != nil {
			w.problem(path, pattern, "must be an array", loose)
			return nil, false
		}
		buf.WriteByte(']')
		return buf.Bytes(), true
	}
	return raw, true
}

func (w *v1walker) object(raw json.RawMessage, t reflect.Type, path, pattern string, loose bool) (json.RawMessage, bool) {
	fields := fieldsOf(t)
	var buf bytes.Buffer
	buf.WriteByte('{')
	n := 0
	err := forEachMember(raw, func(key, quoted, val []byte) {
		name, f, ok := fields.lookupFold(key)
		if !ok || f.v2Only {
			return
		}
		// The v1 readers decode into value structs, where null is a no-op:
		// an earlier value of a duplicate key survives. Dropping null for a
		// pointer field reproduces that here.
		if f.typ.Kind() == reflect.Pointer && isNull(val) {
			return
		}
		out, ok := w.value(val, f.typ, joinPath(path, name), joinPath(pattern, name), loose || f.loose)
		if !ok {
			return
		}
		if n > 0 {
			buf.WriteByte(',')
		}
		n++
		buf.Write(quoted)
		buf.WriteByte(':')
		buf.Write(out)
	})
	if err != nil {
		w.problem(path, pattern, "must be an object", loose)
		return nil, false
	}
	buf.WriteByte('}')
	return buf.Bytes(), true
}

func (w *v1walker) mapValue(raw json.RawMessage, t reflect.Type, path, pattern string, loose bool) (json.RawMessage, bool) {
	var buf bytes.Buffer
	buf.WriteByte('{')
	n := 0
	elemPattern := pattern + ".*"
	err := forEachMember(raw, func(key, quoted, val []byte) {
		out, ok := w.value(val, t.Elem(), joinKey(path, key), elemPattern, loose)
		if !ok {
			return
		}
		if n > 0 {
			buf.WriteByte(',')
		}
		n++
		buf.Write(quoted)
		buf.WriteByte(':')
		buf.Write(out)
	})
	if err != nil {
		w.problem(path, pattern, "must be an object", loose)
		return nil, false
	}
	buf.WriteByte('}')
	return buf.Bytes(), true
}

func isNull(b []byte) bool { return string(bytes.TrimSpace(b)) == "null" }

type fieldInfo struct {
	name   string // JSON name
	typ    reflect.Type
	v1Only bool // contract:"v1"
	v2Only bool // contract:"v2"
	loose  bool // contract:"loose"
}

type fieldSet struct {
	byName map[string]fieldInfo
	names  []string // sorted, for case-insensitive lookup
}

// lookupFold finds the field for a key the way encoding/json does: an
// exact match first, then a case-insensitive one.
func (fs fieldSet) lookupFold(key []byte) (string, fieldInfo, bool) {
	if f, ok := fs.byName[string(key)]; ok {
		return f.name, f, true
	}
	for _, n := range fs.names {
		if strings.EqualFold(n, string(key)) {
			return n, fs.byName[n], true
		}
	}
	return "", fieldInfo{}, false
}

var fieldCache sync.Map // reflect.Type -> fieldSet

// fieldsOf maps the JSON names of t's fields to their types, following the
// encoding/json rules the types here use: the name is the json tag up to
// the first comma, or the Go field name; "-" is skipped; fields of an
// untagged embedded struct are promoted, and direct fields win over them.
func fieldsOf(t reflect.Type) fieldSet {
	if f, ok := fieldCache.Load(t); ok {
		return f.(fieldSet)
	}
	fields := map[string]fieldInfo{}
	var direct []reflect.StructField
	for i := 0; i < t.NumField(); i++ {
		sf := t.Field(i)
		tag := sf.Tag.Get("json")
		if tag == "-" {
			continue
		}
		name, _, _ := strings.Cut(tag, ",")
		if sf.Anonymous && name == "" {
			if et := derefType(sf.Type); et.Kind() == reflect.Struct {
				for k, v := range fieldsOf(et).byName {
					fields[k] = v
				}
				continue
			}
		}
		if !sf.IsExported() {
			continue
		}
		direct = append(direct, sf)
	}
	for _, sf := range direct {
		name, _, _ := strings.Cut(sf.Tag.Get("json"), ",")
		if name == "" {
			name = sf.Name
		}
		flags := contractFlags(sf.Tag)
		fields[name] = fieldInfo{name: name, typ: sf.Type, v1Only: flags["v1"], v2Only: flags["v2"], loose: flags["loose"]}
	}
	fs := fieldSet{byName: fields}
	for k := range fields {
		fs.names = append(fs.names, k)
	}
	sort.Strings(fs.names)
	fieldCache.Store(t, fs)
	return fs
}

func contractFlags(tag reflect.StructTag) map[string]bool {
	flags := map[string]bool{}
	if c := tag.Get("contract"); c != "" {
		for _, f := range strings.Split(c, ",") {
			flags[f] = true
		}
	}
	return flags
}

// joinPath appends an object key to a path: "a.b" for plain keys,
// a["x.y"] for keys that would be ambiguous. Keys longer than maxEcho
// bytes are cut.
func joinPath(path, key string) string {
	key = truncate(key, maxEcho)
	if !plainKey(key) {
		return path + "[" + strconv.Quote(key) + "]"
	}
	if path == "" {
		return key
	}
	return path + "." + key
}

// joinKey is joinPath for a key held as bytes.
func joinKey(path string, key []byte) string {
	if len(key) <= maxEcho && plainKey(key) {
		if path == "" {
			return string(key)
		}
		return path + "." + string(key)
	}
	return joinPath(path, string(key))
}

// plainKey reports whether a key can follow a "." in a path unquoted.
func plainKey[T string | []byte](k T) bool {
	if len(k) == 0 {
		return false
	}
	for i := 0; i < len(k); i++ {
		c := k[i]
		if !(c == '_' || c == '-' || c >= '0' && c <= '9' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z') {
			return false
		}
	}
	return true
}
