package printer

import (
	"bytes"
	"errors"
	"io/ioutil"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/google/go-jsonnet/ast"
	"github.com/google/go-jsonnet/parser"
	"github.com/ksonnet/ksonnet-lib/ksonnet-gen/astext"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFprintf(t *testing.T) {
	cases := []struct {
		name  string
		isErr bool
	}{
		{name: "null"},
		{name: "object"},
		{name: "object_with_hidden_field"},
		{name: "apply_brace"},
		{name: "object_mixin"},
		{name: "object_mixin_with_string_index"},
		{name: "object_with_nested_object"},
		{name: "local"},
		{name: "multi_line_comments"},
		{name: "boolean"},
		{name: "literal"},
		{name: "literal_with_newline"},
		{name: "literal_with_single_quote"},
		{name: "object_field_with_comment"},
		{name: "object_field_trailing_comma"},
		{name: "function_with_no_args"},
		{name: "function_with_args"},
		{name: "function_with_optional_args_ast"},
		{name: "function_with_optional_args_astext"},
		{name: "local_function_with_args"},
		{name: "conditional"},
		{name: "conditional_no_false"},
		{name: "index"},
		{name: "index_with_index"},
		{name: "index_quote_name"},
		{name: "index_quote_name_2"},
		{name: "array"},
		{name: "self_apply"},
		{name: "apply_with_multiple_arguments"},
		{name: "declarations"},
		{name: "chained_apply"},
		{name: "apply_with_index"},
		{name: "object_field_with_local"},
		{name: "local_with_function"},
		{name: "apply_with_number"},
		{name: "local_with_multiline_function"},
		{name: "field_with_string_key"},
		{name: "object_comp"},
		{name: "array_comp"},
		{name: "importstr"},
		{name: "function"},
		{name: "super_index"},
		{name: "block_string"},
		{name: "dollar"},
		{name: "nil_node"},
		{name: "trimmed_whitespace_in_tests"},
		{name: "field_id_keywords"},

		// errors
		{name: "unknown_node", isErr: true},
		{name: "invalid_literal_string", isErr: true},
		{name: "invalid_of_kind", isErr: true},
		{name: "invalid_of_hide", isErr: true},
		{name: "index_no_index_or_id", isErr: true},
		{name: "index_invalid_index", isErr: true},
		{name: "index_invalid_literal_string", isErr: true},
		{name: "null_index", isErr: true},
		{name: "function_with_invalid_optional_arg", isErr: true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var buf bytes.Buffer

			node, ok := fprintfCases[tc.name]
			if !ok {
				t.Fatalf("test case %q does not exist", tc.name)
			}

			err := Fprint(&buf, node)
			if tc.isErr {
				require.Error(t, err)
				return
			}

			require.NoError(t, err)

			testDataFile := filepath.Join("testdata", tc.name)
			testData, err := ioutil.ReadFile(testDataFile)
			require.NoError(t, err, "unable to read test data")

			got := strings.TrimSpace(buf.String())
			expected := strings.TrimSpace(string(testData))
			if got != expected {
				t.Fatalf("Fprint\ngot      = %s\nexpected = %s",
					got, expected)
			}
		})
	}
}

func Test_with_upstream_golden(t *testing.T) {
	dataPath := filepath.FromSlash("testdata/upstream")
	fis, err := ioutil.ReadDir(dataPath)
	require.NoError(t, err)

	for _, fi := range fis {
		t.Run(fi.Name(), func(t *testing.T) {
			defer func() {
				if r := recover(); r != nil {
					t.Logf("recover from panic: %v", r)
				}
			}()

			if fi.IsDir() {
				return
			}

			filePath := filepath.Join(dataPath, fi.Name())
			b, err := ioutil.ReadFile(filePath)
			require.NoError(t, err)

			tokens, err := parser.Lex(fi.Name(), string(b))
			require.NoError(t, err)

			node, err := parser.Parse(tokens)
			require.NoError(t, err)

			var buf bytes.Buffer

			err = Fprint(&buf, node)
			require.NoError(t, err)

			got := strings.TrimSpace(buf.String())
			expected := strings.TrimSpace(string(b))
			if got != expected {
				// dmp := diffmatchpatch.New()
				// diffs := dmp.DiffMain(expected, got, false)
				// t.Fatalf(dmp.DiffPrettyText(diffs))
				t.Fatalf("Fprint from upstream\ngot:\n%s\n===\nexpected:\n%s\n",
					strconv.Quote(got), strconv.Quote(expected))
			}
		})
	}

}

