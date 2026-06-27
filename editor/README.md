# Editor support for `.promptr`

Two independent pieces, both optional:

## Diagnostics — `promptr-lsp`

A minimal language server that publishes diagnostics as you type: syntax errors
from the parser plus the semantic checks in `dsl.Validate` (unresolved
types/clients, malformed unions, mismatched `test` blocks).

```sh
go install github.com/zkrebbekx/promptr/cmd/promptr-lsp@latest
```

Point your editor's LSP client at the `promptr-lsp` binary for the `promptr`
language, over stdio. Neovim example:

```lua
vim.filetype.add({ extension = { promptr = "promptr" } })
vim.api.nvim_create_autocmd("FileType", {
  pattern = "promptr",
  callback = function()
    vim.lsp.start({ name = "promptr", cmd = { "promptr-lsp" } })
  end,
})
```

Scope is diagnostics only. Hover/completion/go-to-definition are future work —
the parser and validator already expose what they'd need.

You can run the same checks in CI without an editor:

```sh
promptr check ./...
```

## Syntax highlighting — tree-sitter grammar

`tree-sitter-promptr/grammar.js` is a hand-authored tree-sitter grammar mirroring
`dsl/lexer.go` + `dsl/parser.go`. Building it into a parser needs the tree-sitter
CLI and Node (kept out of the Go build/CI on purpose):

```sh
cd editor/tree-sitter-promptr
npm install -g tree-sitter-cli
tree-sitter generate
tree-sitter test
```

Then register it with your editor's tree-sitter integration (e.g. nvim-treesitter
or the Zed/Helix grammar config) for the `promptr` filetype.
