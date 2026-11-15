package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/pelletier/go-toml/v2"
)

// Kinds of the keys Administration may change (add-admin-settings, design D2).
const (
	KindInt      = "int"      // a whole number within Min and Max
	KindDuration = "duration" // a Go duration ("15m", "168h") within Min and Max nanoseconds
	KindString   = "string"   // a single line of 1 to Max characters
	KindChoice   = "choice"   // one of Choices
)

// AdminKey is one config.toml key that Administration → Settings may change.
type AdminKey struct {
	Key     string   `json:"key"`
	Group   string   `json:"group"`
	Kind    string   `json:"kind"`
	Min     int64    `json:"min,omitempty"`
	Max     int64    `json:"max,omitempty"`
	Choices []string `json:"choices,omitempty"`
}

// Env is the environment variable that overrides the key (and locks it on the page).
func (k AdminKey) Env() string {
	return EnvPrefix + strings.ToUpper(strings.ReplaceAll(k.Key, ".", "_"))
}

const day = 24 * time.Hour

// AdminKeys is the allowlist: settings, never destinations, nothing irreversible (design D2). Later changes that add
// simple settings add their keys here.
var AdminKeys = []AdminKey{
	{Key: "files.max_size_bytes", Group: "files", Kind: KindInt, Min: 1 << 20, Max: 2 << 30},
	{Key: "files.max_per_post", Group: "files", Kind: KindInt, Min: 1, Max: 50},
	{Key: "session.idle_timeout", Group: "sessions", Kind: KindDuration, Min: int64(5 * time.Minute), Max: int64(90 * day)},
	{Key: "session.absolute_lifetime", Group: "sessions", Kind: KindDuration, Min: int64(time.Hour), Max: int64(365 * day)},
	{Key: "oidc.name", Group: "sso", Kind: KindString, Min: 1, Max: 40},
	{Key: "log.level", Group: "logging", Kind: KindChoice, Choices: []string{"debug", "info", "warn", "error"}},
	{Key: "log.format", Group: "logging", Kind: KindChoice, Choices: []string{"json", "text"}},
}

// Errors of the allowlist.
var (
	// ErrNotEditable is a key outside AdminKeys.
	ErrNotEditable = errors.New("not editable from Administration")
	// ErrUnusualFile is an edit the comparison refused: the file's layout is one the text edit cannot change safely.
	ErrUnusualFile = errors.New("config.toml has a layout the settings page cannot edit safely: edit it by hand")
)

// AdminValueError is a value refused for one key; Msg names the bound.
type AdminValueError struct{ Key, Msg string }

func (e *AdminValueError) Error() string { return e.Key + ": " + e.Msg }

// FindAdminKey returns the allowlisted key, or ErrNotEditable.
func FindAdminKey(key string) (AdminKey, error) {
	i := slices.IndexFunc(AdminKeys, func(k AdminKey) bool { return k.Key == key })
	if i < 0 {
		return AdminKey{}, fmt.Errorf("%s: %w", key, ErrNotEditable)
	}
	return AdminKeys[i], nil
}

// ParseAdminValue checks a JSON value for an allowlisted key and returns it as config.toml holds it: an int64 or a
// string. Durations are written as given (trimmed), after parsing.
func ParseAdminValue(key string, raw json.RawMessage) (any, error) {
	k, err := FindAdminKey(key)
	if err != nil {
		return nil, err
	}
	bad := func(format string, args ...any) error { return &AdminValueError{Key: key, Msg: fmt.Sprintf(format, args...)} }
	switch k.Kind {
	case KindInt:
		var n int64
		if json.Unmarshal(raw, &n) != nil {
			return nil, bad("must be a whole number")
		}
		if n < k.Min {
			return nil, bad("must be at least %d", k.Min)
		}
		if n > k.Max {
			return nil, bad("must be at most %d", k.Max)
		}
		return n, nil
	case KindDuration:
		var s string
		if json.Unmarshal(raw, &s) != nil {
			return nil, bad("must be a duration such as \"30m\" or \"168h\"")
		}
		s = strings.TrimSpace(s)
		d, err := time.ParseDuration(s)
		if err != nil || len(s) > 32 {
			return nil, bad("must be a duration such as \"30m\" or \"168h\"")
		}
		if d < time.Duration(k.Min) {
			return nil, bad("must be at least %s", FormatDuration(time.Duration(k.Min)))
		}
		if d > time.Duration(k.Max) {
			return nil, bad("must be at most %s", FormatDuration(time.Duration(k.Max)))
		}
		return s, nil
	case KindString:
		var s string
		if json.Unmarshal(raw, &s) != nil {
			return nil, bad("must be text")
		}
		s = strings.TrimSpace(s)
		if n := utf8.RuneCountInString(s); n < int(k.Min) || n > int(k.Max) || strings.ContainsFunc(s, isControl) {
			return nil, bad("must be one line of %d to %d characters", k.Min, k.Max)
		}
		return s, nil
	case KindChoice:
		var s string
		if json.Unmarshal(raw, &s) != nil || !slices.Contains(k.Choices, s) {
			return nil, bad("must be one of %s", strings.Join(k.Choices, ", "))
		}
		return s, nil
	}
	return nil, bad("unknown kind")
}

