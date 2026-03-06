---
name: sr-config-domain-based-skill
description: Generate an optimized Semantic Router configuration from model benchmark results
---

# Semantic Router Config Skill

This skill allows the agent to automatically generate and deploy an optimal Semantic Router configuration (`config.yaml`) based on model benchmark results.

The routing logic computes the best model per domain using categorical accuracy, latency, and token throughput.

## When to use this skill

Use this skill when the user asks to:
- "Generate a semantic router config from benchmark results and deploy it"
- "Optimize the AI gateway routing based on recent benchmarks"
- "Update Semantic Router models"

## Expected Inputs

This skill expects model benchmark results in JSON format. The benchmarks must first be retrieved using the available `get_model_benchmarks` MCP tool.

Example benchmark format:

```json
[
  {
    "model": "gpt-3.5-turbo",
    "results": {
      "avgResponseTime": 0.6834,
      "categoryAccuracy": {
        "biology": 0.6,
        "business": 0.4
      },
      "overallAccuracy": 0.4143,
      "tokPerSecond": 20.59
    }
  }
]
```

## Agent Instructions: How to invoke the skill

To use this skill, you MUST follow these exact steps in order:

1. **Retrieve the benchmarks**: Use the `sr-llm-backends` MCP tool to fetch the current model benchmarks.
2. **Execute the script**: Run the included python script `generate-router-config.py` passing the benchmark JSON directly as a string argument.
   ```bash
   python scripts/generate-router-config.py '[{"model": "qwen2.5:3b", "results": ...}]'
   ```
3. **Capture and Deploy**: In a read-only environment, the script avoids crashing by writing the generated Kubernetes ConfigMap manifest directly to `stdout`. Capture this output, save it or pass it directly to your Kubernetes MCP tools to apply it to the cluster (e.g. `kubernetes.apply`).

## Artifacts Generated

In write-enabled environments, `scripts/generate-router-config.py` outputs:
- `config.yaml`: The raw semantic router configuration.
- `temp-router-manifest.yaml`: The Kubernetes ConfigMap defining the configurations.
- `routing-summary.json`: A summary of which models were selected for each domain.
*(If the filesystem is read-only, it skips generating physical files and pushes only the complete manifest to `stdout`)*
