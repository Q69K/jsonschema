// Package jsonschema uses reflection to generate JSON Schemas from Go types [1].
//
// If json tags are present on struct fields, they will be used to infer
// property names and if a property is required (omitempty is present).
//
// [1] http://json-schema.org/latest/json-schema-validation.html
package jsonschema

import (
	"errors"
	"fmt"
	"net"
	"net/url"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Version is the JSON Schema version.
// If extending JSON Schema with custom values use a custom URI.
// RFC draft-wright-json-schema-00, section 6
var Version = "http://json-schema.org/draft-04/schema#"

// Schema is the root schema.
// RFC draft-wright-json-schema-00, section 4.5
type Schema struct {
	*Type
	Definitions Definitions `json:"definitions,omitempty"`
}

// Type represents a JSON Schema object type.
type Type struct {
	// RFC draft-wright-json-schema-00
	Version string `json:"$schema,omitempty"` // section 6.1
	Ref     string `json:"$ref,omitempty"`    // section 7
	// RFC draft-wright-json-schema-validation-00, section 5
	MultipleOf           int              `json:"multipleOf,omitempty"`           // section 5.1
	Maximum              int              `json:"maximum,omitempty"`              // section 5.2
	ExclusiveMaximum     bool             `json:"exclusiveMaximum,omitempty"`     // section 5.3
	Minimum              int              `json:"minimum,omitempty"`              // section 5.4
	ExclusiveMinimum     bool             `json:"exclusiveMinimum,omitempty"`     // section 5.5
	MaxLength            int              `json:"maxLength,omitempty"`            // section 5.6
	MinLength            int              `json:"minLength,omitempty"`            // section 5.7
	Pattern              string           `json:"pattern,omitempty"`              // section 5.8
	AdditionalItems      *Type            `json:"additionalItems,omitempty"`      // section 5.9
	Items                *Type            `json:"items,omitempty"`                // section 5.9
	MaxItems             int              `json:"maxItems,omitempty"`             // section 5.10
	MinItems             int              `json:"minItems,omitempty"`             // section 5.11
	UniqueItems          bool             `json:"uniqueItems,omitempty"`          // section 5.12
	MaxProperties        int              `json:"maxProperties,omitempty"`        // section 5.13
	MinProperties        int              `json:"minProperties,omitempty"`        // section 5.14
	Required             []string         `json:"required,omitempty"`             // section 5.15
	Properties           map[string]*Type `json:"properties,omitempty"`           // section 5.16
	PatternProperties    map[string]*Type `json:"patternProperties,omitempty"`    // section 5.17
	AdditionalProperties interface{}      `json:"additionalProperties,omitempty"` // section 5.18
	Dependencies         map[string]*Type `json:"dependencies,omitempty"`         // section 5.19
	Enum                 []interface{}    `json:"enum,omitempty"`                 // section 5.20
	Type                 string           `json:"type,omitempty"`                 // section 5.21
	AllOf                []*Type          `json:"allOf,omitempty"`                // section 5.22
	AnyOf                []*Type          `json:"anyOf,omitempty"`                // section 5.23
	OneOf                []*Type          `json:"oneOf,omitempty"`                // section 5.24
	Not                  *Type            `json:"not,omitempty"`                  // section 5.25
	Definitions          Definitions      `json:"definitions,omitempty"`          // section 5.26
	// RFC draft-wright-json-schema-validation-00, section 6, 7
	Title       string        `json:"title,omitempty"`       // section 6.1
	Description string        `json:"description,omitempty"` // section 6.1
	Default     interface{}   `json:"default,omitempty"`     // section 6.2
	Format      string        `json:"format,omitempty"`      // section 7
	Examples    []interface{} `json:"examples,omitempty"`    // section 7.4
	// RFC draft-wright-json-schema-hyperschema-00, section 4
	Media          *Type  `json:"media,omitempty"`          // section 4.3
	BinaryEncoding string `json:"binaryEncoding,omitempty"` // section 4.3
}

// Reflect reflects to Schema from a value using the default Reflector
func Reflect(v interface{}) *Schema {
	return ReflectFromType(reflect.TypeOf(v))
}

// ReflectFromType generates root schema using the default Reflector
func ReflectFromType(t reflect.Type) *Schema {
	r := &Reflector{}
	return r.ReflectFromType(t)
}

// A Reflector reflects values into a Schema.
type Reflector struct {
	// AllowAdditionalProperties will cause the Reflector to generate a schema
	// with additionalProperties to 'true' for all struct types. This means
	// the presence of additional keys in JSON objects will not cause validation
	// to fail. Note said additional keys will simply be dropped when the
	// validated JSON is unmarshaled.
	AllowAdditionalProperties bool

	// RequiredFromJSONSchemaTags will cause the Reflector to generate a schema
	// that requires any key tagged with `jsonschema:required`, overriding the
	// default of requiring any key *not* tagged with `json:,omitempty`.
	RequiredFromJSONSchemaTags bool

	// ExpandedStruct will cause the toplevel definitions of the schema not
	// be referenced itself to a definition.
	ExpandedStruct bool

	// IgnoredTypes defines a slice of types that should be ignored in the schema,
	// switching to just allowing additional properties instead.
	IgnoredTypes []interface{}

	// TypeMapper is a function that can be used to map custom Go types to jsconschema types.
	TypeMapper func(reflect.Type) *Type

	DiscriminatedTypes []DiscriminatorType
	EnumTypes          []EnumType
}

type DiscriminatorType struct {
	BaseType           reflect.Type
	DiscriminatorField string
	Ancestors          map[string]reflect.Type
}

type EnumType struct {
	Type   reflect.Type
	Values []interface{}
}

// Reflect reflects to Schema from a value.
func (r *Reflector) Reflect(v interface{}) *Schema {
	return r.ReflectFromType(reflect.TypeOf(v))
}

// ReflectFromType generates root schema
func (r *Reflector) ReflectFromType(t reflect.Type) *Schema {
	definitions := Definitions{}
	if r.ExpandedStruct {
		st := &Type{
			Version:              Version,
			Type:                 "object",
			Properties:           map[string]*Type{},
			AdditionalProperties: r.AllowAdditionalProperties,
		}
		r.reflectStructFields(st, definitions, t)
		r.reflectStruct(definitions, t)
		delete(definitions, t.Name())
		return &Schema{Type: st, Definitions: definitions}
	}

	s := &Schema{
		Type:        r.reflectTypeToSchema(definitions, t),
		Definitions: definitions,
	}
	return s
}

func (r *Reflector) RegisterEnumType(enumType reflect.Type, values []interface{}) error {
	for _, v := range values {
		if reflect.TypeOf(v) != enumType {
			return errors.New(fmt.Sprintf("value (%v) is not %s type", v, enumType.String()))
		}
	}

	r.EnumTypes = append(r.EnumTypes, EnumType{
		Type:   enumType,
		Values: values,
	})

	return nil
}

func (r *Reflector) RegisterDiscriminatorType(baseType reflect.Type, discriminatorField string, ancestors map[string]reflect.Type) error {
	if baseType.Kind() != reflect.Interface {
		return errors.New(fmt.Sprintf("base type %s should be an interface", baseType.Name()))
	}

	for _, t := range ancestors {
		if !t.Implements(baseType) {
			return errors.New(fmt.Sprintf("type %s should implement %s", t.Name(), baseType.Name()))
		}
	}

	r.DiscriminatedTypes = append(r.DiscriminatedTypes, DiscriminatorType{
		BaseType:           baseType,
		DiscriminatorField: discriminatorField,
		Ancestors:          ancestors,
	})

	return nil
}

// Definitions hold schema definitions.
// http://json-schema.org/latest/json-schema-validation.html#rfc.section.5.26
// RFC draft-wright-json-schema-validation-00, section 5.26
type Definitions map[string]*Type

// Available Go defined types for JSON Schema Validation.
// RFC draft-wright-json-schema-validation-00, section 7.3
var (
	timeType = reflect.TypeOf(time.Time{}) // date-time RFC section 7.3.1
	ipType   = reflect.TypeOf(net.IP{})    // ipv4 and ipv6 RFC section 7.3.4, 7.3.5
	uriType  = reflect.TypeOf(url.URL{})   // uri RFC section 7.3.6
)

// Byte slices will be encoded as base64
var byteSliceType = reflect.TypeOf([]byte(nil))

// Go code generated from protobuf enum types should fulfil this interface.
type protoEnum interface {
	EnumDescriptor() ([]byte, []int)
}

var protoEnumType = reflect.TypeOf((*protoEnum)(nil)).Elem()

func (r *Reflector) reflectTypeToSchema(definitions Definitions, t reflect.Type) *Type {
	// Already added to definitions?
	if _, ok := definitions[t.Name()]; ok {
		return &Type{Ref: "#/definitions/" + t.Name()}
	}

	// jsonpb will marshal protobuf enum options as either strings or integers.
	// It will unmarshal either.
	if t.Implements(protoEnumType) {
		return &Type{OneOf: []*Type{
			{Type: "string"},
			{Type: "integer"},
		}}
	}

	enumType := r.getEnum(t)
	if enumType != nil {
		var defaultValue interface{} = nil
		if len(enumType.Values) > 0 {
			defaultValue = enumType.Values[0]
		}
		return &Type{
			Default: defaultValue,
			Enum:    enumType.Values,
		}
	}

	if r.TypeMapper != nil {
		if t := r.TypeMapper(t); t != nil {
			return t
		}
	}

	// Defined format types for JSON Schema Validation
	// RFC draft-wright-json-schema-validation-00, section 7.3
	// TODO email RFC section 7.3.2, hostname RFC section 7.3.3, uriref RFC section 7.3.7
	switch t {
	case ipType:
		// TODO differentiate ipv4 and ipv6 RFC section 7.3.4, 7.3.5
		return &Type{Type: "string", Format: "ipv4"} // ipv4 RFC section 7.3.4
	}

	switch t.Kind() {
	case reflect.Struct:

		switch t {
		case timeType: // date-time RFC section 7.3.1
			return &Type{Type: "string", Format: "date-time"}
		case uriType: // uri RFC section 7.3.6
			return &Type{Type: "string", Format: "uri"}
		default:
			return r.reflectStruct(definitions, t)
		}

	case reflect.Map:
		valueSchema := r.reflectTypeToSchema(definitions, t.Elem())
		return &Type{
			Type:                 "object",
			AdditionalProperties: valueSchema,
		}

	case reflect.Slice, reflect.Array:
		returnType := &Type{}
		if t.Kind() == reflect.Array {
			returnType.MinItems = t.Len()
			returnType.MaxItems = returnType.MinItems
		}
		switch t {
		case byteSliceType:
			returnType.Type = "string"
			returnType.Media = &Type{BinaryEncoding: "base64"}
			return returnType
		default:
			returnType.Type = "array"
			returnType.Items = r.reflectTypeToSchema(definitions, t.Elem())
			return returnType
		}

	case reflect.Interface:
		var schema *Type
		d := r.getDiscriminator(t)
		if d != nil {
			ancestorTypes := make([]*Type, 0)
			for dKey, anc := range d.Ancestors {
				ancRef := r.reflectDiscriminatedStruct(definitions, anc, d.DiscriminatorField, dKey)
				ancRef.Title = dKey
				ancestorTypes = append(ancestorTypes, ancRef)
			}
			sort.Slice(ancestorTypes, func(i, j int) bool {
				return ancestorTypes[i].Ref < ancestorTypes[j].Ref
			})
			schema = &Type{
				OneOf:                ancestorTypes,
				AdditionalProperties: false,
			}
		} else {
			schema = &Type{
				Type:                 "object",
				AdditionalProperties: true,
			}
		}
		if t.Name() != "" {
			return addToDefinitions(definitions, t.Name(), schema)
		} else {
			return schema
		}

	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return &Type{Type: "integer"}

	case reflect.Float32, reflect.Float64:
		return &Type{Type: "number"}

	case reflect.Bool:
		return &Type{Type: "boolean"}

	case reflect.String:
		return &Type{Type: "string"}

	case reflect.Ptr:
		return r.reflectTypeToSchema(definitions, t.Elem())
	}
	panic("unsupported type " + t.String())
}

func (r *Reflector) getDiscriminator(t reflect.Type) *DiscriminatorType {
	for _, d := range r.DiscriminatedTypes {
		if d.BaseType == t {
			return &d
		}
	}
	return nil
}

func (r *Reflector) getEnum(t reflect.Type) *EnumType {
	for _, d := range r.EnumTypes {
		if d.Type == t {
			return &d
		}
	}
	return nil
}

func emptyObjectSchema(additionalProperties bool) *Type {
	if additionalProperties {
		return &Type{
			Type:                 "object",
			Properties:           map[string]*Type{},
			AdditionalProperties: true,
		}
	} else {
		return &Type{
			Type:                 "object",
			Properties:           map[string]*Type{},
			AdditionalProperties: false,
		}
	}
}

// Refects a struct to a JSON Schema type.
func (r *Reflector) reflectStruct(definitions Definitions, t reflect.Type) *Type {
	isIgnored := r.isIgnored(t)
	st := emptyObjectSchema(r.AllowAdditionalProperties || isIgnored)
	if !isIgnored {
		r.reflectStructFields(st, definitions, t)
	}

	return addToDefinitions(definitions, t.Name(), st)
}

func (r *Reflector) reflectDiscriminatedStruct(definitions Definitions, t reflect.Type, dField, dKey string) *Type {
	defName := fmt.Sprintf("%s@%s:%s", t.Name(), dField, dKey)

	isIgnored := r.isIgnored(t)
	st := emptyObjectSchema(r.AllowAdditionalProperties || isIgnored)
	st.Properties[dField] = getConstStringType(dKey)
	st.Required = append(st.Required, dField)
	if !isIgnored {
		r.reflectStructFields(st, definitions, t)
	}

	return addToDefinitions(definitions, defName, st)
}

func addToDefinitions(definitions Definitions, name string, typ *Type) *Type {
	definitions[name] = typ
	return getTypeRef(name)
}

func (r *Reflector) isIgnored(t reflect.Type) bool {
	for _, ignored := range r.IgnoredTypes {
		if reflect.TypeOf(ignored) == t {
			return true
		}
	}
	return false
}

func getTypeRef(name string) *Type {
	return &Type{
		Version: Version,
		Ref:     "#/definitions/" + name,
	}
}

func getConstStringType(value string) *Type {
	return &Type{
		Type:    "string",
		Default: value,
		Enum:    []interface{}{value},
	}
}

func (r *Reflector) reflectStructFields(st *Type, definitions Definitions, t reflect.Type) {
	if t.Kind() == reflect.Ptr {
		t = t.Elem()
	}
	if t.Kind() != reflect.Struct {
		return
	}
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		annotation, exist := r.reflectFieldName(f)

		if annotation.Inline {
			st.AllOf = append(st.AllOf, r.reflectTypeToSchema(definitions, f.Type))
			continue
		}

		// if anonymous and exported type should be processed recursively
		// current type should inherit properties of anonymous one
		if annotation.Name == "" {
			if f.Anonymous && !exist {
				r.reflectStructFields(st, definitions, f.Type)
			}
			continue
		}

		property := r.reflectTypeToSchema(definitions, f.Type)
		property.structKeywordsFromTags(f)
		st.Properties[annotation.Name] = property
		if annotation.IsRequired {
			st.Required = append(st.Required, annotation.Name)
		}
	}

	if len(st.AllOf) == 1 && len(st.OneOf) == 0 {
		st.OneOf = st.AllOf
		st.AllOf = make([]*Type, 0)
	}
}