func isControl(r rune) bool { return r < 0x20 || r == 0x7f || r == ' ' || r == ' ' }

// FormatDuration writes d as a Go duration without trailing zero units ("168h", "1h30m").
func FormatDuration(d time.Duration) string {
	s := d.String()
	if strings.HasSuffix(s, "m0s") {
		s = strings.TrimSuffix(s, "0s")
	}
	if strings.HasSuffix(s, "h0m") {
		s = strings.TrimSuffix(s, "0m")
	}
	return s
}

// Field is one leaf setting with its value as the API shows it (durations formatted; nil for a secret).
type Field struct {
	Key    string
	Env    string
	Secret bool
	Value  any
}

// Fields lists every leaf setting of c in declaration order.
func (c *Config) Fields() []Field {
	var out []Field
	walk(reflect.ValueOf(c).Elem(), EnvPrefix, "", func(f reflect.Value, sf reflect.StructField, env, key string) error {
		fd := Field{Key: key, Env: env, Secret: sf.Tag.Get("secret") == "true"}
		switch {
		case fd.Secret:
			fd.Value = !f.IsZero() // set or not set, never the value
		case f.Type() == durationType:
			fd.Value = FormatDuration(time.Duration(f.Int()))
		case f.Kind() == reflect.Slice:
			fd.Value = append([]string{}, f.Interface().([]string)...)
		default:
			fd.Value = f.Interface()
		}
		out = append(out, fd)
		return nil
	})
	return out
}

// AdminValue is the value of an allowlisted key in c (int64 or string).
func (c *Config) AdminValue(key string) any {
	for _, f := range c.Fields() {
		if f.Key == key {
			if n, ok := f.Value.(int); ok {
				return int64(n)
			}
			return f.Value
		}
	}
	return nil
}

// FindFile returns the config file Load reads for path (the --config value): path itself, or the first of
// DefaultPaths that exists; "" when there is none.
func FindFile(path string) (string, error) {
	if path != "" {
		return filepath.Abs(path)
	}
	for _, p := range DefaultPaths {
		if _, err := os.Stat(p); err == nil {
			return filepath.Abs(p)
		} else if !errors.Is(err, fs.ErrNotExist) {
			return "", fmt.Errorf("config file %s: %w", p, err)
		}
	}
	return "", nil
}

// FileValues decodes a config.toml text as Load does (defaults first, unknown keys and secrets refused), without
// the environment and without Validate.
func FileValues(text []byte) (*Config, error) {
	cfg := Default()
	if err := decodeTOML(text, cfg); err != nil {
		return nil, err
	}
	if err := rejectFileSecrets(cfg); err != nil {
		return nil, err
	}
	return cfg, nil
}

// LoadUnvalidated is Load without Validate: what a helper needs to find the server (its address and TLS), even
// when a setting it does not use is wrong.
func LoadUnvalidated(path string, lookup func(string) (string, bool)) (*Config, error) {
	cfg := Default()
	data, file, err := readConfigFile(path, os.ReadFile)
	if err != nil {
		return nil, err
	}
	if data != nil {
		if err := decodeTOML(data, cfg); err != nil {
			return nil, fmt.Errorf("config file %s: %w", file, err)
		}
	}
	if err := applyEnv(reflect.ValueOf(cfg).Elem(), EnvPrefix, lookup, os.ReadFile); err != nil {
		return nil, err
	}
	return cfg, nil
}

// EditAdmin returns old with the allowlisted keys set to values (int64 or string, as ParseAdminValue returns
// them), every other line and comment kept (design D3). The result is refused unless it decodes as a
// configuration and differs from old in exactly those keys.
func EditAdmin(old []byte, values map[string]any) ([]byte, error) {
	if len(values) == 0 {
		return nil, errors.New("nothing to change")
	}
	for k, v := range values {
		raw, err := json.Marshal(v)
		if err != nil {
			return nil, err
		}
		if _, err := ParseAdminValue(k, raw); err != nil {
			return nil, err
		}
	}
	if _, err := FileValues(old); err != nil {
		return nil, fmt.Errorf("config.toml: %w", err)
	}
	text, err := SetKeys(string(old), values)
	if err != nil {
		return nil, err
	}
	if err := CheckEdit(old, []byte(text), values); err != nil {
		return nil, err
	}
	return []byte(text), nil
}

