package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

type setting interface {
	resolve(*Resolved) error
}

type schema struct {
	fields []setting
}

func newSchema(fields ...setting) schema { return schema{fields: fields} }

func (s schema) resolve(r *Resolved) error {
	for _, field := range s.fields {
		if err := field.resolve(r); err != nil {
			return err
		}
	}
	return nil
}

type field[T any] struct {
	path        string
	declared    func(Config) (T, bool)
	apply       func(*Config, T)
	override    func(Overrides) *T
	setOrigin   func(*Origins, Origin)
	parse       func(string) (T, error)
	envName     string
	environment bool
}

type fieldOption[T any] func(*field[T])

func register[T any](
	path string,
	parse func(string) (T, error),
	declared func(Config) (T, bool),
	apply func(*Config, T),
	options ...fieldOption[T],
) setting {
	f := field[T]{path: path, parse: parse, declared: declared, apply: apply}
	for _, option := range options {
		option(&f)
	}
	return f
}

func mutable[T any](override func(Overrides) *T, setOrigin func(*Origins, Origin)) fieldOption[T] {
	return func(f *field[T]) {
		f.override = override
		f.setOrigin = setOrigin
	}
}

func environment[T any](name ...string) fieldOption[T] {
	return func(f *field[T]) {
		f.environment = true
		if len(name) > 0 {
			f.envName = name[0]
		}
	}
}

func (f field[T]) resolve(r *Resolved) error {
	if value, ok := f.declared(r.Config); ok {
		f.apply(&r.Config, value)
		f.origin(&r.Origins, OriginConfig)
	} else {
		f.origin(&r.Origins, OriginDefault)
	}
	if f.override != nil {
		if value := f.override(r.Overrides); value != nil {
			f.apply(&r.Config, *value)
			f.origin(&r.Origins, OriginGUI)
		}
	}
	if !f.environment {
		return nil
	}
	name := f.envName
	if name == "" {
		name = environmentName(f.path)
	}
	raw, ok := os.LookupEnv(name)
	if !ok || raw == "" {
		return nil
	}
	value, err := f.parse(raw)
	if err != nil {
		return fmt.Errorf("%s=%q: %w", name, raw, err)
	}
	f.apply(&r.Config, value)
	f.origin(&r.Origins, OriginEnv)
	return nil
}

func (f field[T]) origin(origins *Origins, origin Origin) {
	if f.setOrigin != nil {
		f.setOrigin(origins, origin)
	}
}

func environmentName(path string) string {
	replacer := strings.NewReplacer(".", "_", "-", "_")
	return "ZAMO_" + strings.ToUpper(replacer.Replace(path))
}

func parseString(raw string) (string, error) { return raw, nil }

var settingsSchema = newSchema(
	register("database", parseString,
		func(c Config) (string, bool) { return c.Database, c.Database != "" },
		func(c *Config, value string) { c.Database = value },
		environment[string]("ZAMO_DB"),
	),
	register("ai_enabled", strconv.ParseBool,
		func(c Config) (bool, bool) {
			if c.AIEnabledRaw == nil {
				return false, false
			}
			return *c.AIEnabledRaw, true
		},
		func(c *Config, value bool) { c.AIEnabledRaw = new(value) },
		mutable(func(o Overrides) *bool { return o.AIEnabled },
			func(o *Origins, origin Origin) { o.AIEnabled = origin }),
		environment[bool](),
	),
	register("llm.provider", parseString,
		func(c Config) (string, bool) { return c.LLM.Provider, c.LLM.Provider != "" },
		func(c *Config, value string) { c.LLM.Provider = value },
		mutable(func(o Overrides) *string { return o.LLM.Provider },
			func(o *Origins, origin Origin) { o.Provider = origin }),
		environment[string](),
	),
	register("llm.model", parseString,
		func(c Config) (string, bool) { return c.LLM.Model, c.LLM.Model != "" },
		func(c *Config, value string) { c.LLM.Model = value },
		mutable(func(o Overrides) *string { return o.LLM.Model },
			func(o *Origins, origin Origin) { o.Model = origin }),
		environment[string](),
	),
	register("llm.base_url", parseString,
		func(c Config) (string, bool) { return c.LLM.BaseURL, c.LLM.BaseURL != "" },
		func(c *Config, value string) { c.LLM.BaseURL = value },
		mutable(func(o Overrides) *string { return o.LLM.BaseURL },
			func(o *Origins, origin Origin) { o.BaseURL = origin }),
		environment[string](),
	),
)