func (t *Type) structKeywordsFromTags(f reflect.StructField) {
	t.Description = f.Tag.Get("jsonschema_description")
	tags := strings.Split(f.Tag.Get("jsonschema"), ",")
	t.genericKeywords(tags)
	switch t.Type {
	case "string":
		t.stringKeywords(tags)
	case "number":
		t.numbericKeywords(tags)
	case "integer":
		t.numbericKeywords(tags)
	case "array":
		t.arrayKeywords(tags)
	}
}

// read struct tags for generic keyworks
func (t *Type) genericKeywords(tags []string) {
	for _, tag := range tags {
		nameValue := strings.Split(tag, "=")
		if len(nameValue) == 2 {
			name, val := nameValue[0], nameValue[1]
			switch name {
			case "title":
				t.Title = val
			case "description":
				t.Description = val
			}
		}
	}
}

// read struct tags for string type keyworks
func (t *Type) stringKeywords(tags []string) {
	for _, tag := range tags {
		nameValue := strings.Split(tag, "=")
		if len(nameValue) == 2 {
			name, val := nameValue[0], nameValue[1]
			switch name {
			case "minLength":
				i, _ := strconv.Atoi(val)
				t.MinLength = i
			case "maxLength":
				i, _ := strconv.Atoi(val)
				t.MaxLength = i
			case "pattern":
				t.Pattern = val
			case "format":
				switch val {
				case "date-time", "email", "hostname", "ipv4", "ipv6", "uri":
					t.Format = val
					break
				}
			case "default":
				t.Default = val
			case "example":
				t.Examples = append(t.Examples, val)
			}
		}
	}
}

