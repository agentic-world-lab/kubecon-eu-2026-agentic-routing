#!/usr/bin/env python3
"""
generate-router-config.py

Reads benchmark results JSON and generates:
1. config.yaml for the Semantic Router
2. temp-router-manifest.yaml Kubernetes ConfigMap
3. routing-summary.json

Usage:
    python scripts/generate-router-config.py benchmarks.json
"""

import json
import sys
from collections import defaultdict, Counter

DOMAINS = [
    "business",
    "law",
    "psychology",
    "biology",
    "chemistry",
    "history",
    "other",
    "health",
    "economics",
    "math",
    "physics",
    "computer science",
    "philosophy",
    "engineering",
]

REASONING_DOMAINS = {"math", "physics", "chemistry"}

SYSTEM_PROMPTS = {
    "business": "You are a senior business consultant and strategic advisor with expertise in corporate strategy, operations management, financial analysis, marketing, and organizational development. Provide practical, actionable business advice backed by proven methodologies and industry best practices. Consider market dynamics, competitive landscape, and stakeholder interests in your recommendations.",
    "law": "You are a knowledgeable legal expert with comprehensive understanding of legal principles, case law, statutory interpretation, and legal procedures across multiple jurisdictions. Provide accurate legal information and analysis while clearly stating that your responses are for informational purposes only and do not constitute legal advice. Always recommend consulting with qualified legal professionals for specific legal matters.",
    "psychology": "You are a psychology expert with deep knowledge of cognitive processes, behavioral patterns, mental health, developmental psychology, social psychology, and therapeutic approaches. Provide evidence-based insights grounded in psychological research and theory. When discussing mental health topics, emphasize the importance of professional consultation and avoid providing diagnostic or therapeutic advice.",
    "biology": "You are a biology expert with comprehensive knowledge spanning molecular biology, genetics, cell biology, ecology, evolution, anatomy, physiology, and biotechnology. Explain biological concepts with scientific accuracy, use appropriate terminology, and provide examples from current research. Connect biological principles to real-world applications and emphasize the interconnectedness of biological systems.",
    "chemistry": "You are a chemistry expert specializing in chemical reactions, molecular structures, and laboratory techniques. Provide detailed, step-by-step explanations.",
    "history": "You are a historian with expertise across different time periods and cultures. Provide accurate historical context and analysis.",
    "health": "You are a health and medical information expert with knowledge of anatomy, physiology, diseases, treatments, preventive care, nutrition, and wellness. Provide accurate, evidence-based health information while emphasizing that your responses are for educational purposes only and should never replace professional medical advice, diagnosis, or treatment. Always encourage users to consult healthcare professionals for medical concerns and emergencies.",
    "economics": "You are an economics expert with deep understanding of microeconomics, macroeconomics, econometrics, financial markets, monetary policy, fiscal policy, international trade, and economic theory. Analyze economic phenomena using established economic principles, provide data-driven insights, and explain complex economic concepts in accessible terms. Consider both theoretical frameworks and real-world applications in your responses.",
    "math": "You are a mathematics expert. Provide step-by-step solutions, show your work clearly, and explain mathematical concepts in an understandable way.",
    "physics": "You are a physics expert with deep understanding of physical laws and phenomena. Provide clear explanations with mathematical derivations when appropriate.",
    "computer science": "You are a computer science expert with knowledge of algorithms, data structures, programming languages, and software engineering. Provide clear, practical solutions with code examples when helpful.",
    "philosophy": "You are a philosophy expert with comprehensive knowledge of philosophical traditions, ethical theories, logic, metaphysics, epistemology, political philosophy, and the history of philosophical thought. Engage with complex philosophical questions by presenting multiple perspectives, analyzing arguments rigorously, and encouraging critical thinking. Draw connections between philosophical concepts and contemporary issues while maintaining intellectual honesty about the complexity and ongoing nature of philosophical debates.",
    "engineering": "You are an engineering expert with knowledge across multiple engineering disciplines including mechanical, electrical, civil, chemical, software, and systems engineering. Apply engineering principles, design methodologies, and problem-solving approaches to provide practical solutions. Consider safety, efficiency, sustainability, and cost-effectiveness in your recommendations. Use technical precision while explaining concepts clearly, and emphasize the importance of proper engineering practices and standards.",
    "other": "You are a helpful and knowledgeable assistant. Provide accurate, helpful responses across a wide range of topics."
}

