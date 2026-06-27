/**
 * tree-sitter grammar for the .promptr schema language.
 *
 * This is a hand-authored artifact: it mirrors dsl/lexer.go + dsl/parser.go so
 * any editor with tree-sitter support gets syntax highlighting for .promptr.
 * Building it (into a parser.c / WASM) needs the tree-sitter CLI and Node and is
 * intentionally left out of the Go build/CI — see editor/README.md.
 */
module.exports = grammar({
  name: "promptr",

  extras: ($) => [/\s/, $.comment],

  word: ($) => $.identifier,

  rules: {
    source_file: ($) => repeat($._declaration),

    _declaration: ($) =>
      choice(
        $.enum_decl,
        $.class_decl,
        $.union_decl,
        $.client_decl,
        $.function_decl,
        $.test_decl,
      ),

    enum_decl: ($) =>
      seq("enum", field("name", $.identifier), "{", repeat($.identifier), "}"),

    class_decl: ($) =>
      seq("class", field("name", $.identifier), "{", repeat($.field), "}"),

    field: ($) =>
      seq(
        field("name", $.identifier),
        field("type", $.type),
        repeat($.attribute),
        optional(","),
      ),

    attribute: ($) =>
      seq("@", field("name", $.identifier), "(", $.string, ")"),

    union_decl: ($) =>
      seq(
        "union",
        field("name", $.identifier),
        "=",
        sep1($.identifier, "|"),
      ),

    type: ($) => choice($.map_type, $.union_type, $.named_type),

    named_type: ($) =>
      seq(
        field("base", $.identifier),
        optional(seq("[", "]")),
        optional("?"),
      ),

    map_type: ($) =>
      seq("map", "<", $.identifier, ",", $.type, ">", optional("?")),

    union_type: ($) => prec.left(seq($.identifier, repeat1(seq("|", $.identifier)))),

    client_decl: ($) =>
      seq(
        "client",
        field("name", $.identifier),
        "{",
        repeat($._client_setting),
        "}",
      ),

    _client_setting: ($) =>
      choice(
        seq("provider", $.string),
        seq("model", $.string),
        seq("retry", $.number),
        seq("fallback", $.ident_list),
        seq("round_robin", $.ident_list),
        seq($.identifier, $.string),
      ),

    ident_list: ($) => seq("[", optional(sep1($.identifier, ",")), "]"),

    function_decl: ($) =>
      seq(
        "function",
        field("name", $.identifier),
        "(",
        optional(sep1($.param, ",")),
        ")",
        "->",
        optional("stream"),
        field("return", $.type),
        "{",
        repeat($._function_body),
        "}",
      ),

    param: ($) => seq(field("name", $.identifier), ":", field("type", $.type)),

    _function_body: ($) =>
      choice(
        seq("client", field("client", $.identifier)),
        seq("prompt", field("prompt", choice($.raw_string, $.string))),
      ),

    test_decl: ($) =>
      seq(
        "test",
        field("name", $.identifier),
        "{",
        repeat($._test_body),
        "}",
      ),

    _test_body: ($) =>
      choice(
        seq("function", $.identifier),
        seq("args", "{", repeat(seq($.identifier, $._value)), "}"),
      ),

    _value: ($) => choice($.string, $.raw_string, $.number, $.identifier),

    identifier: ($) => /[A-Za-z_][A-Za-z0-9_]*/,
    number: ($) => /[0-9]+/,
    string: ($) => /"([^"\\]|\\.)*"/,
    raw_string: ($) => seq('#"', /([^"]|"[^#])*/, '"#'),
    comment: ($) => token(seq("//", /.*/)),
  },
});

function sep1(rule, separator) {
  return seq(rule, repeat(seq(separator, rule)));
}
