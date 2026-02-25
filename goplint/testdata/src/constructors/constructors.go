// SPDX-License-Identifier: MPL-2.0

package constructors

// Config has a NewConfig constructor — no diagnostic for constructor check.
type Config struct {
	Name string // want `struct field constructors\.Config\.Name uses primitive type string`
}

func NewConfig(name string) *Config { return &Config{Name: name} } // want `parameter "name" of constructors\.NewConfig uses primitive type string`

// Server has no NewServer constructor — should be flagged.
type Server struct { // want `exported struct constructors\.Server has no NewServer\(\) constructor`
	Addr string // want `struct field constructors\.Server\.Addr uses primitive type string`
}

// Client has no NewClient constructor — should be flagged.
type Client struct { // want `exported struct constructors\.Client has no NewClient\(\) constructor`
	URL string // want `struct field constructors\.Client\.URL uses primitive type string`
}

// unexportedStruct is unexported — should NOT be checked by constructors.
type unexportedStruct struct {
	data string // want `struct field constructors\.unexportedStruct\.data uses primitive type string`
}

// Options has NewOptions below.
type Options struct {
	Timeout int // want `struct field constructors\.Options\.Timeout uses primitive type int`
}

func NewOptions() *Options { return &Options{} }

// NamedType is not a struct — should NOT be checked by --check-constructors.
type NamedType string

// --- Error type exclusions ---

// ConnectionError is an error type by name — not flagged for missing constructor.
type ConnectionError struct {
	Message string // want `struct field constructors\.ConnectionError\.Message uses primitive type string`
}

func (e *ConnectionError) Error() string { return e.Message }

// ParseFailure implements error without the "Error" suffix — still skipped
// because it has an Error() method.
type ParseFailure struct {
	Msg string // want `struct field constructors\.ParseFailure\.Msg uses primitive type string`
}

func (e *ParseFailure) Error() string { return e.Msg }

// ServerError has "Error" suffix but no Error() method — still skipped by
// the naming convention.
type ServerError struct {
	Code int // want `struct field constructors\.ServerError\.Code uses primitive type int`
}

// --- Variant constructor (prefix match) ---

// Metadata has NewMetadataFromSource (variant constructor) — should NOT
// be flagged for missing constructor because the prefix match finds it.
type Metadata struct {
	id int // want `struct field constructors\.Metadata\.id uses primitive type int`
}

func NewMetadataFromSource(id int) *Metadata { return &Metadata{id: id} } // want `parameter "id" of constructors\.NewMetadataFromSource uses primitive type int`

// Result has NewResultFromData — variant constructor satisfies the check.
type Result struct {
	data string // want `struct field constructors\.Result\.data uses primitive type string`
}

func NewResultFromData(data string) *Result { return &Result{data: data} } // want `parameter "data" of constructors\.NewResultFromData uses primitive type string`
