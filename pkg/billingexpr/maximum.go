package billingexpr

import (
	"fmt"
	"math"

	"github.com/expr-lang/expr/ast"
	"github.com/expr-lang/expr/parser"
)

// FreezeRequestPricing folds request/time predicates into literals before a
// quote is authorized. Only derived numbers/booleans survive; body/header
// contents are never copied into the persisted expression. Token predicates
// remain dynamic so settlement can still select the correct usage tier.
func FreezeRequestPricing(expression string, request RequestInput) (string, error) {
	version, body := ParseExprVersion(expression)
	if _, err := CompileFromCache(expression); err != nil {
		return "", err
	}
	tree, err := parser.Parse(body)
	if err != nil {
		return "", err
	}
	visitor := &requestPricingFreezer{request: request}
	ast.Walk(&tree.Node, visitor)
	if usesRequestProbe(tree.Node) {
		return "", fmt.Errorf("request pricing must reduce to numeric amounts or boolean conditions")
	}
	frozen := fmt.Sprintf("v%d:%s", version, tree.Node.String())
	if _, err := CompileFromCache(frozen); err != nil {
		return "", err
	}
	return frozen, nil
}

type requestPricingFreezer struct{ request RequestInput }

func (v *requestPricingFreezer) Visit(node *ast.Node) {
	if !usesRequestProbe(*node) || containsPricingMarker(*node) {
		return
	}
	if ast.Find(*node, func(n ast.Node) bool {
		ident, ok := n.(*ast.IdentifierNode)
		if !ok {
			return false
		}
		switch ident.Value {
		case "p", "c", "len", "cr", "cc", "cc1h", "img", "img_cr", "img_o", "ai", "ao", "image_count", "u":
			return true
		}
		return false
	}) != nil {
		return
	}
	expression := (*node).String()
	// Compile uses AsFloat64; a boolean subtree needs an explicit conversion.
	value, _, err := RunExprWithRequest(expression, TokenParams{}, v.request)
	if err == nil && !math.IsNaN(value) && !math.IsInf(value, 0) {
		if call, ok := (*node).(*ast.CallNode); ok {
			if callee, ok := call.Callee.(*ast.IdentifierNode); ok {
				switch callee.Value {
				case "hour", "minute", "weekday", "month", "day":
					ast.Patch(node, &ast.IntegerNode{Value: int(value)})
					return
				}
			}
		}
		ast.Patch(node, &ast.FloatNode{Value: value})
		return
	}
	value, _, err = RunExprWithRequest("("+expression+") ? 1.0 : 0.0", TokenParams{}, v.request)
	if err == nil {
		ast.Patch(node, &ast.BoolNode{Value: value == 1})
	}
}

type costInterval struct{ low, high float64 }

// UsageBound is an enforced workload range, not a spending limit or an estimate.
type UsageBound struct{ Minimum, Maximum float64 }

func MaximumTaskQuota(snapshot *BillingSnapshot, bounds map[string]UsageBound) (float64, error) {
	if snapshot == nil || !snapshot.TaskUsageBilling {
		return 0, fmt.Errorf("invalid task budget snapshot")
	}
	_, body := ParseExprVersion(snapshot.ExprString)
	tree, err := parser.Parse(body)
	if err != nil {
		return 0, err
	}
	variables := make(map[string]costInterval, len(bounds))
	for key, bound := range bounds {
		if bound.Minimum < 0 || bound.Maximum < bound.Minimum || math.IsNaN(bound.Minimum) || math.IsNaN(bound.Maximum) || math.IsInf(bound.Minimum, 0) || math.IsInf(bound.Maximum, 0) {
			return 0, fmt.Errorf("invalid task usage bound for %s", key)
		}
		variables["u:"+key] = costInterval{bound.Minimum, bound.Maximum}
	}
	interval, err := boundPricingCost(tree.Node, variables)
	if err != nil {
		return 0, err
	}
	maximum := quotaConversion(interval.high, snapshot)
	if maximum < 0 || math.IsNaN(maximum) || math.IsInf(maximum, 0) {
		return 0, fmt.Errorf("task expression has no finite nonnegative upper bound")
	}
	return maximum, nil
}

// MaximumTokenQuota bounds every supported arithmetic branch, rather than
// evaluating a single large-token sample (which is unsafe for nonmonotonic
// tiers). Unsupported/unbounded arithmetic fails before the upstream call.
func MaximumTokenQuota(snapshot *BillingSnapshot, bounds TokenParams, imageCount int) (float64, error) {
	if snapshot == nil || snapshot.TaskUsageBilling {
		return 0, fmt.Errorf("invalid token budget snapshot")
	}
	_, body := ParseExprVersion(snapshot.ExprString)
	tree, err := parser.Parse(body)
	if err != nil {
		return 0, err
	}
	upperBounds := map[string]float64{"p": bounds.P, "c": bounds.C, "len": bounds.Len, "cr": bounds.CR, "cc": bounds.CC, "cc1h": bounds.CC1h, "img": bounds.Img, "img_cr": bounds.ImgCR, "img_o": bounds.ImgO, "ai": bounds.AI, "ao": bounds.AO, "image_count": float64(imageCount)}
	variables := make(map[string]costInterval, len(upperBounds))
	for key, value := range upperBounds {
		if value < 0 || math.IsNaN(value) || math.IsInf(value, 0) {
			return 0, fmt.Errorf("invalid token budget bound")
		}
		variables[key] = costInterval{0, value}
	}
	variables["image_count"] = costInterval{1, float64(imageCount)}
	interval, err := boundPricingCost(tree.Node, variables)
	if err != nil {
		return 0, err
	}
	value := quotaConversion(interval.high, snapshot)
	if value < 0 || math.IsNaN(value) || math.IsInf(value, 0) {
		return 0, fmt.Errorf("billing expression has no finite nonnegative upper bound")
	}
	return value, nil
}

