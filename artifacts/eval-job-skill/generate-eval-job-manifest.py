#!/usr/bin/env python3

# generate-eval-job-manifest.py
# Zero-dependency: uses only Python built-ins (no pip, no yaml library)

import sys
from textwrap import dedent


def generate_manifest(name, model_name_from_spec, endpoint, namespace):
    """Generate valid Kubernetes Job YAML using only built-in Python."""
    job_name = f"eval-{name}"

    manifest = f"""\
        apiVersion: batch/v1
        kind: Job
        metadata:
          name: {job_name}
          namespace: {namespace}
        spec:
          backoffLimit: 0
          template:
            spec:
              serviceAccountName: default
              containers:
              - name: eval
                image: fjvicens/mmlu-pro-eval-job:0.8
                env:
                - name: EVAL_MODEL
                  value: "{model_name_from_spec}"
                - name: EVAL_ENDPOINT
                  value: "{endpoint}"
                - name: OPENAI_API_KEY
                  valueFrom:
                    secretKeyRef:
                      name: openai-secret
                      key: OPENAI_API_KEY
              restartPolicy: Never
        """

    return dedent(manifest).strip() + "\n"


def print_usage():
    print("""Generate MMLU Eval Job Manifest

Usage:
  python generate-eval-job-manifest.py <name> <model_name_from_spec> <endpoint> <namespace>

Example:
  python generate-eval-job-manifest.py my-backend gpt-4o http://10.0.0.1:8000/v1 default

No pip, no yaml, no problem.
""")


if __name__ == "__main__":
    args = sys.argv[1:]

    if not args or "-h" in args or "--help" in args:
        print_usage()
        sys.exit(0)

    if len(args) != 4:
        print("Error: Need exactly 4 arguments: <name> <model_name_from_spec> <endpoint> <namespace>")
        print_usage()
        sys.exit(1)

    name, model_name_from_spec, endpoint, namespace = args

    yaml_output = generate_manifest(name, model_name_from_spec, endpoint, namespace)
    print(yaml_output)
