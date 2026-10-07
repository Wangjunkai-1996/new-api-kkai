package billingexpr

import (
	"math"

	"github.com/expr-lang/expr/ast"
	"github.com/expr-lang/expr/parser"
)

// SupportsMonotonicTokenEstimate identifies expressions whose cost cannot
// decrease when any nonnegative token dimension grows. Request-dependent and
// fixed pricing are excluded; callers must supply and enforce their own budget.
func SupportsMonotonicTokenEstimate(expression string) bool {
	_, body := ParseExprVersion(expression)
	tree, err := parser.Parse(body)
	if err != nil || !monotonicTokenCost(tree.Node) {
		return false
	}
	_, err = CompileFromCache(expression)
	return err == nil
}

func monotonicTokenCost(node ast.Node) bool {
	switch value := node.(type) {
	case *ast.IntegerNode:
		return value.Value >= 0
	case *ast.FloatNode:
		return value.Value >= 0 && !math.IsNaN(value.Value) && !math.IsInf(value.Value, 0)
	case *ast.IdentifierNode:
		switch value.Value {
		case "p", "c", "len", "cr", "cc", "cc1h", "img", "img_cr", "img_o", "ai", "ao":
			return true
		}
	case *ast.BinaryNode:
		return (value.Operator == "+" || value.Operator == "*") &&
			monotonicTokenCost(value.Left) && monotonicTokenCost(value.Right)
	case *ast.CallNode:
		callee, ok := value.Callee.(*ast.IdentifierNode)
		if !ok || callee.Value != "tier" || len(value.Arguments) != 2 {
			return false
		}
		_, literalName := value.Arguments[0].(*ast.StringNode)
		return literalName && monotonicTokenCost(value.Arguments[1])
	}
	return false
}
