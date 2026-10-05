#!/usr/bin/env python3
"""Prepare exact option before/after images from redacted, read-only snapshots.

This performs no database or network operations. Prices are always derived
from the observed values; provider/official price catalogues are not inputs.
The Go pricing-plan command validates the output before it is executable.
"""
import argparse
import hashlib
import json
import os
from pathlib import Path


def encode(value):
    return json.dumps(value, ensure_ascii=False, separators=(",", ":"), sort_keys=True)


def save(path, value):
    fd = os.open(path, os.O_WRONLY | os.O_CREAT | os.O_TRUNC, 0o600)
    os.fchmod(fd, 0o600)
    with os.fdopen(fd, "w") as output:
        json.dump(value, output, ensure_ascii=False, indent=2)
        output.write("\n")


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--snapshot", action="append", required=True)
    parser.add_argument("--compatibility", required=True)
    parser.add_argument("--expression-prices", required=True)
    parser.add_argument("--migration-id", required=True)
    parser.add_argument("--out-prefix", required=True)
    args = parser.parse_args()
    options, active, sources, counts, used_groups = {}, [], [], {}, []
    for path in args.snapshot:
        raw = Path(path).read_bytes()
        snapshot = json.loads(raw)
        if snapshot.get("mode") != "read_only":
            raise ValueError("snapshot must explicitly identify its read-only source")
        sources.append({"path": str(Path(path).resolve()), "sha256": hashlib.sha256(raw).hexdigest(),
                        "captured_at": snapshot["captured_at"]})
        for key in ("billing_options", "billing_options_extra"):
            for name, value in snapshot.get(key, {}).items():
                if name == "error" or not isinstance(value, str):
                    raise ValueError("snapshot contains a query error")
                if name in options and options[name] != value:
                    raise ValueError("conflicting snapshot option " + name)
                options[name] = value
        active.extend(snapshot.get("active_models", []))
        counts.update(snapshot.get("pending_counts", {}))
        used_groups.extend(snapshot.get("used_groups", []))
    for table in ("subscription_orders", "subscription_plans", "user_subscriptions", "subscription_pre_consume_records"):
        if counts.get(table + "_size", {}).get("count") != 0:
            raise ValueError("nonempty or unobserved subscription table " + table)
    compatibility = json.loads(Path(args.compatibility).read_text())
    decode = lambda key: json.loads(options[key])
    source = {"price": decode("Price"), "usd_exchange_rate": decode("USDExchangeRate"),
              "quota_display_type": options["general_setting.quota_display_type"],
              "amount_discount": decode("payment_setting.amount_discount"),
              "topup_group_ratio": decode("TopupGroupRatio"), "group_ratio": decode("GroupRatio"),
              "group_group_ratio": decode("GroupGroupRatio")}
    if source["price"] != 0.075 or source["usd_exchange_rate"] != 1:
        raise ValueError("unexpected legacy price/exchange; source must be re-reviewed")
    if not used_groups:
        raise ValueError("observed enabled ability group coverage is required")
    source["implicit_group_ratio"] = {row["group"]: 1 for row in used_groups
                                      if row["source"] == "abilities" and row["group"] not in source["group_ratio"]}
    changes, entries, comparisons = {}, [], []

    def change(key, after):
        changes[key] = {"key": key, "before": options.get(key), "after": after}

    def entry(key, path, kind, unit, old, new, basis="divide_by_7"):
        old_value = old if isinstance(old, str) else encode(old)
        new_value = new if isinstance(new, str) else encode(new)
        entries.append({"key": key, "path": path, "kind": kind, "unit": unit, "basis": basis,
                        "old_value": old_value, "target_value": new_value, "classified": True,
                        "evidence": "Exact read-only snapshot option; current monetary result / 7; no official catalogue replacement"})

    for key, value in {"Price": "1", "USDExchangeRate": "7",
                       "general_setting.quota_display_type": "USD",
                       "payment_setting.amount_discount": "{}", "MinTopUp": "1",
                       "payment_setting.amount_options": "[1,5,10,20,50,100,200,500]"}.items():
        change(key, value)
    change("TopupGroupRatio", encode({key: 1 for key in source["topup_group_ratio"]}))
    effective_groups = {**source["group_ratio"], **source["implicit_group_ratio"]}
    change("GroupRatio", encode({key: value / 2 for key, value in effective_groups.items()}))
    change("GroupGroupRatio", encode({group: {key: value / 2 for key, value in ratios.items()}
                                      for group, ratios in source["group_group_ratio"].items()}))
    for key in ("CompletionRatio", "CacheRatio", "CreateCacheRatio", "CacheCreationRatio", "ImageRatio",
                "AudioRatio", "AudioCompletionRatio", "billing_setting.billing_mode", "SelfUseModeEnabled"):
        if key in options:
            change(key, options[key])
    for alias, canonical in {"group_ratio_setting.group_ratio": "GroupRatio",
                             "group_ratio_setting.group_group_ratio": "GroupGroupRatio"}.items():
        if alias in options:
            change(alias, changes[canonical]["after"])
    if "DisplayInCurrencyEnabled" in options:
        change("DisplayInCurrencyEnabled", "true")
    for key in ("QuotaForNewUser", "QuotaForInviter", "QuotaForInvitee", "QuotaRemindThreshold",
                "checkin_setting.min_quota", "checkin_setting.max_quota"):
        if key in options:
            old = int(options[key])
            if old < 0:
                raise ValueError("negative quota option " + key)
            change(key, str((old * 3 + 20) // 40))
    for path, kind, unit in (("ModelRatio", "ratio", "currency_per_2m_input_tokens"),
                             ("ModelPrice", "fixed", "currency_per_billed_unit"),
                             ("tool_price_setting.prices", "tool", "currency_per_1000_calls")):
        values = decode(path)
        target = {key: value / 7 for key, value in values.items()}
        change(path, encode(target))
        for key, value in sorted(values.items()):
            entry(key, path, kind, unit, value, target[key])
            comparisons.append({"path": path, "model_or_tool": key, "unit": unit,
                                "old_price": value, "target_price": target[key], "example_old_group": 0.4,
                                "example_target_group": 0.2, "old_rmb": value * 0.4 * 0.075,
                                "target_rmb": target[key] * 0.2,
                                "rmb_factor_before_quota_rounding": 20 / 21 if value else None})
    expr = decode("billing_setting.billing_expr")
    target_expr = json.loads(Path(args.expression_prices).read_text())
    if set(target_expr) != set(expr):
        raise ValueError("expression adapter must cover the exact source map")
    for model, old in sorted(expr.items()):
        entry(model, "billing_setting.billing_expr", "expression", "currency_per_1m_normalized_tokens",
              old, target_expr[model])
    change("billing_setting.billing_expr", encode(target_expr))
    image = decode("ImagePricingPolicy")
    target_image = json.loads(encode(image))
    target_image["version"] = args.migration_id + ".usd"
    for model, policy in target_image["models"].items():
        for tier, value in policy["tiers"].items():
            old = value["unit_price"]
            value["unit_price"] /= 7
            comparisons.append({"path": "ImagePricingPolicy", "model_or_tool": model, "tier": tier,
                                "unit": "currency_per_image", "old_price": old, "target_price": value["unit_price"],
                                "rmb_factor_before_quota_rounding": 20 / 21})
    for key in sorted(image):
        entry(key, "ImagePricingPolicy", "image", "image_policy_metadata" if key != "models" else "currency_per_image",
              image[key], target_image[key], "divide_by_7" if key == "models" else "manual" if key == "version" else "keep")
    change("ImagePricingPolicy", encode(target_image))
    payment_entries = []
    for key in ("Price", "USDExchangeRate", "MinTopUp", "payment_setting.amount_options", "payment_setting.amount_discount", "TopupGroupRatio"):
        payment_entries.append({"key": key, "path": key, "kind": "payment", "unit": "reviewed_recharge_config",
                                "basis": "manual", "old_value": options[key], "target_value": changes[key]["after"],
                                "evidence": "Strict CNY1 purchases USD1; discounts removed; current explicit payment options", "classified": True})
    unsupported_payment = [key for key in options if key.startswith(("Stripe", "Waffo", "Creem"))]
    if unsupported_payment:
        raise ValueError("provider-specific prices require review: " + ",".join(unsupported_payment))
    inventory = {"migration_id": args.migration_id, "source_epoch": "legacy_075", "source": source,
                 "models_complete": True, "models": entries, "payments_complete": True, "payments": payment_entries,
                 "subscriptions_complete": True, "subscriptions": [{"key": "subscription_tables", "kind": "subscription",
                    "unit": "row_count", "basis": "keep", "old_value": "0", "target_value": "0",
                    "evidence": "Read-only ledger inventory: plans/orders/subscriptions/pre-consumes empty; apply rechecks live tables", "classified": True}],
                 "compatibility": compatibility, "options": [changes[key] for key in sorted(changes)]}
    coverage = []
    ratios, fixed, modes = decode("ModelRatio"), decode("ModelPrice"), decode("billing_setting.billing_mode")
    for model in sorted({item["model"] for item in active}):
        paths = [path for path, values in (("ModelRatio", ratios), ("ModelPrice", fixed)) if model in values]
        if model in expr and modes.get(model) == "tiered_expr":
            paths.insert(0, "billing_setting.billing_expr")
        coverage.append({"model": model, "configured_paths": paths,
                         "state": "configured" if paths else "already_unpriced_self_use_disabled_preserved"})
    if any(not row["configured_paths"] for row in coverage) and options.get("SelfUseModeEnabled") != "false":
        raise ValueError("unpriced model requires explicit observed fallback review")
    report = {"migration_id": args.migration_id, "sources": sources, "policy": "observed monetary prices /7; consumption groups /2; wallet *3/40",
              "rmb_factor_before_quota_rounding": 20 / 21, "official_catalogue_used": False,
              "active_model_coverage": coverage, "comparisons": comparisons,
              "implicit_group_ratio_source": source["implicit_group_ratio"],
              "implicit_group_evidence": "enabled abilities plus setting/ratio_setting/group_ratio.go GetGroupRatio fallback=1; target explicit 0.5",
              "scope": "Local preparation only; installed runtime/maintenance/backup identity gates remain mandatory"}
    save(args.out_prefix + "-pricing-inventory.json", inventory)
    save(args.out_prefix + "-pricing-comparison.json", report)
    print(f"Prepared {len(changes)} options, {len(entries)} price entries, {len(coverage)} active model reviews")


if __name__ == "__main__":
    main()