CATEGORIES = [
  {"name": "business", "description": "Business and management related queries", "mmlu_categories": ["business"]},
  {"name": "law", "description": "Legal questions and law-related topics", "mmlu_categories": ["law"]},
  {"name": "psychology", "description": "Psychology and mental health topics", "mmlu_categories": ["psychology"]},
  {"name": "biology", "description": "Biology and life sciences questions", "mmlu_categories": ["biology"]},
  {"name": "chemistry", "description": "Chemistry and chemical sciences questions", "mmlu_categories": ["chemistry"]},
  {"name": "history", "description": "Historical questions and cultural topics", "mmlu_categories": ["history"]},
  {"name": "other", "description": "General knowledge and miscellaneous topics", "mmlu_categories": ["other"]},
  {"name": "health", "description": "Health and medical information queries", "mmlu_categories": ["health"]},
  {"name": "economics", "description": "Economics and financial topics", "mmlu_categories": ["economics"]},
  {"name": "math", "description": "Mathematics and quantitative reasoning", "mmlu_categories": ["math"]},
  {"name": "physics", "description": "Physics and physical sciences", "mmlu_categories": ["physics"]},
  {"name": "computer science", "description": "Computer science and programming", "mmlu_categories": ["computer science"]},
  {"name": "philosophy", "description": "Philosophy and ethical questions", "mmlu_categories": ["philosophy"]},
  {"name": "engineering", "description": "Engineering and technical problem-solving", "mmlu_categories": ["engineering"]},
]


def normalize_domain(name: str) -> str:
    return name.lower().replace(" ", "_")


def load_benchmarks(path):
    with open(path) as f:
        return json.load(f)


def select_best_models(benchmarks):
    """
    Returns dict domain -> best_model using:
    1. highest accuracy
    2. lowest latency
    3. highest tok/sec
    4. alphabetical model
    """
    domain_candidates = defaultdict(list)

    for entry in benchmarks:
        model = entry["model"]
        results = entry["results"]
        latency = results.get("avgResponseTime", float("inf"))
        tokps = results.get("tokensPerSecond", 0)
        cats = results.get("categoryAccuracy", {})

        for domain, acc in cats.items():
            d = normalize_domain(domain)
            domain_candidates[d].append(
                {
                    "model": model,
                    "accuracy": acc,
                    "latency": latency,
                    "tokps": tokps,
                }
            )

    best = {}

    for domain, candidates in domain_candidates.items():
        candidates.sort(
            key=lambda x: (
                -x["accuracy"],
                x["latency"],
                -x["tokps"],
                x["model"],
            )
        )
        best[domain] = candidates[0]["model"]

    return best


def compute_default_model(mapping, benchmarks):
    counts = Counter(mapping.values())
    most_common = counts.most_common()

    if len(most_common) == 0:
        return None

    if len(most_common) == 1 or most_common[0][1] > most_common[1][1]:
        return most_common[0][0]

    # fallback highest overall accuracy
    best_model = None
    best_acc = -1

    for entry in benchmarks:
        acc = entry["results"].get("overallAccuracy", 0)
        if acc > best_acc:
            best_acc = acc
            best_model = entry["model"]

    return best_model


def build_model_config(models):
    cfg = {}
    for m in models:
        cfg[m] = {"reasoning_family": "generic"}
    return cfg


def build_decision(domain, model):
    decision_name = f"{domain}_decision" if domain != "other" else "general_decision"
    # Find matching category description
    desc = next((c["description"] for c in CATEGORIES if c["name"] == domain), f"{domain} queries")
    
    plugins = [
        {
            "type": "system_prompt",
            "configuration": {
                "system_prompt": SYSTEM_PROMPTS.get(domain, f"You are an expert in {domain.replace('_',' ')}.")
            },
        }
    ]

    # Add specific plugins per domain based on requested behavior
    if domain in ["psychology", "health"]:
        plugins.append({
            "type": "semantic-cache",
            "configuration": {
                "enabled": True,
                "similarity_threshold": 0.95 if domain == "health" else 0.92
            }
        })

    return {
        "name": decision_name,
        "description": desc,
        "priority": 100 if domain != "other" else 50,
        "rules": {
            "operator": "OR",
            "conditions": [{"type": "domain", "name": domain}],
        },
        "modelRefs": [
            {
                "model": model,
                "use_reasoning": domain in REASONING_DOMAINS,
            }
        ],
        "plugins": plugins,
    }