var (
	id1 = ast.Identifier("foo")
	id2 = ast.Identifier("bar")

	fprintfCases = map[string]ast.Node{
		"null": &ast.LiteralNull{},
		"object": &ast.Object{
			Fields: ast.ObjectFields{},
		},
		"object_with_hidden_field": &ast.Object{
			Fields: ast.ObjectFields{
				{
					Kind:  ast.ObjectFieldID,
					Id:    newIdentifier("foo"),
					Expr2: &ast.Object{},
				},
				{
					Kind:  ast.ObjectFieldID,
					Id:    newIdentifier("bar"),
					Expr2: &ast.Object{},
				},
			},
		},
		"apply_brace": &ast.ApplyBrace{
			Left:  &ast.Var{Id: *newIdentifier("params")},
			Right: &astext.Object{},
		},
		"index": &ast.Object{
			Fields: ast.ObjectFields{
				{
					Kind: ast.ObjectFieldID,
					Id:   &id1,
					Expr2: &ast.Index{
						Id: newIdentifier("baz"),
						Target: &ast.Index{
							Id: newIdentifier("bar"),
							Target: &ast.Var{
								Id: *newIdentifier("foo"),
							},
						},
					},
				},
			},
		},
		"index_with_index": &ast.Object{
			Fields: ast.ObjectFields{
				{
					Kind: ast.ObjectFieldID,
					Id:   &id1,
					Expr2: &ast.Index{
						Id: newIdentifier("baz"),
						Target: &ast.Index{
							Index: &ast.LiteralString{
								Value: "bar",
								Kind:  ast.StringDouble,
							},
							Target: &ast.Var{
								Id: *newIdentifier("foo"),
							},
						},
					},
				},
			},
		},
		"index_quote_name": &ast.Object{
			Fields: ast.ObjectFields{
				{
					Kind: ast.ObjectFieldID,
					Id:   &id1,
					Expr2: &ast.Index{
						Id: newIdentifier("baz-dashed"),
						Target: &ast.Index{
							Id: newIdentifier("bar-dashed"),
							Target: &ast.Var{
								Id: *newIdentifier("foo"),
							},
						},
					},
				},
			},
		},
		"index_quote_name_2": &ast.Index{
			Target: &ast.Index{
				Target: &ast.Apply{
					Target: &ast.Index{
						Target: &ast.Var{
							Id: *newIdentifier("std"),
						},
						Id: newIdentifier("extVar"),
					},
					Arguments: ast.Arguments{
						Positional: ast.Nodes{
							&ast.LiteralString{
								Value: "__ksonnet/params",
								Kind:  ast.StringDouble,
							},
						},
					},
				},
				Id: newIdentifier("components"),
			},
			Index: &ast.LiteralString{
				Value: "my-service",
				Kind:  ast.StringDouble,
			},
		},
		"object_mixin": &ast.Object{
			Fields: ast.ObjectFields{
				{
					Kind:       ast.ObjectFieldID,
					Id:         &id1,
					Expr2:      &ast.Object{},
					SuperSugar: true,
				},
			},
		},
		"object_mixin_with_string_index": &ast.Object{
			Fields: ast.ObjectFields{
				{
					Kind: 3,
					Hide: ast.ObjectFieldInherit,
					Expr1: &ast.LiteralString{
						Kind:  ast.StringDouble,
						Value: "id",
					},
					Expr2:      &ast.Object{},
					SuperSugar: true,
				},
			},
		},
		"object_with_nested_object": &ast.Object{
			Fields: ast.ObjectFields{
				{
					Kind: ast.ObjectFieldID,
					Id:   &id1,
					Expr2: &ast.Object{
						Fields: ast.ObjectFields{
							{
								Kind:  ast.ObjectFieldID,
								Id:    &id2,
								Expr2: &ast.Object{},
							},
						},
					},
				},
			},
		},
		"local": &ast.Object{
			Fields: ast.ObjectFields{
				{
					Kind:  ast.ObjectLocal,
					Hide:  ast.ObjectFieldVisible,
					Id:    &id2,
					Expr2: &ast.Object{},
				},
			},
		},
		"multi_line_comments": &astext.Object{
			Fields: []astext.ObjectField{
				{
					ObjectField: ast.ObjectField{
						Kind:  ast.ObjectLocal,
						Hide:  ast.ObjectFieldVisible,
						Id:    &id2,
						Expr2: &ast.Object{},
					},
					Comment: &astext.Comment{
						Text: "line 1\n\nline 3\nline 4",
					},
				},
			},
		},
		"boolean": &ast.Object{
			Fields: ast.ObjectFields{
				{
					Kind: ast.ObjectFieldID,
					Hide: ast.ObjectFieldInherit,
					Id:   newIdentifier("isTrue"),
					Expr2: &ast.LiteralBoolean{
						Value: true,
					},
				},
				{
					Kind: ast.ObjectFieldID,
					Hide: ast.ObjectFieldInherit,
					Id:   newIdentifier("isFalse"),
					Expr2: &ast.LiteralBoolean{
						Value: false,
					},
				},
			},
		},
		"literal": &ast.Object{
			Fields: ast.ObjectFields{
				{
					Kind: ast.ObjectFieldID,
					Hide: ast.ObjectFieldInherit,
					Id:   &id1,
					Expr2: &ast.LiteralString{
						Value: "value",
						Kind:  ast.StringDouble,
					},
				},
			},
		},
		"literal_with_newline": &ast.Object{
			Fields: ast.ObjectFields{
				{
					Kind: ast.ObjectFieldID,
					Hide: ast.ObjectFieldInherit,
					Id:   &id1,
					Expr2: &ast.LiteralString{
						Value: "value1\nvalue2",
						Kind:  ast.StringDouble,
					},
				},
			},
		},
		"literal_with_single_quote": &ast.Object{
			Fields: ast.ObjectFields{
				{
					Kind: ast.ObjectFieldID,
					Hide: ast.ObjectFieldInherit,
					Id:   &id1,
					Expr2: &ast.LiteralString{
						Value: "value1",
						Kind:  ast.StringSingle,
					},
				},
			},
		},
		"object_field_with_comment": &astext.Object{
			Object: ast.Object{TrailingComma: false},
			Fields: []astext.ObjectField{
				{
					ObjectField: ast.ObjectField{
						Kind: ast.ObjectFieldID,
						Hide: ast.ObjectFieldInherit,
						Id:   &id1,
						Expr2: &ast.LiteralString{
							Value: "value",
							Kind:  ast.StringDouble,
						},
					},
					Comment: &astext.Comment{
						Text: "a comment",
					},
				},
			},
		},
		"object_field_trailing_comma": &astext.Object{
			Object: ast.Object{TrailingComma: true},
			Fields: []astext.ObjectField{
				{
					ObjectField: ast.ObjectField{
						Kind: ast.ObjectFieldID,
						Hide: ast.ObjectFieldInherit,
						Id:   &id1,
						Expr2: &ast.LiteralString{
							Value: "value",
							Kind:  ast.StringDouble,
						},
					},
					Comment: &astext.Comment{
						Text: "a comment",
					},
				},
			},
		},
		"function_with_no_args": &ast.Object{
			Fields: ast.ObjectFields{
				{
					Kind: ast.ObjectFieldID,
					Id:   &id1,
					Expr2: &ast.Binary{
						Left:  newLiteralNumber("1"),
						Right: newLiteralNumber("2"),
						Op:    ast.BopPlus,
					},
					Method: &ast.Function{
						Parameters: ast.Parameters{},
					},
				},
			},
		},
		"function_with_args": &ast.Object{
			Fields: ast.ObjectFields{
				{
					Kind: ast.ObjectFieldID,
					Id:   &id1,
					Expr2: &ast.Binary{
						Left:  &ast.Var{Id: *newIdentifier("myVar")},
						Right: newLiteralNumber("2"),
						Op:    ast.BopPlus,
					},
					Method: &ast.Function{
						Parameters: ast.Parameters{
							Required: ast.Identifiers{
								*newIdentifier("one"),
								*newIdentifier("two"),
							},
						},
					},
				},
			},
		},
		"function_with_optional_args_astext": &ast.Object{
			Fields: ast.ObjectFields{
				{
					Kind: ast.ObjectFieldID,
					Id:   newIdentifier("alpha"),
					Expr2: &ast.Binary{
						Left:  &ast.Var{Id: *newIdentifier("myVar")},
						Right: newLiteralNumber("2"),
						Op:    ast.BopPlus,
					},
					Method: &ast.Function{
						Parameters: ast.Parameters{
							Required: ast.Identifiers{
								*newIdentifier("one"),
								*newIdentifier("two"),
							},
							Optional: []ast.NamedParameter{
								{
									Name:       *newIdentifier("opt1"),
									DefaultArg: newLiteralNumber("1"),
								},
							},
						},
					},
				},
				{
					Kind: ast.ObjectFieldID,
					Id:   newIdentifier("beta"),
					Expr2: &ast.Binary{
						Left:  &ast.Var{Id: *newIdentifier("myVar")},
						Right: newLiteralNumber("2"),
						Op:    ast.BopPlus,
					},
					Method: &ast.Function{
						Parameters: ast.Parameters{
							Required: ast.Identifiers{
								*newIdentifier("one"),
								*newIdentifier("two"),
							},
							Optional: []ast.NamedParameter{
								{
									Name: *newIdentifier("opt1"),
									DefaultArg: &astext.Object{
										Fields: []astext.ObjectField{
											{
												ObjectField: ast.ObjectField{
													Kind:  ast.ObjectFieldID,
													Hide:  ast.ObjectFieldInherit,
													Id:    newIdentifier("foo"),
													Expr2: newLiteralNumber("1"),
												},
											},
										},
									},
								},
							},
						},
					},
				},
			},
		},
		"function_with_optional_args_ast": &ast.Object{
			Fields: ast.ObjectFields{
				{
					Kind: ast.ObjectFieldID,
					Id:   newIdentifier("alpha"),
					Expr2: &ast.Binary{
						Left:  &ast.Var{Id: *newIdentifier("myVar")},
						Right: newLiteralNumber("2"),
						Op:    ast.BopPlus,
					},
					Method: &ast.Function{
						Parameters: ast.Parameters{
							Required: ast.Identifiers{
								*newIdentifier("one"),
								*newIdentifier("two"),
							},
							Optional: []ast.NamedParameter{
								{
									Name:       *newIdentifier("opt1"),
									DefaultArg: newLiteralNumber("1"),
								},
							},
						},
					},
				},
				{
					Kind: ast.ObjectFieldID,
					Id:   newIdentifier("beta"),
					Expr2: &ast.Binary{
						Left:  &ast.Var{Id: *newIdentifier("myVar")},
						Right: newLiteralNumber("2"),
						Op:    ast.BopPlus,
					},
					Method: &ast.Function{
						Parameters: ast.Parameters{
							Required: ast.Identifiers{
								*newIdentifier("one"),
								*newIdentifier("two"),
							},
							Optional: []ast.NamedParameter{
								{
									Name: *newIdentifier("opt1"),
									DefaultArg: &ast.Object{
										Fields: []ast.ObjectField{
											{
												Kind:  ast.ObjectFieldID,
												Hide:  ast.ObjectFieldInherit,
												Id:    newIdentifier("foo"),
												Expr2: newLiteralNumber("1"),
											},
										},
									},
								},
							},
						},
					},
				},
			},
		},
		"local_function_with_args": &ast.Object{
			Fields: ast.ObjectFields{
				{
					Kind: ast.ObjectLocal,
					Id:   newIdentifier("foo"),
					Expr2: &ast.Binary{
						Left:  &ast.Var{Id: *newIdentifier("myVar")},
						Right: newLiteralNumber("2"),
						Op:    ast.BopPlus,
					},
					Method: &ast.Function{
						Parameters: ast.Parameters{
							Required: ast.Identifiers{
								*newIdentifier("one"),
								*newIdentifier("two"),
							},
						},
					},
				},
			},
		},
		"conditional": &ast.Conditional{
			Cond: &ast.Binary{
				Left: &ast.Apply{
					Target: &ast.Index{
						Id: newIdentifier("type"),
						Target: &ast.Var{
							Id: *newIdentifier("std"),
						},
					},
					Arguments: ast.Arguments{
						Positional: ast.Nodes{
							&ast.Var{Id: *newIdentifier("foo")},
						},
					},
				},
				Right: &ast.LiteralString{
					Value: "array",
					Kind:  ast.StringDouble,
				},
				Op: ast.BopManifestEqual,
			},
			BranchTrue: &astext.Object{
				Oneline: true,
				Fields: astext.ObjectFields{
					{
						ObjectField: ast.ObjectField{
							Id:    newIdentifier("foo"),
							Kind:  ast.ObjectFieldID,
							Hide:  ast.ObjectFieldInherit,
							Expr2: &ast.Var{Id: *newIdentifier("foo")},
						},
					},
				},
			},
			BranchFalse: &astext.Object{
				Oneline: true,
				Fields: astext.ObjectFields{
					{
						ObjectField: ast.ObjectField{
							Id:   newIdentifier("foo"),
							Kind: ast.ObjectFieldID,
							Hide: ast.ObjectFieldInherit,
							Expr2: &ast.Array{
								Elements: ast.Nodes{
									&ast.Var{Id: *newIdentifier("foo")},
								},
							},
						},
					},
				},
			},
		},
		"conditional_no_false": &ast.Conditional{
			Cond: &ast.Binary{
				Left: &ast.Apply{
					Target: &ast.Index{
						Id: newIdentifier("type"),
						Target: &ast.Var{
							Id: *newIdentifier("std"),
						},
					},
					Arguments: ast.Arguments{
						Positional: ast.Nodes{
							&ast.Var{Id: *newIdentifier("foo")},
						},
					},
				},
				Right: &ast.LiteralString{
					Value: "array",
					Kind:  ast.StringDouble,
				},
				Op: ast.BopManifestEqual,
			},
			BranchTrue: &astext.Object{
				Oneline: true,
				Fields: astext.ObjectFields{
					{
						ObjectField: ast.ObjectField{
							Id:    newIdentifier("foo"),
							Kind:  ast.ObjectFieldID,
							Hide:  ast.ObjectFieldInherit,
							Expr2: &ast.Var{Id: *newIdentifier("foo")},
						},
					},
				},
			},
		},
		"array": &ast.Array{
			Elements: ast.Nodes{
				&ast.Var{Id: *newIdentifier("foo")},
				&ast.Self{},
				&ast.LiteralString{
					Value: "string",
				},
			},
		},
		"self_apply": &ast.Apply{Target: &ast.Self{}},
		"apply_with_multiple_arguments": &ast.Apply{
			Target: &ast.Self{},
			Arguments: ast.Arguments{
				Positional: ast.Nodes{
					&ast.Var{Id: *newIdentifier("a")},
					&ast.Var{Id: *newIdentifier("b")},
				},
			},
		},
		"declarations": &ast.Local{
			Binds: ast.LocalBinds{
				ast.LocalBind{
					Variable: *newIdentifier("a"),
					Body: &ast.Import{
						File: &ast.LiteralString{
							Kind:  ast.StringDouble,
							Value: "a",
						},
					},
				},
			},
			Body: &ast.Local{
				Binds: ast.LocalBinds{
					ast.LocalBind{
						Variable: *newIdentifier("b"),
						Body: &ast.LiteralString{
							Kind:  ast.StringDouble,
							Value: "b",
						},
					},
				},
				Body: &ast.Local{
					Binds: ast.LocalBinds{
						ast.LocalBind{
							Variable: *newIdentifier("c"),
							Body: &ast.Apply{
								Target: &ast.Index{
									Id: newIdentifier("new"),
									Target: &ast.Var{
										Id: *newIdentifier("deployment"),
									},
								},
							},
						},
					},
					Body: &ast.Object{},
				},
			},
		},
		"chained_apply": &ast.Apply{
			Arguments: ast.Arguments{
				Positional: ast.Nodes{
					&ast.Var{Id: *newIdentifier("bar")},
				},
			},
			Target: &ast.Index{
				Id: newIdentifier("withBar"),
				Target: &ast.Apply{
					Arguments: ast.Arguments{
						Positional: ast.Nodes{
							&ast.Var{Id: *newIdentifier("foo")},
						},
					},
					Target: &ast.Index{
						Id: newIdentifier("withFoo"),
						Target: &ast.Var{
							Id: *newIdentifier("di"),
						},
					},
				},
			},
		},
		"apply_with_index": &ast.Apply{
			Target: &ast.Index{
				Id: newIdentifier("charlie"),
				Target: &ast.Index{
					Id: newIdentifier("beta"),
					Target: &ast.Var{
						Id: *newIdentifier("alpha"),
					},
				},
			},
			Arguments: ast.Arguments{
				Positional: ast.Nodes{
					&ast.LiteralString{
						Kind:  ast.StringDouble,
						Value: "arg1",
					},
				},
			},
		},
		"object_field_with_local": &ast.Object{
			Fields: ast.ObjectFields{
				{
					Kind: ast.ObjectFieldID,
					Id:   newIdentifier("fn"),
					Expr2: &ast.Local{
						Binds: ast.LocalBinds{
							{
								Variable: *newIdentifier("foo"),
								Body: &ast.LiteralString{
									Value: "a",
									Kind:  ast.StringDouble,
								},
							},
						},
						Body: &ast.Object{},
					},
					Method: &ast.Function{
						Parameters: ast.Parameters{},
					},
				},
			},
		},
		"local_with_function": &ast.Local{
			Binds: ast.LocalBinds{
				{
					Variable: *newIdentifier("foo"),
					Body: &ast.Function{
						Body: &ast.LiteralString{
							Value: "a",
							Kind:  ast.StringDouble,
						},
					},
				},
			},
			Body: &ast.Object{},
		},
		"apply_with_number": &ast.Apply{Target: newLiteralNumber("1")},
		"local_with_multiline_function": &ast.Local{
			Binds: ast.LocalBinds{
				{
					Variable: *newIdentifier("foo"),
					Body: &ast.Function{
						Body: &ast.Local{
							Binds: ast.LocalBinds{
								{
									Variable: *newIdentifier("a"),
									Body: &astext.Object{
										Oneline: true,
										Fields: astext.ObjectFields{
											{
												ObjectField: ast.ObjectField{
													Id:   newIdentifier("a"),
													Kind: ast.ObjectFieldID,
													Hide: ast.ObjectFieldInherit,
													Expr2: &ast.LiteralString{
														Value: "a",
														Kind:  ast.StringDouble,
													},
												},
											},
										},
									},
								},
							},
							Body: &ast.Local{
								Binds: ast.LocalBinds{
									{
										Variable: *newIdentifier("b"),
										Body: &astext.Object{
											Oneline: true,
											Fields: astext.ObjectFields{
												{
													ObjectField: ast.ObjectField{
														Id:   newIdentifier("b"),
														Kind: ast.ObjectFieldID,
														Hide: ast.ObjectFieldInherit,
														Expr2: &ast.LiteralString{
															Value: "b",
															Kind:  ast.StringDouble,
														},
													},
												},
											},
										},
									},
								},
								Body: &ast.Binary{
									Left:  &ast.Var{Id: *newIdentifier("a")},
									Right: &ast.Var{Id: *newIdentifier("b")},
									Op:    ast.BopPlus,
								},
							},
						},
					},
				},
			},
			Body: &ast.Object{},
		},
		"field_with_string_key": &astext.Object{
			Fields: astext.ObjectFields{
				{
					ObjectField: ast.ObjectField{
						Kind: ast.ObjectFieldID,
						Expr1: &ast.LiteralString{
							Kind:  ast.StringDouble,
							Value: "key",
						},
						Expr2: &ast.Var{
							Id: *newIdentifier("value"),
						},
					},
				},
			},
		},
		"object_comp": &astext.Object{
			Fields: astext.ObjectFields{
				{
					ObjectField: ast.ObjectField{
						Kind: ast.ObjectFieldID,
						Hide: ast.ObjectFieldInherit,
						Id:   newIdentifier("field"),
						Expr2: &ast.ObjectComp{
							Fields: ast.ObjectFields{
								{
									Kind: ast.ObjectFieldExpr,
									Hide: ast.ObjectFieldInherit,
									Expr1: &ast.Var{
										Id: *newIdentifier("x"),
									},
									Expr2: &ast.Binary{
										Left: &ast.Index{
											Target: &ast.Index{
												Target: &ast.Var{
													Id: *newIdentifier("envParams"),
												},
												Id: newIdentifier("components"),
											},
											Index: &ast.Var{
												Id: *newIdentifier("x"),
											},
										},
										Op: ast.BopPlus,
										Right: &ast.Var{
											Id: *newIdentifier("globals"),
										},
									},
								},
							},
							Spec: ast.ForSpec{
								VarName: *newIdentifier("x"),
								Expr: &ast.Apply{
									Target: &ast.Index{
										Target: &ast.Var{
											Id: *newIdentifier("std"),
										},
										Id: newIdentifier("objectFields"),
									},
									Arguments: ast.Arguments{
										Positional: ast.Nodes{
											&ast.Index{
												Target: &ast.Var{
													Id: *newIdentifier("envParams"),
												},
												Id: newIdentifier("components"),
											},
										},
									},
								},
							},
						},
					},
				},
			},
		},
		"importstr": &ast.ImportStr{
			File: &ast.LiteralString{
				Value: "file.txt",
				Kind:  ast.StringDouble,
			},
		},
		"array_comp": &ast.ArrayComp{
			Body: &ast.Object{
				Fields: ast.ObjectFields{
					{
						Id:    newIdentifier("kind"),
						Kind:  ast.ObjectFieldID,
						Hide:  ast.ObjectFieldInherit,
						Expr2: &ast.Var{Id: *newIdentifier("kind")},
					},
					{
						Id:   newIdentifier("qty"),
						Kind: ast.ObjectFieldID,
						Hide: ast.ObjectFieldInherit,
						Expr2: &ast.Binary{
							Left:  &ast.LiteralNumber{Value: float64(4), OriginalString: "4"},
							Right: &ast.LiteralNumber{Value: float64(3), OriginalString: "3"},
							Op:    ast.BopDiv,
						},
					},
				},
			},
			Spec: ast.ForSpec{
				VarName: *newIdentifier("kind"),
				Expr: &ast.Array{
					Elements: ast.Nodes{
						&ast.LiteralString{Value: "Honey Syrup", Kind: ast.StringSingle},
						&ast.LiteralString{Value: "Lemon Juice", Kind: ast.StringSingle},
						&ast.LiteralString{Value: "Farmers Gin", Kind: ast.StringSingle},
					},
				},
			},
		},
		"function": &ast.Function{
			Body: &astext.Object{Oneline: true},
		},
		"super_index": &ast.SuperIndex{
			Id: newIdentifier("metadata"),
		},
		"block_string": &ast.LiteralString{
			Kind:  ast.StringBlock,
			Value: "text",
		},
		"dollar":   &ast.Dollar{},
		"nil_node": nil,

		// errors
		"unknown_node":           &noopNode{},
		"invalid_literal_string": &ast.LiteralString{Kind: 99},
		"invalid_of_kind": &ast.Object{
			Fields: ast.ObjectFields{{Kind: 99}},
		},
		"invalid_of_hide": &ast.Object{
			Fields: ast.ObjectFields{{Hide: 99}},
		},
		"index_no_index_or_id":         &ast.Index{},
		"index_invalid_index":          &ast.Index{Index: &noopNode{}},
		"index_invalid_literal_string": &ast.Index{Index: (*ast.LiteralString)(nil)},
		"null_index":                   (*ast.Index)(nil),
		"function_with_invalid_optional_arg": &ast.Function{
			Parameters: ast.Parameters{
				Optional: []ast.NamedParameter{
					{
						DefaultArg: &unprintableNode{},
					},
				},
			},
		},
		"trimmed_whitespace_in_tests": &ast.Object{
			Fields: ast.ObjectFields{},
		},
		"field_id_keywords": &ast.Object{
			Fields: ast.ObjectFields{
				ast.ObjectField{
					Kind: ast.ObjectFieldStr,
					Hide: ast.ObjectFieldInherit,
					Expr1: &ast.LiteralString{
						Value: "error",
						Kind:  ast.StringSingle,
					},
					Expr2: &ast.LiteralString{
						Value: "value",
						Kind:  ast.StringSingle,
					},
				},
				ast.ObjectField{
					Kind: ast.ObjectFieldStr,
					Hide: ast.ObjectFieldInherit,
					Expr1: &ast.LiteralString{
						Value: "local",
						Kind:  ast.StringSingle,
					},
					Expr2: &ast.LiteralString{
						Value: "value",
						Kind:  ast.StringSingle,
					},
				},
				ast.ObjectField{
					Kind: ast.ObjectFieldStr,
					Hide: ast.ObjectFieldInherit,
					Expr1: &ast.LiteralString{
						Value: "unquoteme",
						Kind:  ast.StringSingle,
					},
					Expr2: &ast.LiteralString{
						Value: "value",
						Kind:  ast.StringSingle,
					},
				},
				ast.ObjectField{
					Kind: ast.ObjectFieldStr,
					Hide: ast.ObjectFieldInherit,
					Expr1: &ast.LiteralString{
						Value: "but-not-me",
						Kind:  ast.StringSingle,
					},
					Expr2: &ast.LiteralString{
						Value: "value",
						Kind:  ast.StringSingle,
					},
				},
				ast.ObjectField{
					Kind: ast.ObjectFieldStr,
					Hide: ast.ObjectFieldInherit,
					Expr1: &ast.LiteralString{
						Value: "nor:me",
						Kind:  ast.StringSingle,
					},
					Expr2: &ast.LiteralString{
						Value: "value",
						Kind:  ast.StringSingle,
					},
				},
			},
		},
	}
)