// read struct tags for numberic type keyworks
func (t *Type) numbericKeywords(tags []string) {
	for _, tag := range tags {
		nameValue := strings.Split(tag, "=")
		if len(nameValue) == 2 {
			name, val := nameValue[0], nameValue[1]
			switch name {
			case "multipleOf":
				i, _ := strconv.Atoi(val)
				t.MultipleOf = i
			case "minimum":
				i, _ := strconv.Atoi(val)
				t.Minimum = i
			case "maximum":
				i, _ := strconv.Atoi(val)
				t.Maximum = i
			case "exclusiveMaximum":
				b, _ := strconv.ParseBool(val)
				t.ExclusiveMaximum = b
			case "exclusiveMinimum":
				b, _ := strconv.ParseBool(val)
				t.ExclusiveMinimum = b
			case "default":
				i, _ := strconv.Atoi(val)
				t.Default = i
			case "example":
				if i, err := strconv.Atoi(val); err == nil {
					t.Examples = append(t.Examples, i)
				}
			}
		}
	}
}

// read struct tags for object type keyworks
// func (t *Type) objectKeywords(tags []string) {
//     for _, tag := range tags{
//         nameValue := strings.Split(tag, "=")
//         name, val := nameValue[0], nameValue[1]
//         switch name{
//             case "dependencies":
//                 t.Dependencies = val
//                 break;
//             case "patternProperties":
//                 t.PatternProperties = val
//                 break;
//         }
//     }
// }

