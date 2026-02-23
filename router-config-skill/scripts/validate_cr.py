#!/usr/bin/env python3
"""
Validates IntelligentPool and IntelligentRoute CRs for the semantic router.

Usage:
  python3 validate_cr.py '<yaml content>'

Output:
  VALID: <summary>
  INVALID: <list of errors>
"""

import sys
import yaml


def validate_pool(spec: dict) -> list[str]:
    """Validate an IntelligentPool spec. Returns list of errors."""
    errors = []

    default_model = spec.get("defaultModel", "")
    if not default_model:
        errors.append("spec.defaultModel is required and must be non-empty")

    models = spec.get("models")
    if not models or not isinstance(models, list):
        errors.append("spec.models is required and must be a non-empty list")
        return errors

    if len(models) == 0:
        errors.append("spec.models must contain at least one model")
        return errors

    model_names = set()
    for i, model in enumerate(models):
        name = model.get("name", "")
        if not name:
            errors.append(f"spec.models[{i}].name is required and must be non-empty")
            continue

        if name in model_names:
            errors.append(f"spec.models[{i}].name '{name}' is duplicated")
        model_names.add(name)

        pricing = model.get("pricing")
        if pricing:
            for field in ("inputTokenPrice", "outputTokenPrice"):
                val = pricing.get(field)
                if val is not None:
                    try:
                        if float(val) < 0:
                            errors.append(
                                f"spec.models[{i}].pricing.{field} must be non-negative, got {val}"
                            )
                    except (TypeError, ValueError):
                        errors.append(
                            f"spec.models[{i}].pricing.{field} must be a number, got {val!r}"
                        )

    if default_model and model_names and default_model not in model_names:
        errors.append(
            f"spec.defaultModel '{default_model}' does not match any model name. "
            f"Available: {sorted(model_names)}"
        )

    return errors


def validate_route(spec: dict) -> list[str]:
    """Validate an IntelligentRoute spec. Returns list of errors."""
    errors = []

    signals = spec.get("signals", {})

    # Collect known domain names
    domains = signals.get("domains", [])
    domain_names = set()
    for i, domain in enumerate(domains):
        name = domain.get("name", "")
        if not name:
            errors.append(f"spec.signals.domains[{i}].name is required")
        else:
            domain_names.add(name)

    # Collect known keyword signal names
    keywords = signals.get("keywords", [])
    keyword_names = set()
    for i, kw in enumerate(keywords):
        name = kw.get("name", "")
        if not name:
            errors.append(f"spec.signals.keywords[{i}].name is required")
            continue
        keyword_names.add(name)

        op = kw.get("operator", "")
        if op not in ("AND", "OR", "NOR"):
            errors.append(
                f"spec.signals.keywords[{i}].operator must be AND, OR, or NOR, got '{op}'"
            )

        kw_list = kw.get("keywords", [])
        if not kw_list:
            errors.append(f"spec.signals.keywords[{i}].keywords must be a non-empty list")

    # Collect known embedding signal names
    embeddings = signals.get("embeddings", [])
    embedding_names = set()
    for i, emb in enumerate(embeddings):
        name = emb.get("name", "")
        if not name:
            errors.append(f"spec.signals.embeddings[{i}].name is required")
            continue
        embedding_names.add(name)

        threshold = emb.get("threshold")
        if threshold is not None:
            try:
                t = float(threshold)
                if t < 0 or t > 1:
                    errors.append(
                        f"spec.signals.embeddings[{i}].threshold must be between 0 and 1, got {t}"
                    )
            except (TypeError, ValueError):
                errors.append(
                    f"spec.signals.embeddings[{i}].threshold must be a number, got {threshold!r}"
                )

        candidates = emb.get("candidates", [])
        if not candidates:
            errors.append(f"spec.signals.embeddings[{i}].candidates must be a non-empty list")

    all_signal_names = domain_names | keyword_names | embedding_names

    # Validate decisions
    decisions = spec.get("decisions", [])
    if not decisions:
        errors.append("spec.decisions is required and must be a non-empty list")
        return errors

    decision_names = set()
    for i, decision in enumerate(decisions):
        d_name = decision.get("name", "")
        if not d_name:
            errors.append(f"spec.decisions[{i}].name is required")
            continue

        if d_name in decision_names:
            errors.append(f"spec.decisions[{i}].name '{d_name}' is duplicated")
        decision_names.add(d_name)

        priority = decision.get("priority", 0)
        try:
            if int(priority) < 0:
                errors.append(
                    f"spec.decisions[{i}].priority must be non-negative, got {priority}"
                )
        except (TypeError, ValueError):
            errors.append(
                f"spec.decisions[{i}].priority must be an integer, got {priority!r}"
            )

        # Validate signals combination
        sigs = decision.get("signals", {})
        operator = sigs.get("operator", "")
        if operator not in ("AND", "OR"):
            errors.append(
                f"spec.decisions[{i}].signals.operator must be AND or OR, got '{operator}'"
            )

        conditions = sigs.get("conditions", [])
        if not conditions:
            errors.append(f"spec.decisions[{i}].signals.conditions must be non-empty")

        for j, cond in enumerate(conditions):
            c_type = cond.get("type", "")
            c_name = cond.get("name", "")

            if not c_type:
                errors.append(
                    f"spec.decisions[{i}].signals.conditions[{j}].type is required"
                )
            if not c_name:
                errors.append(
                    f"spec.decisions[{i}].signals.conditions[{j}].name is required"
                )

            # Cross-reference: condition name must match a defined signal
            if c_type == "domain" and c_name and c_name not in domain_names:
                errors.append(
                    f"spec.decisions[{i}].signals.conditions[{j}]: domain '{c_name}' "
                    f"is not defined in spec.signals.domains. "
                    f"Available: {sorted(domain_names)}"
                )
            elif c_type == "keyword" and c_name and c_name not in keyword_names:
                errors.append(
                    f"spec.decisions[{i}].signals.conditions[{j}]: keyword '{c_name}' "
                    f"is not defined in spec.signals.keywords. "
                    f"Available: {sorted(keyword_names)}"
                )
            elif c_type == "embedding" and c_name and c_name not in embedding_names:
                errors.append(
                    f"spec.decisions[{i}].signals.conditions[{j}]: embedding '{c_name}' "
                    f"is not defined in spec.signals.embeddings. "
                    f"Available: {sorted(embedding_names)}"
                )

        # Validate modelRefs
        model_refs = decision.get("modelRefs", [])
        if not model_refs:
            errors.append(f"spec.decisions[{i}].modelRefs must have at least one entry")
        else:
            for k, ref in enumerate(model_refs):
                model = ref.get("model", "")
                if not model:
                    errors.append(
                        f"spec.decisions[{i}].modelRefs[{k}].model is required"
                    )

    return errors


