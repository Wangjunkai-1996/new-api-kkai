package creditmigration

import (
	"fmt"
	"math"
	"reflect"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/pkg/billingexpr"
	"github.com/QuantumNous/new-api/pkg/imagepricing"
	"github.com/expr-lang/expr/ast"
	"github.com/expr-lang/expr/parser"
)

// DivideMonetaryExpression changes only monetary coefficients inside pricing
// branches. Keeping the tier shape also preserves price-page/log rendering.
// Unknown shapes fail closed rather than rewriting thresholds or multipliers.
func DivideMonetaryExpression(source string) (string, error) {
	_, body := billingexpr.ParseExprVersion(source)
	tree, err := parser.Parse(body)
	if err != nil {
		return "", err
	}
	if err := scaleCurrencyBranch(tree.Node); err != nil {
		return "", err
	}
	target := tree.Node.String()
	if strings.HasPrefix(source, "v1:") {
		target = "v1:" + target
	}
	if _, err := billingexpr.CompileFromCache(target); err != nil {
		return "", err
	}
	return target, nil
}

func scaleCurrencyBranch(node ast.Node) error {
	switch n := node.(type) {
	case *ast.ConditionalNode:
		if err := scaleCurrencyBranch(n.Exp1); err != nil {
			return err
		}
		return scaleCurrencyBranch(n.Exp2)
	case *ast.CallNode:
		name, ok := n.Callee.(*ast.IdentifierNode)
		if !ok || name.Value != "tier" || len(n.Arguments) != 2 {
			return fmt.Errorf("unreviewed monetary call requires an explicit monetary literal adapter")
		}
		// Legacy per-request schedules store a raw monetary result directly
		// inside tier(), in the existing token-expression conversion unit.
		switch amount := n.Arguments[1].(type) {
		case *ast.IntegerNode:
			if amount.Value < 0 {
				return fmt.Errorf("negative tier amount")
			}
			n.Arguments[1] = &ast.FloatNode{Value: float64(amount.Value) / 7}
			return nil
		case *ast.FloatNode:
			if amount.Value < 0 || math.IsNaN(amount.Value) || math.IsInf(amount.Value, 0) {
				return fmt.Errorf("invalid tier amount")
			}
			amount.Value /= 7
			return nil
		}
		return scaleLinearPrice(n.Arguments[1])
	case *ast.BinaryNode:
		if n.Operator == "*" {
			left, right := &tierProbe{}, &tierProbe{}
			ast.Walk(&n.Left, left)
			ast.Walk(&n.Right, right)
			if left.found && !right.found {
				return scaleCurrencyBranch(n.Left)
			}
			if right.found && !left.found {
				return scaleCurrencyBranch(n.Right)
			}
		}
	}
	return scaleLinearPrice(node)
}

func scaleLinearPrice(node ast.Node) error {
	n, ok := node.(*ast.BinaryNode)
	if !ok {
		return fmt.Errorf("nonlinear or fixed pricing requires an explicit monetary literal adapter")
	}
	if n.Operator == "+" {
		if err := scaleLinearPrice(n.Left); err != nil {
			return err
		}
		return scaleLinearPrice(n.Right)
	}
	if n.Operator != "*" {
		return fmt.Errorf("unreviewed monetary term requires an explicit monetary literal adapter")
	}
	variable, price := n.Left, &n.Right
	if _, ok := variable.(*ast.IdentifierNode); !ok {
		variable, price = n.Right, &n.Left
	}
	identifier, ok := variable.(*ast.IdentifierNode)
	if !ok {
		return fmt.Errorf("monetary term is not a token coefficient")
	}
	switch identifier.Value {
	case "p", "c", "cr", "cc", "cc1h", "img", "img_cr", "img_o", "ai", "ao":
	default:
		return fmt.Errorf("unreviewed monetary variable %s", identifier.Value)
	}
	var amount float64
	switch literal := (*price).(type) {
	case *ast.IntegerNode:
		amount = float64(literal.Value)
	case *ast.FloatNode:
		amount = literal.Value
	default:
		return fmt.Errorf("monetary coefficient must be a numeric literal")
	}
	if amount < 0 || math.IsInf(amount, 0) || math.IsNaN(amount) {
		return fmt.Errorf("invalid monetary coefficient")
	}
	*price = &ast.FloatNode{Value: amount / 7}
	return nil
}

