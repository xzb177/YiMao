package handlers

import (
	"go/ast"
	"go/parser"
	"go/token"
	"testing"
)

// Callback handlers must return callback.Response (or use the privacy-safe
// callback helper when more than one message is unavoidable). Calling Telegram
// mutation methods directly with ctx.ChatID bypasses the central renderer and
// can publish a user's callback result to the whole group.
func TestCoreCallbackHandlersDoNotMutateCtxChatDirectly(t *testing.T) {
	fset := token.NewFileSet()
	for _, name := range []string{"request.go", "feedback.go", "admin.go"} {
		t.Run(name, func(t *testing.T) {
			file, err := parser.ParseFile(fset, name, nil, 0)
			if err != nil {
				t.Fatal(err)
			}
			ast.Inspect(file, func(n ast.Node) bool {
				call, ok := n.(*ast.CallExpr)
				if !ok {
					return true
				}
				sel, ok := call.Fun.(*ast.SelectorExpr)
				if !ok || (sel.Sel.Name != "SendMessage" && sel.Sel.Name != "EditMessage" && sel.Sel.Name != "DeleteMessage") {
					return true
				}
				for _, arg := range call.Args {
					argSel, ok := arg.(*ast.SelectorExpr)
					if !ok {
						continue
					}
					ident, identOK := argSel.X.(*ast.Ident)
					if identOK && ident.Name == "ctx" && (argSel.Sel.Name == "ChatID" || argSel.Sel.Name == "MessageID") {
						t.Errorf("direct %s(ctx.%s) bypasses callback renderer at %s", sel.Sel.Name, argSel.Sel.Name, fset.Position(call.Pos()))
					}
				}
				return true
			})
		})
	}
}