func Test_printer_indent(t *testing.T) {
	cases := []struct {
		name     string
		level    int
		expected string
		output   []byte
		mode     IndentMode
	}{
		{
			name:  "space: empty",
			level: 0, expected: "", output: make([]byte, 0)},
		{
			name:  "space: not at eol",
			level: 0, expected: "word", output: []byte("word")},
		{
			name:  "space: at eol",
			level: 0, expected: "word\n", output: []byte("word\n")},
		{
			name:  "space: indent level 1: empty",
			level: 1, expected: "", output: make([]byte, 0)},
		{
			name:  "space: indent level 1: not at eol",
			level: 1, expected: "word", output: []byte("word")},
		{
			name:  "space: indent level 1: at eol",
			level: 1, expected: "word\n  ", output: []byte("word\n")},
		{
			name:  "tab: empty",
			level: 0, expected: "", output: make([]byte, 0), mode: IndentModeTab},
		{
			name:  "tab: not at eol",
			level: 0, expected: "word", output: []byte("word"), mode: IndentModeTab},
		{
			name:  "tab: at eol",
			level: 0, expected: "word\n", output: []byte("word\n"), mode: IndentModeTab},
		{
			name:  "tab: indent level 1: empty",
			level: 1, expected: "", output: make([]byte, 0), mode: IndentModeTab},
		{
			name:  "tab: indent level 1: not at eol",
			level: 1, expected: "word", output: []byte("word"), mode: IndentModeTab},
		{
			name:  "tab: indent level 1: at eol",
			level: 1, expected: "word\n\t", output: []byte("word\n"), mode: IndentModeTab},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := printer{cfg: Config{IndentMode: tc.mode, IndentSize: 2}}
			p.indentLevel = tc.level

			for _, b := range tc.output {
				p.writeByte(b, 1)
			}

			expected := tc.expected
			if got := string(p.output); got != expected {
				t.Fatalf("Fprint\ngot      = %s\nexpected = %s",
					strconv.Quote(got), strconv.Quote(expected))
			}
		})
	}
}