type tierProbe struct{ found bool }

func (p *tierProbe) Visit(node *ast.Node) {
	if call, ok := (*node).(*ast.CallNode); ok {
		if name, ok := call.Callee.(*ast.IdentifierNode); ok && name.Value == "tier" {
			p.found = true
		}
	}
}

func DivideImagePricingPolicy(source, version string) (string, error) {
	var policy imagepricing.Config
	if err := common.UnmarshalJsonStr(source, &policy); err != nil {
		return "", err
	}
	if _, err := imagepricing.Compile(policy); err != nil {
		return "", err
	}
	if strings.TrimSpace(version) == "" || version == policy.Version {
		return "", fmt.Errorf("image policy requires a new explicit version")
	}
	policy.Version = version
	for name, model := range policy.Models {
		for tier, price := range model.Tiers {
			price.UnitPrice /= 7
			model.Tiers[tier] = price
		}
		policy.Models[name] = model
	}
	if _, err := imagepricing.Compile(policy); err != nil {
		return "", err
	}
	encoded, err := common.Marshal(policy)
	return string(encoded), err
}

func validateMonetaryTransforms(changes map[string]OptionChange) error {
	for key, change := range changes {
		if change.Before == nil {
			continue
		}
		switch key {
		case "SelfUseModeEnabled":
			if *change.Before != change.After {
				return fmt.Errorf("%w: self-use pricing fallback must remain unchanged", ErrPricingInventoryIncomplete)
			}
		case "ModelRatio", "ModelPrice", "tool_price_setting.prices":
			var before, after map[string]float64
			if common.UnmarshalJsonStr(*change.Before, &before) != nil || common.UnmarshalJsonStr(change.After, &after) != nil || len(before) != len(after) {
				return fmt.Errorf("%w: invalid monetary map %s", ErrPricingInventoryIncomplete, key)
			}
			for model, price := range before {
				value, ok := after[model]
				if !ok || math.IsNaN(price) || math.IsInf(price, 0) || price < 0 || value != price/7 {
					return fmt.Errorf("%w: %s.%s must divide the current price by 7", ErrPricingInventoryIncomplete, key, model)
				}
			}
		case "billing_setting.billing_expr":
			var before, after map[string]string
			if common.UnmarshalJsonStr(*change.Before, &before) != nil || common.UnmarshalJsonStr(change.After, &after) != nil || len(before) != len(after) {
				return fmt.Errorf("%w: invalid expression map", ErrPricingInventoryIncomplete)
			}
			for model, expression := range before {
				target, err := DivideMonetaryExpression(expression)
				if err != nil || after[model] != target {
					return fmt.Errorf("%w: expression %s must divide the current monetary result by 7", ErrPricingInventoryIncomplete, model)
				}
			}
		case "ImagePricingPolicy":
			var after imagepricing.Config
			if err := common.UnmarshalJsonStr(change.After, &after); err != nil {
				return err
			}
			expected, err := DivideImagePricingPolicy(*change.Before, after.Version)
			if err != nil || !optionValuesEqual(expected, change.After) {
				return fmt.Errorf("%w: image policy must only change its version and monetary unit prices", ErrPricingInventoryIncomplete)
			}
		case "CompletionRatio", "CacheRatio", "CreateCacheRatio", "CacheCreationRatio", "ImageRatio", "AudioRatio", "AudioCompletionRatio", "billing_setting.billing_mode":
			var before, after any
			if common.UnmarshalJsonStr(*change.Before, &before) != nil || common.UnmarshalJsonStr(change.After, &after) != nil || !reflect.DeepEqual(before, after) {
				return fmt.Errorf("%w: dimensionless option %s must remain unchanged", ErrPricingInventoryIncomplete, key)
			}
		}
	}
	return nil
}