// read struct tags for array type keyworks
func (t *Type) arrayKeywords(tags []string) {
	var defaultValues []interface{}
	for _, tag := range tags {
		nameValue := strings.Split(tag, "=")
		if len(nameValue) == 2 {
			name, val := nameValue[0], nameValue[1]
			switch name {
			case "minItems":
				i, _ := strconv.Atoi(val)
				t.MinItems = i
			case "maxItems":
				i, _ := strconv.Atoi(val)
				t.MaxItems = i
			case "uniqueItems":
				t.UniqueItems = true
			case "default":
				defaultValues = append(defaultValues, val)
			}
		}
	}
	if len(defaultValues) > 0 {
		t.Default = defaultValues
	}
}

func requiredFromJSONTags(tags []string) bool {
	if ignoredByJSONTags(tags) {
		return false
	}

	return !stringsSliceContains(tags[1:], "omitempty")
}

func stringsSliceContains(strs []string, item string) bool {
	for _, s := range strs {
		if s == item {
			return true
		}
	}
	return false
}

func requiredFromJSONSchemaTags(tags []string) bool {
	if ignoredByJSONSchemaTags(tags) {
		return false
	}
	return stringsSliceContains(tags, "required")
}

func ignoredByJSONTags(tags []string) bool {
	return tags[0] == "-"
}

