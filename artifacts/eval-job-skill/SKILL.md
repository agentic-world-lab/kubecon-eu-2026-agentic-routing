---
name: eval-job-skill
description: Generate a Kubernetes Job manifest for MMLU model evaluation
---

# Eval Job Manifest Skill

This skill generates a Kubernetes `batch/v1` Job manifest YAML for launching an MMLU evaluation against a model endpoint.

## When to use this skill

Use this skill when asked to:
- "Launch an evaluation job for a model"
- "Trigger an MMLU evaluation"
- "Create an eval job for an LLMBackend"

## Expected Inputs

The skill requires four parameters:
- `name`: The LLMBackend resource name (used to derive the job name as `eval-{name}`)
- `model_name_from_spec`: The model identifier from `spec.model` (e.g. `gpt-4o`)
- `endpoint`: The model endpoint URL (e.g. `http://10.0.0.1:8000/v1`)
- `namespace`: Kubernetes namespace for the job

## Agent Instructions: How to invoke the skill

To use this skill, follow these exact steps in order:

1. **Extract parameters** from the input message: `name`, `model_name_from_spec`, `endpoint`, `namespace`.
2. **Execute the script**: Run the included python script passing the four parameters as positional arguments:
   ```bash
   python /skills/eval-job-skill/generate-eval-job-manifest.py <name> <model_name_from_spec> <endpoint> <namespace>
   ```
   Example:
   ```bash
   python /skills/eval-job-skill/generate-eval-job-manifest.py my-backend gpt-4o http://10.0.0.1:8000/v1 default
   ```
3. **Capture the output**: The script writes the complete Job manifest YAML to `stdout`.
4. **Apply the manifest**: Use the `k8s_apply_manifest` tool to apply the captured YAML to the cluster.
5. **Return the result**: Report success or failure of the apply operation.

## Artifacts Generated

The script outputs a single Kubernetes Job YAML manifest to `stdout` with:
- Deterministic job name: `eval-{name}`
- Image: `fjvicens/mmlu-pro-eval-job:0.5`
- Environment variables: `EVAL_MODEL`, `EVAL_ENDPOINT`, `OPENAI_API_KEY` (from secret)
- `backoffLimit: 0`, `restartPolicy: Never`
