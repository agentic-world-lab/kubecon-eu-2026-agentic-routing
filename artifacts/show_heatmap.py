#!/usr/bin/env python3
import json
import subprocess
import os
import sys
import argparse

# MMLU Categories to Router Domains mapping (from config.go)
MMLU_TO_DOMAIN = {
    "biology":          "science",
    "business":         "finance",
    "chemistry":        "science",
    "computer science": "technology",
    "economics":        "finance",
    "engineering":      "technology",
    "health":           "health",
    "history":          "legal",
    "law":              "legal",
    "math":             "science",
    "other":            "general",
    "philosophy":       "general",
    "physics":          "science",
    "psychology":       "health",
}

DOMAINS = ["science", "finance", "technology", "health", "legal", "general"]

def get_strategy():
    try:
        cmd = "kubectl get po -n intelligent-router-system intelligent-router-0 -o jsonpath='{.spec.containers[0].env[?(@.name==\"OPTIMIZATION_TARGET\")].value}'"
        result = subprocess.check_output(cmd, shell=True, text=True, stderr=subprocess.DEVNULL).strip()
        return result if result else "accuracy"
    except:
        return "accuracy"

def get_weights(strategy):
    if strategy == "latency":
        return {"quality": 0.1, "latency": 0.8, "cost": 0.1}
    elif strategy == "cost":
        return {"quality": 0.1, "latency": 0.1, "cost": 0.8}
    else: # accuracy
        return {"quality": 0.8, "latency": 0.1, "cost": 0.1}

def get_live_metrics():
    """Fetches live latency (in ms) and budget pressure from the router's Prometheus endpoint."""
    latencies = {}
    pressure = 0.0
    try:
        cmd = "kubectl get --raw \"/api/v1/namespaces/intelligent-router-system/pods/intelligent-router-0:9091/proxy/metrics\""
        output = subprocess.check_output(cmd, shell=True, text=True, stderr=subprocess.DEVNULL)
        for line in output.split('\n'):
            if line.startswith("intelligent_router_latency_ms{"):
                # intelligent_router_latency_ms{model="gpt-4.1"} 5983
                parts = line.split('} ')
                if len(parts) == 2:
                    model = parts[0].split('model="')[1].split('"')[0]
                    latency_ms = float(parts[1])
                    latencies[model] = latency_ms / 1000.0  # Convert to seconds
            elif line.startswith("intelligent_router_budget_pressure{"):
                parts = line.split('} ')
                if len(parts) == 2:
                    val = float(parts[1])
                    if val > pressure:
                        pressure = val
    except Exception as e:
        print(f"Warning: Could not fetch live metrics: {e}", file=sys.stderr)
    return latencies, pressure

def fetch_models(live_latencies=None):
    models = {}
    if live_latencies is None:
        live_latencies = {}
        
    try:
        cmd = "kubectl get llmbackends -A -o json"
        data = json.loads(subprocess.check_output(cmd, shell=True, text=True))
        for item in data.get("items", []):
            if item.get("status", {}).get("phase") == "Evaluated":
                name = item["spec"]["model"]
                results = item["status"]["results"]
                
                # Calculate cost metric (cost per 1M tokens)
                prompt_cost = float(results.get("pricing", {}).get("prompt", 0))
                comp_cost = float(results.get("pricing", {}).get("completion", 0))
                tps = float(results.get("tokensPerSecond", 0))
                
                if prompt_cost + comp_cost > 0:
                    cost = prompt_cost + comp_cost
                elif tps > 0:
                    cost = 1.0 / tps
                else:
                    cost = 0.0

                # Map MMLU categories to the 6 domains
                domain_sums = {d: 0.0 for d in DOMAINS}
                domain_counts = {d: 0 for d in DOMAINS}
                
                category_acc = results.get("categoryAccuracy", {})
                for cat, acc in category_acc.items():
                    domain = MMLU_TO_DOMAIN.get(cat, "general")
                    domain_sums[domain] += float(acc)
                    domain_counts[domain] += 1
                
                quality_scores = {}
                for d in DOMAINS:
                    if domain_counts[d] > 0:
                        quality_scores[d] = domain_sums[d] / domain_counts[d]
                    else:
                        quality_scores[d] = 0.0

                # Determine latency (live vs baseline)
                latency = float(results.get("avgResponseTime", 0))
                if name in live_latencies and live_latencies[name] > 0:
                    latency = live_latencies[name]

                models[name] = {
                    "latency": latency,
                    "cost": cost,
                    "quality": quality_scores
                }
    except Exception as e:
        print(f"Error fetching models: {e}")
        sys.exit(1)
    return models

def normalize(val, min_val, max_val):
    if max_val == min_val:
        return 0.5
    return (val - min_val) / (max_val - min_val)