// SetKeys returns text with the given "table.key" values set, keeping comments, order and every other line: an
// existing line gets the new value and keeps its trailing comment, a missing key is added after the last line of
// its table, a missing table is appended. Ported from ad-manager (internal/config/panelkeys.go).
func SetKeys(text string, keys map[string]any) (string, error) {
	lines := strings.SplitAfter(text, "\n")
	done := map[string]bool{}
	lastOf := map[string]int{} // table → index of its last line (header or key)
	table := ""
	for i, ln := range lines {
		t := strings.TrimSpace(ln)
		if strings.HasPrefix(t, "[[") {
			table = "\x00" + t // an array of tables: none of its keys is ours
			continue
		}
		if strings.HasPrefix(t, "[") {
			if end := strings.Index(t, "]"); end > 0 {
				table = strings.TrimSpace(t[1:end])
				lastOf[table] = i
			}
			continue
		}
		k, rest, ok := strings.Cut(t, "=")
		if !ok || strings.HasPrefix(t, "#") {
			continue
		}
		lastOf[table] = i
		full := strings.TrimSpace(k)
		if table != "" {
			full = table + "." + full
		}
		v, ok := keys[full]
		if !ok {
			continue
		}
		enc, err := tomlValue(v)
		if err != nil {
			return "", err
		}
		indent := ln[:len(ln)-len(strings.TrimLeft(ln, " \t"))]
		nl := ln[len(strings.TrimRight(ln, "\r\n")):]
		lines[i] = indent + strings.TrimSpace(k) + " = " + enc + trailingComment(rest) + nl
		done[full] = true
	}
	var missing []string
	for k := range keys {
		if !done[k] {
			missing = append(missing, k)
		}
	}
	slices.Sort(missing)
	var tail strings.Builder
	added := map[string]bool{}
	for _, k := range missing {
		tbl, key, _ := strings.Cut(k, ".")
		enc, err := tomlValue(keys[k])
		if err != nil {
			return "", err
		}
		line := key + " = " + enc + "\n"
		if i, ok := lastOf[tbl]; ok {
			if !strings.HasSuffix(lines[i], "\n") {
				lines[i] += "\n"
			}
			lines[i] += line
			continue
		}
		if !added[tbl] {
			tail.WriteString("\n[" + tbl + "]\n")
			added[tbl] = true
		}
		tail.WriteString(line)
	}
	out := strings.Join(lines, "")
	if tail.Len() > 0 {
		if out != "" && !strings.HasSuffix(out, "\n") {
			out += "\n"
		}
		out += tail.String()
	}
	return out, nil
}

// trailingComment returns the whitespace and "# comment" after a one-line value, "" when there is none. Quotes are
// followed so a '#' inside a string is not taken for one; a value spread over several lines is left to CheckEdit.
func trailingComment(value string) string {
	value = strings.TrimRight(value, "\r\n")
	var quote byte
	for i := 0; i < len(value); i++ {
		c := value[i]
		switch {
		case quote == '"' && c == '\\':
			i++
		case quote != 0 && c == quote:
			quote = 0
		case quote == 0 && (c == '"' || c == '\''):
			quote = c
		case quote == 0 && c == '#':
			j := i
			for j > 0 && (value[j-1] == ' ' || value[j-1] == '\t') {
				j--
			}
			gap := value[j:i]
			if gap == "" {
				gap = " "
			}
			return gap + value[i:]
		}
	}
	return ""
}

// tomlValue encodes one int64 or string as TOML.
func tomlValue(v any) (string, error) {
	switch x := v.(type) {
	case int64:
		return strconv.FormatInt(x, 10), nil
	case string:
		b, err := toml.Marshal(map[string]any{"v": x})
		if err != nil {
			return "", err
		}
		_, enc, _ := strings.Cut(strings.TrimSpace(string(b)), "=")
		return strings.TrimSpace(enc), nil
	}
	return "", fmt.Errorf("unsupported value %T", v)
}

// CheckEdit makes sure edited is old with exactly keys changed: the whole TOML documents are compared, unknown
// keys and multi-line values included, and edited still decodes as a configuration (ad-manager's CheckEdit).
func CheckEdit(old, edited []byte, keys map[string]any) error {
	var want, got map[string]any
	if err := toml.Unmarshal(old, &want); err != nil {
		return fmt.Errorf("config.toml: %w", err)
	}
	if err := toml.Unmarshal(edited, &got); err != nil {
		return fmt.Errorf("%w (%v)", ErrUnusualFile, err)
	}
	if _, err := FileValues(edited); err != nil {
		return fmt.Errorf("%w (%v)", ErrUnusualFile, err)
	}
	if want == nil {
		want = map[string]any{}
	}
	for k, v := range keys {
		tbl, key, _ := strings.Cut(k, ".")
		t, ok := want[tbl].(map[string]any)
		if !ok {
			t = map[string]any{}
			want[tbl] = t
		}
		t[key] = v
	}
	if !reflect.DeepEqual(want, got) {
		return ErrUnusualFile
	}
	return nil
}
