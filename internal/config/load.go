package config

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"reflect"
	"strconv"
	"strings"

	"github.com/pelletier/go-toml/v2"
)

// EnvPrefix prefixes every environment variable MeetHub reads.
const EnvPrefix = "MEETHUB_"

// DefaultPaths are tried in order when no --config path is given.
var DefaultPaths = []string{"config.toml", "/etc/meethub/config.toml"}

// Load builds the configuration from defaults, the TOML file and the
// environment (os.LookupEnv), then validates it. path is the --config value;
// when empty, DefaultPaths are tried and a missing file is not an error.
func Load(path string) (*Config, error) {
	return LoadWith(path, os.LookupEnv, os.ReadFile)
}

// LoadWith is Load with injectable environment lookup and file reading, for tests.
func LoadWith(path string, lookup func(string) (string, bool), readFile func(string) ([]byte, error)) (*Config, error) {
	cfg := Default()

	data, file, err := readConfigFile(path, readFile)
	if err != nil {
		return nil, err
	}
	if data != nil {
		if err := decodeTOML(data, cfg); err != nil {
			return nil, fmt.Errorf("config file %s: %w", file, err)
		}
		if err := rejectFileSecrets(cfg); err != nil {
			return nil, fmt.Errorf("config file %s: %w", file, err)
		}
	}
	if err := applyEnv(reflect.ValueOf(cfg).Elem(), EnvPrefix, lookup, readFile); err != nil {
		return nil, err
	}
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("invalid configuration:\n%w", err)
	}
	return cfg, nil
}

func readConfigFile(path string, readFile func(string) ([]byte, error)) ([]byte, string, error) {
	if path != "" {
		data, err := readFile(path)
		if err != nil {
			return nil, "", fmt.Errorf("read config file: %w", err)
		}
		return data, path, nil
	}
	for _, p := range DefaultPaths {
		data, err := readFile(p)
		if err == nil {
			return data, p, nil
		}
		if !errors.Is(err, fs.ErrNotExist) {
			return nil, "", fmt.Errorf("read config file %s: %w", p, err)
		}
	}
	return nil, "", nil
}

func decodeTOML(data []byte, cfg *Config) error {
	dec := toml.NewDecoder(strings.NewReader(string(data)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(cfg); err != nil {
		var strict *toml.StrictMissingError
		if errors.As(err, &strict) {
			return fmt.Errorf("unknown settings:\n%s", strict.String())
		}
		return err
	}
	return nil
}

// rejectFileSecrets fails when any secret field was set by the TOML file.
func rejectFileSecrets(cfg *Config) error {
	var errs []error
	walk(reflect.ValueOf(cfg).Elem(), EnvPrefix, "", func(f reflect.Value, sf reflect.StructField, env, key string) error {
		if sf.Tag.Get("secret") == "true" && !f.IsZero() {
			errs = append(errs, fmt.Errorf("%s: secrets must come from %s or %s_FILE, not the config file", key, env, env))
		}
		return nil
	})
	return errors.Join(errs...)
}

// applyEnv overrides fields from MEETHUB_<SECTION>_<FIELD> variables (and
// <NAME>_FILE for secrets).
func applyEnv(v reflect.Value, prefix string, lookup func(string) (string, bool), readFile func(string) ([]byte, error)) error {
	var errs []error
	walk(v, prefix, "", func(f reflect.Value, sf reflect.StructField, env, key string) error {
		val, ok := lookup(env)
		if sf.Tag.Get("secret") == "true" {
			fileVal, fileOK := lookup(env + "_FILE")
			if ok && fileOK {
				errs = append(errs, fmt.Errorf("%s and %s_FILE are both set: use one", env, env))
				return nil
			}
			if fileOK {
				b, err := readFile(fileVal)
				if err != nil {
					errs = append(errs, fmt.Errorf("%s_FILE: %w", env, err))
					return nil
				}
				val, ok = strings.TrimRight(string(b), "\r\n"), true
			}
		}
		if !ok {
			return nil
		}
		if err := setValue(f, val); err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", env, err))
		}
		return nil
	})
	return errors.Join(errs...)
}

// walk visits every leaf field with its environment variable name and TOML key.
func walk(v reflect.Value, envPrefix, keyPrefix string, fn func(f reflect.Value, sf reflect.StructField, env, key string) error) {
	t := v.Type()
	for i := range t.NumField() {
		sf := t.Field(i)
		name := strings.Split(sf.Tag.Get("toml"), ",")[0]
		if name == "" || name == "-" {
			continue
		}
		f := v.Field(i)
		env := envPrefix + strings.ToUpper(name)
		key := keyPrefix + name
		if f.Kind() == reflect.Struct {
			walk(f, env+"_", key+".", fn)
			continue
		}
		_ = fn(f, sf, env, key)
	}
}

var durationType = reflect.TypeFor[Duration]()

func setValue(f reflect.Value, s string) error {
	if f.Type() == durationType {
		var d Duration
		if err := d.UnmarshalText([]byte(s)); err != nil {
			return err
		}
		f.Set(reflect.ValueOf(d))
		return nil
	}
	switch f.Kind() {
	case reflect.String:
		f.SetString(s)
	case reflect.Int, reflect.Int64:
		n, err := strconv.ParseInt(strings.TrimSpace(s), 10, 64)
		if err != nil {
			return fmt.Errorf("not an integer: %q", s)
		}
		f.SetInt(n)
	case reflect.Bool:
		b, err := strconv.ParseBool(strings.TrimSpace(s))
		if err != nil {
			return fmt.Errorf("not a boolean: %q", s)
		}
		f.SetBool(b)
	case reflect.Slice:
		var items []string
		for p := range strings.SplitSeq(s, ",") {
			if p = strings.TrimSpace(p); p != "" {
				items = append(items, p)
			}
		}
		f.Set(reflect.ValueOf(items))
	default:
		return fmt.Errorf("unsupported setting type %s", f.Kind())
	}
	return nil
}

// Redacted returns a copy safe to print or log: every secret replaced by "***".
func (c *Config) Redacted() Config {
	cp := *c
	walk(reflect.ValueOf(&cp).Elem(), EnvPrefix, "", func(f reflect.Value, sf reflect.StructField, _, _ string) error {
		if sf.Tag.Get("secret") == "true" && !f.IsZero() {
			f.SetString("***")
		}
		return nil
	})
	cp.HTTP.TrustedProxies = append([]string(nil), c.HTTP.TrustedProxies...)
	return cp
}