type unprintableNode struct {
	ast.Object
}

func Test_printer_indent_empty(t *testing.T) {
	p := printer{cfg: DefaultConfig}
	p.indentLevel = 1
	p.indent()
	if len(p.output) != 0 {
		t.Errorf("indent() with empty output should not change output")
	}
}

func Test_printer_err(t *testing.T) {
	p := printer{cfg: DefaultConfig}
	p.err = errors.New("error")

	n := &ast.Object{}
	p.print(n)

	if len(p.output) != 0 {
		t.Errorf("print() in error state should not add any output")
	}
}

func Test_handleObjectField_unknown_object(t *testing.T) {
	p := printer{cfg: DefaultConfig}
	p.handleObjectField(nil)
	require.Error(t, p.err)
}

func Test_quoteString(t *testing.T) {
	tests := []struct {
		s         string
		expected  string
		useSingle bool
	}{
		{
			s:         "\\tFoo\tBar",
			expected:  `'\tFoo\tBar'`,
			useSingle: true,
		},
		{
			s:         "\\tFoo\tBar",
			expected:  `"\tFoo\tBar"`,
			useSingle: false,
		},
		{
			s:         "Foo\n\u000aBar",
			expected:  `'Foo\n\nBar'`,
			useSingle: true,
		},
		{
			s:         "Foo\n\\u000a\rBar",
			expected:  `'Foo\n\n\rBar'`,
			useSingle: true,
		},
		{
			s:         "'Foo'\\n\"Bar\\\"",
			expected:  `'\'Foo\'\n"Bar"'`,
			useSingle: true,
		},
		{
			s:         "'Foo'\\n\"Bar\\\"",
			expected:  `"'Foo'\n\"Bar\""`,
			useSingle: false,
		},
	}

	for _, tc := range tests {
		actual := stringQuote(tc.s, tc.useSingle)
		assert.Equal(t, tc.expected, actual)
	}
}

func newLiteralNumber(in string) *ast.LiteralNumber {
	f, err := strconv.ParseFloat(in, 64)
	if err != nil {
		return &ast.LiteralNumber{OriginalString: in, Value: 0}
	}
	return &ast.LiteralNumber{OriginalString: in, Value: f}
}

// newIdentifier creates an identifier.
func newIdentifier(value string) *ast.Identifier {
	id := ast.Identifier(value)
	return &id
}

type noopNode struct{}

func (n *noopNode) Context() ast.Context             { return nil }
func (n *noopNode) Loc() *ast.LocationRange          { return nil }
func (n *noopNode) FreeVariables() ast.Identifiers   { return nil }
func (n *noopNode) SetFreeVariables(ast.Identifiers) {}
func (n *noopNode) SetContext(ast.Context)           {}
