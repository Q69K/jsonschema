package jsonschema

import (
	"encoding/json"
	"io/ioutil"
	"net"
	"net/url"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type GrandfatherType struct {
	FamilyName string `json:"family_name" jsonschema:"required"`
}

type SomeBaseType struct {
	SomeBaseProperty     int `json:"some_base_property"`
	SomeBasePropertyYaml int `yaml:"some_base_property_yaml"`
	// The jsonschema required tag is nonsensical for private and ignored properties.
	// Their presence here tests that the fields *will not* be required in the output
	// schema, even if they are tagged required.
	somePrivateBaseProperty   string          `json:"i_am_private" jsonschema:"required"`
	SomeIgnoredBaseProperty   string          `json:"-" jsonschema:"required"`
	SomeSchemaIgnoredProperty string          `jsonschema:"-,required"`
	Grandfather               GrandfatherType `json:"grand"`

	SomeUntaggedBaseProperty           bool `jsonschema:"required"`
	someUnexportedUntaggedBaseProperty bool
}

type MapType map[string]interface{}

type nonExported struct {
	PublicNonExported  int
	privateNonExported int
}

type ProtoEnum int32

func (ProtoEnum) EnumDescriptor() ([]byte, []int) { return []byte(nil), []int{0} }

const (
	Unset ProtoEnum = iota
	Great
)

type AorB interface{}

type A struct {
	AorB
	NameA string `json:"name_a"`
}

type B struct {
	AorB
	NameB string `json:"name_b"`
}

type TestUser struct {
	SomeBaseType
	nonExported
	MapType

	ID      int                    `json:"id" jsonschema:"required"`
	Name    string                 `json:"name" jsonschema:"required,minLength=1,maxLength=20,pattern=.*,description=this is a property,title=the name,example=joe,example=lucy,default=alex"`
	Friends []int                  `json:"friends,omitempty" jsonschema_description:"list of IDs, omitted when empty"`
	Tags    map[string]interface{} `json:"tags,omitempty"`

	TestFlag       bool
	IgnoredCounter int `json:"-"`

	// Tests for RFC draft-wright-json-schema-validation-00, section 7.3
	BirthDate time.Time `json:"birth_date,omitempty"`
	Website   url.URL   `json:"website,omitempty"`
	IPAddress net.IP    `json:"network_address,omitempty"`

	// Tests for RFC draft-wright-json-schema-hyperschema-00, section 4
	Photo []byte `json:"photo,omitempty" jsonschema:"required"`

	// Tests for jsonpb enum support
	Feeling ProtoEnum `json:"feeling,omitempty"`
	Age     int       `json:"age" jsonschema:"minimum=18,maximum=120,exclusiveMaximum=true,exclusiveMinimum=true"`
	Email   string    `json:"email" jsonschema:"format=email"`

	AorB AorB `json:"a_or_b"`
}

type CustomTime time.Time

type CustomTypeField struct {
	CreatedAt CustomTime
}

type MyEnum string

const (
	MyEnumValueA MyEnum = "a"
	MyEnumValueB MyEnum = "b"
)

type TestStruct3 struct {
	MyEnum MyEnum
}

type InlinedStruct struct {
	A string `json:"a"`
}

type StructWithInline struct {
	InlinedStruct `json:",inline"`
}

type CaseX_Interface interface{}

type CaseX_StructA struct {
	A string `json:"a"`
}

type CaseX_StructB struct {
	B string `json:"b"`
}

func registerCaseXHierarchy(r *Reflector) *Reflector {
	_ = r.RegisterDiscriminatorType(
		reflect.TypeOf((*CaseX_Interface)(nil)).Elem(),
		"typ", map[string]reflect.Type{
			"typA": reflect.TypeOf(CaseX_StructA{}),
			"typB": reflect.TypeOf(CaseX_StructB{}),
		},
	)
	return r
}

type CaseX_WithInlinedDiscriminatedType struct {
	CaseX_Interface `json:",inline"`
}

func TestSchemaGeneration(t *testing.T) {
	tests := []struct {
		typ       interface{}
		reflector *Reflector
		fixture   string
	}{
		//{&TestStruct3{}, &Reflector{
		//	EnumTypes: []EnumType{
		//		{
		//			Type:   reflect.TypeOf((*MyEnum)(nil)).Elem(),
		//			Values: []interface{}{MyEnumValueA, MyEnumValueB},
		//		},
		//	},
		//}, "fixtures/struct_enums.json"},
		//{&TestUser{}, &Reflector{}, "fixtures/defaults.json"},
		//{&TestUser{}, &Reflector{AllowAdditionalProperties: true}, "fixtures/allow_additional_props.json"},
		//{&TestUser{}, &Reflector{RequiredFromJSONSchemaTags: true}, "fixtures/required_from_jsontags.json"},
		//{&TestUser{}, &Reflector{ExpandedStruct: true}, "fixtures/defaults_expanded_toplevel.json"},
		//{&TestUser{}, &Reflector{IgnoredTypes: []interface{}{GrandfatherType{}, A{}, B{}}}, "fixtures/ignore_type.json"},
		//{&StructWithInline{InlinedStruct{A:"a value"}}, &Reflector{}, "fixtures/inlined_struct.json"},
		{&CaseX_WithInlinedDiscriminatedType{}, registerCaseXHierarchy(&Reflector{}), "fixtures/caseX_inlined_hierarchy.json"},
		//{&CustomTypeField{}, &Reflector{
		//	TypeMapper: func(i reflect.Type) *Type {
		//		if i == reflect.TypeOf(CustomTime{}) {
		//			return &Type{
		//				Type:   "string",
		//				Format: "date-time",
		//			}
		//		}
		//		return nil
		//	},
		//}, "fixtures/custom_type.json"},
	}

	for _, tt := range tests {
		name := strings.TrimSuffix(filepath.Base(tt.fixture), ".json")
		t.Run(name, func(t *testing.T) {
			f, err := ioutil.ReadFile(tt.fixture)
			require.NoError(t, err)

			err = tt.reflector.RegisterDiscriminatorType(
				reflect.TypeOf((*AorB)(nil)).Elem(),
				"type", map[string]reflect.Type{
					"a": reflect.TypeOf(A{}),
					"b": reflect.TypeOf(B{}),
				},
			)
			require.NoError(t, err)

			actualSchema := tt.reflector.Reflect(tt.typ)
			expectedSchema := &Schema{}

			err = json.Unmarshal(f, expectedSchema)
			require.NoError(t, err)

			expectedJSON, _ := json.MarshalIndent(expectedSchema, "", "  ")
			actualJSON, _ := json.MarshalIndent(actualSchema, "", "  ")
			require.Equal(t, string(expectedJSON), string(actualJSON))
		})
	}
}