func boundPricingCost(node ast.Node, variables map[string]costInterval) (costInterval, error) {
	var result costInterval
	switch n := node.(type) {
	case *ast.IntegerNode:
		result = costInterval{float64(n.Value), float64(n.Value)}
	case *ast.FloatNode:
		result = costInterval{n.Value, n.Value}
	case *ast.IdentifierNode:
		value, ok := variables[n.Value]
		if !ok {
			return result, fmt.Errorf("unbounded pricing variable %s", n.Value)
		}
		result = value
	case *ast.UnaryNode:
		value, err := boundPricingCost(n.Node, variables)
		if err != nil {
			return result, err
		}
		switch n.Operator {
		case "+":
			result = value
		case "-":
			result = costInterval{-value.high, -value.low}
		default:
			return result, fmt.Errorf("unsupported budget unary operator %s", n.Operator)
		}
	case *ast.ConditionalNode:
		if condition, ok := n.Cond.(*ast.BoolNode); ok {
			if condition.Value {
				return boundPricingCost(n.Exp1, variables)
			}
			return boundPricingCost(n.Exp2, variables)
		}
		left, err := boundPricingCost(n.Exp1, variables)
		if err != nil {
			return result, err
		}
		right, err := boundPricingCost(n.Exp2, variables)
		if err != nil {
			return result, err
		}
		result = costInterval{min(left.low, right.low), max(left.high, right.high)}
	case *ast.BinaryNode:
		a, err := boundPricingCost(n.Left, variables)
		if err != nil {
			return result, err
		}
		b, err := boundPricingCost(n.Right, variables)
		if err != nil {
			return result, err
		}
		switch n.Operator {
		case "+":
			result = costInterval{a.low + b.low, a.high + b.high}
		case "-":
			result = costInterval{a.low - b.high, a.high - b.low}
		case "*":
			values := []float64{a.low * b.low, a.low * b.high, a.high * b.low, a.high * b.high}
			result = costInterval{values[0], values[0]}
			for _, v := range values[1:] {
				result.low = min(result.low, v)
				result.high = max(result.high, v)
			}
		case "/":
			if b.low <= 0 && b.high >= 0 {
				return result, fmt.Errorf("pricing denominator can reach zero within the authorized usage")
			}
			values := []float64{a.low / b.low, a.low / b.high, a.high / b.low, a.high / b.high}
			result = costInterval{values[0], values[0]}
			for _, v := range values[1:] {
				result.low = min(result.low, v)
				result.high = max(result.high, v)
			}
		default:
			return result, fmt.Errorf("unsupported budget operator %s", n.Operator)
		}
	case *ast.CallNode:
		callee, ok := n.Callee.(*ast.IdentifierNode)
		if !ok {
			return result, fmt.Errorf("unsupported budget function")
		}
		if callee.Value == "tier" && len(n.Arguments) == 2 {
			return boundPricingCost(n.Arguments[1], variables)
		}
		if callee.Value == "u" && len(n.Arguments) == 1 {
			key, literal := n.Arguments[0].(*ast.StringNode)
			if !literal {
				return result, fmt.Errorf("task budget requires a literal usage key")
			}
			bound, exists := variables["u:"+key.Value]
			if !exists {
				return result, fmt.Errorf("task usage %s has no verified maximum", key.Value)
			}
			return bound, nil
		}
		values := make([]costInterval, len(n.Arguments))
		for i, arg := range n.Arguments {
			value, err := boundPricingCost(arg, variables)
			if err != nil {
				return result, err
			}
			values[i] = value
		}
		if len(values) == 1 {
			a := values[0]
			switch callee.Value {
			case "fixed":
				result = costInterval{a.low * 1_000_000, a.high * 1_000_000}
			case "abs":
				result = costInterval{min(math.Abs(a.low), math.Abs(a.high)), max(math.Abs(a.low), math.Abs(a.high))}
				if a.low <= 0 && a.high >= 0 {
					result.low = 0
				}
			case "ceil":
				result = costInterval{math.Ceil(a.low), math.Ceil(a.high)}
			case "floor":
				result = costInterval{math.Floor(a.low), math.Floor(a.high)}
			default:
				return result, fmt.Errorf("unsupported budget function %s", callee.Value)
			}
		} else if len(values) == 2 {
			a, b := values[0], values[1]
			switch callee.Value {
			case "min":
				result = costInterval{min(a.low, b.low), min(a.high, b.high)}
			case "max":
				result = costInterval{max(a.low, b.low), max(a.high, b.high)}
			default:
				return result, fmt.Errorf("unsupported budget function %s", callee.Value)
			}
		} else {
			return result, fmt.Errorf("unsupported budget function %s", callee.Value)
		}
	default:
		return result, fmt.Errorf("unsupported pricing budget expression")
	}
	if math.IsNaN(result.low) || math.IsNaN(result.high) || math.IsInf(result.low, 0) || math.IsInf(result.high, 0) {
		return result, fmt.Errorf("pricing budget arithmetic is not finite")
	}
	return result, nil
}