def main():
    if len(sys.argv) < 2:
        print("INVALID: No YAML content provided. Usage: validate_cr.py '<yaml>'")
        sys.exit(1)

    raw = sys.argv[1]

    try:
        # Support multi-doc YAML (separated by ---) and single docs
        docs = []
        for part in raw.split("\n---"):
            part = part.strip()
            if not part:
                continue
            parsed = yaml.safe_load(part)
            if parsed and isinstance(parsed, dict):
                docs.append(parsed)
    except yaml.YAMLError as e:
        print(f"INVALID: YAML parse error: {e}")
        sys.exit(1)

    if not docs:
        print("INVALID: No valid YAML documents found")
        sys.exit(1)

    all_errors = []

    for doc in docs:

        kind = doc.get("kind", "")
        spec = doc.get("spec", {})
        metadata = doc.get("metadata", {})
        name = metadata.get("name", "<unnamed>")
        ns = metadata.get("namespace", "<no namespace>")

        if kind == "IntelligentPool":
            errors = validate_pool(spec)
            if errors:
                all_errors.append(f"IntelligentPool '{name}' in '{ns}':")
                all_errors.extend(f"  - {e}" for e in errors)

        elif kind == "IntelligentRoute":
            errors = validate_route(spec)
            if errors:
                all_errors.append(f"IntelligentRoute '{name}' in '{ns}':")
                all_errors.extend(f"  - {e}" for e in errors)

        else:
            all_errors.append(
                f"Unknown kind '{kind}'. Expected IntelligentPool or IntelligentRoute."
            )

    if all_errors:
        print("INVALID: " + "\n".join(all_errors))
        sys.exit(1)
    else:
        kinds = [d.get("kind", "?") for d in docs if d]
        print(f"VALID: {', '.join(kinds)} passed all checks")
        sys.exit(0)


if __name__ == "__main__":
    main()
