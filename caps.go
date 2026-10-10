package main

import (
	"encoding/json"
	"strings"
)

// The ways a server takes the changes of documents.
const (
	syncNone        = 0
	syncFull        = 1
	syncIncremental = 2
)

// serverCaps are what a language server does, as its capabilities say.
type serverCaps struct {
	sync   int
	legend semanticLegend
	// The requests it answers.
	hover, definition, typeDefinition, implementation, references bool
	completion, signatureHelp, rename, codeAction                 bool
	formatting, rangeFormatting, documentSymbol, workspaceSymbol  bool
	documentHighlight, inlayHint, foldingRange                    bool
	// completionTriggers are the characters asking for completions as
	// they are typed, signatureTriggers for the signature;
	// completionResolve that it tells more of a completion chosen.
	completionTriggers, signatureTriggers []string
	completionResolve                     bool
}

// parseCaps reads the capabilities of a server's answer to initialize.
func parseCaps(raw json.RawMessage) serverCaps {
	var c struct {
		TextDocumentSync       json.RawMessage `json:"textDocumentSync"`
		SemanticTokensProvider *struct {
			Legend semanticLegend `json:"legend"`
		} `json:"semanticTokensProvider"`
		HoverProvider                   json.RawMessage `json:"hoverProvider"`
		DefinitionProvider              json.RawMessage `json:"definitionProvider"`
		TypeDefinitionProvider          json.RawMessage `json:"typeDefinitionProvider"`
		ImplementationProvider          json.RawMessage `json:"implementationProvider"`
		ReferencesProvider              json.RawMessage `json:"referencesProvider"`
		RenameProvider                  json.RawMessage `json:"renameProvider"`
		CodeActionProvider              json.RawMessage `json:"codeActionProvider"`
		DocumentFormattingProvider      json.RawMessage `json:"documentFormattingProvider"`
		DocumentRangeFormattingProvider json.RawMessage `json:"documentRangeFormattingProvider"`
		DocumentSymbolProvider          json.RawMessage `json:"documentSymbolProvider"`
		WorkspaceSymbolProvider         json.RawMessage `json:"workspaceSymbolProvider"`
		DocumentHighlightProvider       json.RawMessage `json:"documentHighlightProvider"`
		InlayHintProvider               json.RawMessage `json:"inlayHintProvider"`
		FoldingRangeProvider            json.RawMessage `json:"foldingRangeProvider"`
		CompletionProvider              *struct {
			TriggerCharacters []string `json:"triggerCharacters"`
			ResolveProvider   bool     `json:"resolveProvider"`
		} `json:"completionProvider"`
		SignatureHelpProvider *struct {
			TriggerCharacters   []string `json:"triggerCharacters"`
			RetriggerCharacters []string `json:"retriggerCharacters"`
		} `json:"signatureHelpProvider"`
	}
	json.Unmarshal(raw, &c)
	out := serverCaps{sync: syncIncremental}
	// A number, or options with the number as change.
	var kind int
	if json.Unmarshal(c.TextDocumentSync, &kind) == nil && len(c.TextDocumentSync) > 0 {
		out.sync = kind
	} else {
		var opts struct {
			Change *int `json:"change"`
		}
		if json.Unmarshal(c.TextDocumentSync, &opts) == nil && opts.Change != nil {
			out.sync = *opts.Change
		}
	}
	if p := c.SemanticTokensProvider; p != nil {
		out.legend = p.Legend
	}
	out.hover = provides(c.HoverProvider)
	out.definition = provides(c.DefinitionProvider)
	out.typeDefinition = provides(c.TypeDefinitionProvider)
	out.implementation = provides(c.ImplementationProvider)
	out.references = provides(c.ReferencesProvider)
	out.rename = provides(c.RenameProvider)
	out.codeAction = provides(c.CodeActionProvider)
	out.formatting = provides(c.DocumentFormattingProvider)
	out.rangeFormatting = provides(c.DocumentRangeFormattingProvider)
	out.documentSymbol = provides(c.DocumentSymbolProvider)
	out.workspaceSymbol = provides(c.WorkspaceSymbolProvider)
	out.documentHighlight = provides(c.DocumentHighlightProvider)
	out.inlayHint = provides(c.InlayHintProvider)
	out.foldingRange = provides(c.FoldingRangeProvider)
	if p := c.CompletionProvider; p != nil {
		out.completion = true
		out.completionTriggers = p.TriggerCharacters
		out.completionResolve = p.ResolveProvider
	}
	if p := c.SignatureHelpProvider; p != nil {
		out.signatureHelp = true
		out.signatureTriggers = append(p.TriggerCharacters, p.RetriggerCharacters...)
	}
	return out
}

// provides reports whether a capability says a server answers: true, or
// options; not absent, null or false.
func provides(raw json.RawMessage) bool {
	s := strings.TrimSpace(string(raw))
	return s != "" && s != "null" && s != "false"
}

// settingsSection returns a section of settings, as python.analysis, by its
// dotted path; nil for none. "" is all of them.
func settingsSection(settings map[string]any, name string) any {
	if settings == nil {
		return nil
	}
	if name == "" {
		return settings
	}
	if v, ok := settings[name]; ok {
		return v
	}
	var cur any = settings
	for _, k := range strings.Split(name, ".") {
		m, ok := cur.(map[string]any)
		if !ok {
			return nil
		}
		if cur, ok = m[k]; !ok {
			return nil
		}
	}
	return cur
}