def score_models(domain, models, weights):
    if not models:
        return []
    
    # Calculate min/max per dimension for normalization
    q_min, q_max = min(m["quality"][domain] for m in models.values()), max(m["quality"][domain] for m in models.values())
    l_min, l_max = min(m["latency"] for m in models.values()), max(m["latency"] for m in models.values())
    c_min, c_max = min(m["cost"] for m in models.values()), max(m["cost"] for m in models.values())
    
    scores = []
    for name, m in models.items():
        q_norm = normalize(m["quality"][domain], q_min, q_max)
        l_norm = normalize(m["latency"], l_min, l_max)
        c_norm = normalize(m["cost"], c_min, c_max)
        
        # Final = Quality - Latency - Cost (Latency & Cost act as penalties)
        final = (weights["quality"] * q_norm) - (weights["latency"] * l_norm) - (weights["cost"] * c_norm)
        scores.append((name, final, m["quality"][domain]))
        
    # Sort descending by final score
    scores.sort(key=lambda x: x[1], reverse=True)
    return scores

def print_heatmap(use_live=False):
    strategy = get_strategy()
    weights = get_weights(strategy)
    
    live_latencies = None
    pressure = 0.0
    original_cost_weight = weights["cost"]
    
    if use_live:
        live_latencies, pressure = get_live_metrics()
        # Amplified cost weight under budget pressure (exactly like config.go)
        if pressure > 0:
            weights["cost"] = weights["cost"] * (1.0 + pressure)
            
    models = fetch_models(live_latencies)
    
    if not models:
        print("No Evaluated LLMBackends found.")
        return

    mode_str = "\033[93mLIVE (Dynamic)\033[0m" if use_live else "\033[90mBASELINE (Static)\033[0m"
    pressure_str = f" │ Pressure: \033[93m{pressure:.3f}\033[0m" if pressure > 0 else ""

    print("\n" + "═" * 98)
    print(f" 🎯 ROUTER SELECTION HEATMAP (Strategy: \033[96m{strategy.upper()}\033[0m) [{mode_str}]{pressure_str}")
    if use_live:
        print(f" 🧮 Formula: Score = ({weights['quality']:.1f} * Q) - ({weights['latency']:.1f} * LiveLatency) - ({weights['cost']:.2f} * Cost)")
        if pressure > 0:
            print(f"    └─ \033[93mCost heavily penalized due to budget pressure ({original_cost_weight:.1f} * (1 + {pressure:.3f}))\033[0m")
    else:
        print(f" 🧮 Formula: Score = ({weights['quality']:.1f} * Q) - ({weights['latency']:.1f} * BaseLatency) - ({weights['cost']:.1f} * BaseCost)")
    print("═" * 98)

    print(f"{'DOMAIN':<14} │ {'TOP CHOICE (1st)':<31} │ {'RUNNER UP (2nd)':<31}")
    print("─" * 98)
    
    # ANSI Colors
    C_DOMAIN = "\033[95m"
    C_MODEL1 = "\033[92m"
    C_MODEL2 = "\033[93m"
    C_RESET  = "\033[0m"
    C_DIM    = "\033[90m"

    for domain in DOMAINS:
        scores = score_models(domain, models, weights)
        if not scores:
            continue
            
        top1 = scores[0]
        top2 = scores[1] if len(scores) > 1 else ("N/A", 0, 0)
        
        domain_str = f"{C_DOMAIN}{domain.capitalize():<14}{C_RESET}"
        
        # Display live latency next to the model name if we are in live mode
        t1_lat = f"{models[top1[0]]['latency']:.1f}s" if use_live and top1[0] != "N/A" else ""
        t2_lat = f"{models[top2[0]]['latency']:.1f}s" if use_live and top2[0] != "N/A" else ""
        
        m1_display = f"{top1[0]:<17} {t1_lat:>5}"
        m2_display = f"{top2[0]:<17} {t2_lat:>5}"

        # Format model string: Name (RawQualityLimit)
        m1_str = f"{C_MODEL1}{m1_display:<24}{C_RESET} {C_DIM}(q:{top1[2]:.2f}){C_RESET}"
        m2_str = f"{C_MODEL2}{m2_display:<24}{C_RESET} {C_DIM}(q:{top2[2]:.2f}){C_RESET}"
        
        print(f"{domain_str} │ {m1_str} │ {m2_str}")
    
    print("═" * 98 + "\n")

if __name__ == "__main__":
    parser = argparse.ArgumentParser(description="Show Intelligent Router Decision Heatmap")
    parser.add_argument("--live", action="store_true", help="Fetch live latencies and budget pressure directly from the router to show the current dynamic map")
    args = parser.parse_args()
    
    print_heatmap(use_live=args.live)