def build_config(best_models, default_model):
    models_used = set(best_models.values())
    if default_model:
        models_used.add(default_model)

    config = {
        "gateway_type": "agentgateway",
        "bert_model": {
            "model_id": "models/mom-embedding-light",
            "threshold": 0.6,
            "use_cpu": True
        },
        "classifier": {
            "category_model": {
                "model_id": "models/mom-domain-classifier",
                "use_modernbert": True,
                "threshold": 0.6,
                "use_cpu": True,
                "category_mapping_path": "models/mom-domain-classifier/category_mapping.json"
            },
            "pii_model": {
                "model_id": "models/pii_classifier_modernbert-base_presidio_token_model",
                "use_modernbert": True,
                "threshold": 0.7,
                "use_cpu": True,
                "pii_mapping_path": "models/pii_classifier_modernbert-base_presidio_token_model/label_mapping.json"
            }
        },
        "strategy": "priority",
        "categories": CATEGORIES,
        "model_config": build_model_config(models_used),
        "default_model": default_model,
        "decisions": [],
    }

    for domain in DOMAINS:
        model = best_models.get(domain, default_model)
        config["decisions"].append(build_decision(domain, model))

    return config


def dict_to_yaml(data, indent=0, indent_step=2):
    """Helper to dump dicts and lists to yaml without the yaml module."""
    lines = []
    prefix = ' ' * indent
    if isinstance(data, dict):
        for k, v in data.items():
            if isinstance(v, (dict, list)) and len(v) > 0:
                lines.append(f"{prefix}{k}:")
                lines.append(dict_to_yaml(v, indent + indent_step, indent_step))
            elif isinstance(v, list) and len(v) == 0:
                lines.append(f"{prefix}{k}: []")
            elif isinstance(v, bool):
                lines.append(f"{prefix}{k}: {'true' if v else 'false'}")
            elif isinstance(v, str):
                if '\n' in v or ':' in v or v == '' or v.startswith('You are'):
                    # use literal block for multi-line or complicated strings
                    lines.append(f"{prefix}{k}: >\n{prefix}{' ' * indent_step}{v}")
                else:
                    lines.append(f"{prefix}{k}: {v}")
            else:
                lines.append(f"{prefix}{k}: {v}")
    elif isinstance(data, list):
        for item in data:
            if isinstance(item, dict):
                first = True
                for k, v in item.items():
                    p = f"{prefix[:-indent_step]}{' ' * (indent_step - 2)}- " if first else prefix
                    if isinstance(v, (dict, list)) and len(v) > 0:
                        lines.append(f"{p}{k}:")
                        lines.append(dict_to_yaml(v, indent + indent_step, indent_step))
                    elif isinstance(v, list) and len(v) == 0:
                        lines.append(f"{p}{k}: []")
                    elif isinstance(v, bool):
                        lines.append(f"{p}{k}: {'true' if v else 'false'}")
                    elif isinstance(v, str):
                        if '\n' in v or ':' in v or v == '' or v.startswith('You are'):
                            lines.append(f"{p}{k}: >\n{prefix}{' ' * indent_step}{v}")
                        else:
                            lines.append(f"{p}{k}: {v}")
                    else:
                        lines.append(f"{p}{k}: {v}")
                    first = False
            else:
                # simple list item
                if isinstance(item, str):
                    lines.append(f"{prefix[:-indent_step]}{' ' * (indent_step - 2)}- \"{item}\"")
                else:
                    lines.append(f"{prefix[:-indent_step]}{' ' * (indent_step - 2)}- {item}")
    return "\n".join(lines)


def build_manifest(config_yaml):
    indented_config = "\n".join(["    " + line for line in config_yaml.split("\n")])
    return f"""apiVersion: v1
kind: ConfigMap
metadata:
  name: semantic-router-config
  namespace: vllm-semantic-router-system
data:
  config.yaml: |
{indented_config}
"""

def main():
    if len(sys.argv) != 2:
        print("Usage: generate-router-config.py <benchmarks.json | json_string>")
        sys.exit(1)

    arg = sys.argv[1]
    
    if arg.strip().startswith("[") or arg.strip().startswith("{"):
        try:
            benchmarks = json.loads(arg)
        except json.JSONDecodeError:
            try:
                import ast
                benchmarks = ast.literal_eval(arg)
            except Exception:
                # Fallback for environments that incorrectly over-escape double quotes in bash single-quote literal args
                arg_unescaped = arg.replace('\\"', '"').replace("\\'", "'")
                benchmarks = json.loads(arg_unescaped)
    else:
        benchmarks = load_benchmarks(arg)

    best_models = select_best_models(benchmarks)
    default_model = compute_default_model(best_models, benchmarks)
    config = build_config(best_models, default_model)

    config_yaml = dict_to_yaml(config)
    manifest_yaml = build_manifest(config_yaml)

    # Always output manifest to stdout for the agent to capture in read-only environments
    print(manifest_yaml)

    # Try to write files if possible
    try:
        with open("config.yaml", "w") as f:
            f.write(config_yaml)
        with open("temp-router-manifest.yaml", "w") as f:
            f.write(manifest_yaml)
        with open("routing-summary.json", "w") as f:
            json.dump(best_models, f, indent=2)
    except OSError:
        pass


if __name__ == "__main__":
    main()