func ignoredByJSONSchemaTags(tags []string) bool {
	return tags[0] == "-"
}

type fieldAnnotation struct {
	Name       string
	IsRequired bool
	Inline     bool
}

func (r *Reflector) reflectFieldName(f reflect.StructField) (fieldAnnotation, bool) {
	jsonTags, exist := f.Tag.Lookup("json")
	if !exist {
		jsonTags = f.Tag.Get("yaml")
	}

	jsonTagsList := strings.Split(jsonTags, ",")

	if ignoredByJSONTags(jsonTagsList) {
		return fieldAnnotation{}, exist
	}

	jsonSchemaTags := strings.Split(f.Tag.Get("jsonschema"), ",")
	if ignoredByJSONSchemaTags(jsonSchemaTags) {
		return fieldAnnotation{}, exist
	}

	name := f.Name
	required := requiredFromJSONTags(jsonTagsList)
	inlined := stringsSliceContains(jsonTagsList, "inline")

	if r.RequiredFromJSONSchemaTags {
		required = requiredFromJSONSchemaTags(jsonSchemaTags)
	}

	if jsonTagsList[0] != "" {
		name = jsonTagsList[0]
	}

	// field not anonymous and not export has no export name
	if !f.Anonymous && f.PkgPath != "" {
		name = ""
	}

	// field anonymous but without json tag should be inherited by current type
	if f.Anonymous && !exist {
		name = ""
	}

	return fieldAnnotation{name, required, inlined}, exist
}
