package billingexpr

import (
	"fmt"
	"math"
	"strings"

	"github.com/expr-lang/expr/ast"
)

// ValidateDisplayBillingExpr permits only cache-read unit prices to differ.
// The shapes come from fresh, unoptimized parser trees: cr * 0 must remain a
// separately priced category, and conditions must never be rewritten.
func ValidateDisplayBillingExpr(actual, display string) error {
	actualEntry, err := compileEntryFromCacheByHash(actual, ExprHashString(actual))
	if err != nil {
		return err
	}
	displayEntry, err := compileEntryFromCacheByHash(display, ExprHashString(display))
	if err != nil {
		return err
	}
	if actualEntry.version != displayEntry.version || actualEntry.cacheReadPriceShape == "" ||
		actualEntry.cacheReadPriceShape != displayEntry.cacheReadPriceShape {
		return fmt.Errorf("display billing expression requires unique tier names and may only change cache-read unit prices; regenerate or remove it when changing billing rules")
	}
	return nil
}

// cacheReadDisplayShape visits pricing leaves, never tier conditions or request
// multipliers. Task and fixed pricing have no cache-read display override.
func cacheReadDisplayShape(node ast.Node) string {
	tiers := make(map[string]bool)
	if ast.Find(node, func(part ast.Node) bool {
		identifier, ok := part.(*ast.IdentifierNode)
		if ok && (identifier.Value == "fixed" || identifier.Value == "u") {
			return true
		}
		call, ok := part.(*ast.CallNode)
		if !ok || len(call.Arguments) != 2 {
			return false
		}
		callee, ok := call.Callee.(*ast.IdentifierNode)
		if !ok || callee.Value != "tier" {
			return false
		}
		label, ok := call.Arguments[0].(*ast.StringNode)
		if !ok {
			return true
		}
		// Keep this equivalent to the log UI's normalizeTierLabel lookup.
		name := strings.ToLower(strings.Join(strings.Fields(label.Value), ""))
		name = strings.NewReplacer("<=", "<", "<＝", "<", "≤", "<", "＜=", "<", "＜＝", "<", "＜", "<",
			">=", ">", ">＝", ">", "≥", ">", "＞=", ">", "＞＝", ">", "＞", ">").Replace(name)
		if name == "" || tiers[name] {
			return true
		}
		tiers[name] = true
		return false
	}) != nil || normalizeCacheReadPricingLeaves(node) == 0 {
		return ""
	}
	return node.String()
}

func normalizeCacheReadPricingLeaves(node ast.Node) int {
	switch part := node.(type) {
	case *ast.ConditionalNode:
		return normalizeCacheReadPricingLeaves(part.Exp1) + normalizeCacheReadPricingLeaves(part.Exp2)
	case *ast.BinaryNode:
		if part.Operator != "*" {
			return 0
		}
		if containsPricingMarker(part.Left) && !containsPricingMarker(part.Right) {
			return normalizeCacheReadPricingLeaves(part.Left)
		}
		if containsPricingMarker(part.Right) && !containsPricingMarker(part.Left) {
			return normalizeCacheReadPricingLeaves(part.Right)
		}
	case *ast.CallNode:
		callee, ok := part.Callee.(*ast.IdentifierNode)
		if ok && callee.Value == "tier" && len(part.Arguments) == 2 {
			return normalizeCacheReadPriceTerms(part.Arguments[1])
		}
	}
	return 0
}

func normalizeCacheReadPriceTerms(node ast.Node) int {
	part, ok := node.(*ast.BinaryNode)
	if !ok {
		return 0
	}
	if part.Operator == "+" {
		return normalizeCacheReadPriceTerms(part.Left) + normalizeCacheReadPriceTerms(part.Right)
	}
	if part.Operator != "*" {
		return 0
	}
	variable, price := part.Left, &part.Right
	if _, ok := variable.(*ast.IdentifierNode); !ok {
		variable, price = part.Right, &part.Left
	}
	identifier, ok := variable.(*ast.IdentifierNode)
	if !ok || identifier.Value != "cr" {
		return 0
	}
	amount, ok := requestRuleNumber(*price)
	if !ok || amount < 0 || math.IsNaN(amount) || math.IsInf(amount, 0) {
		return 0
	}
	*price = &ast.IntegerNode{Value: 0}
	return 1
}
